// Package workload ports HammerDB v6.1's PostgreSQL and MySQL TPROC-C workload.
// It is GPL-3.0 licensed; see LICENSE and NOTICE.
package workload

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
)

//go:embed *.go *.sql LICENSE NOTICE README.md
var source embed.FS

var Test = bench.Test{
	Name: "hammerdb-tpcc", Define: define, Source: source,
	SourcePackage: "github.com/stroppy-io/stroppy-contrib/hammerdb-tpcc/workload",
}

func init() { bench.Register(Test) }

type workload struct {
	mysql, prepared, noStoredProcs, historyPK bool
	storageEngine                             string
	mode                                      string
	workers, warehouses, loadWorkers          int
	iterations                                int64
	seed                                      int64
	rampup, duration, asyncDelay              time.Duration
	storedprocs, keyandthink, allwarehouse    bool
	raiseerror, asyncScale, asyncVerbose      bool
	timeprofile, partition, vacuum, create    bool
	asyncClients                              int
	pool                                      *poolConfig
	connections                               map[string]bench.DriverRef
	attempts, failures                        *bench.CounterHandle
	latency                                   *bench.HistogramHandle
	nopm, tpm                                 *bench.GaugeHandle
	result                                    measurement
	itemRandom                                *random
	nextLoader                                atomic.Int64
}

func define(d *bench.Def) error {
	w := &workload{}
	w.mode, _ = d.Param.String("mode", "timed", "HammerDB driver mode.", bench.OneOf("test", "timed"))
	w.workers, _ = d.Param.Int("vus", 1, "Active HammerDB worker threads; excludes the timed monitor.", bench.Min(1), bench.Max(1000000))
	w.iterations, _ = d.Param.Int64("iterations", 2147483647, "Transactions per client, including ramp-up.", bench.Min(int64(1)))
	w.rampup, _ = d.Param.Duration("rampup", 2*time.Minute, "Traffic before the first counter snapshot.", bench.Min(time.Duration(0)))
	w.duration, _ = d.Param.Duration("duration", 5*time.Minute, "Timed counter measurement window.", bench.Min(time.Nanosecond))
	w.seed, _ = d.Param.Int64("seed", 1, "Tcl-compatible random seed; worker streams use seed plus worker index.", bench.Min(int64(1)), bench.Max(int64(2147483646)))
	w.warehouses, _ = d.Param.Int("warehouses", 1, "Warehouses to create; runtime reads the database maximum.", bench.Min(1), bench.Max(100000))
	w.loadWorkers, _ = d.Param.Int("load-workers", 1, "Parallel warehouse loaders.", bench.Min(1))
	w.create, _ = d.Param.Bool("create", false, "Create and populate a new schema before running; existing tables are never dropped.")
	w.storedprocs, _ = d.Param.Bool("storedprocs", false, "PostgreSQL procedures instead of functions; MySQL uses procedures by default.")
	w.prepared, _ = d.Param.Bool("prepared", false, "MySQL SQL PREPARE/EXECUTE calls.")
	w.noStoredProcs, _ = d.Param.Bool("no-stored-procs", false, "MySQL client-side SQL transactions instead of procedure calls.")
	w.historyPK, _ = d.Param.Bool("history-pk", false, "MySQL 8.0.30+ invisible primary key on history.")
	w.storageEngine, _ = d.Param.String("storage-engine", "innodb", "MySQL table storage engine.")
	w.keyandthink, _ = d.Param.Bool("keyandthink", false, "Apply HammerDB keying and rounded exponential think times.")
	w.allwarehouse, _ = d.Param.Bool("allwarehouse", false, "Choose each transaction's warehouse from the worker-assigned list.")
	w.raiseerror, _ = d.Param.Bool("raiseerror", false, "Stop the affected client on a SQL error instead of continuing; never retry.")
	w.asyncScale, _ = d.Param.Bool("async-scale", false, "Run multiple concurrent clients per worker thread (timed mode only).")
	w.asyncClients, _ = d.Param.Int("async-client", 10, "Clients per worker when async-scale is enabled.", bench.Min(1), bench.Max(10000))
	w.asyncDelay, _ = d.Param.Duration("async-delay", time.Second, "Delay between asynchronous client logins.", bench.Min(time.Duration(0)))
	w.asyncVerbose, _ = d.Param.Bool("async-verbose", false, "Log asynchronous transaction activity.")
	w.timeprofile, _ = d.Param.Bool("timeprofile", false, "Record per-transaction client latency histograms.")
	w.partition, _ = d.Param.Bool("partition", false, "Hash-partition order_line when creating at least 200 warehouses.")
	w.vacuum, _ = d.Param.Bool("vacuum", false, "Run VACUUM and CHECKPOINT after the timed run; requires database privileges.")
	queryTimeout, _ := d.Param.Duration("query-timeout", 0, "Per-statement timeout; zero preserves HammerDB's unbounded queries.", bench.Min(time.Duration(0)))
	d.Drivers.QueryTimeout(queryTimeout)
	poolFile, _ := d.Param.String("connect-pool", "", "HammerDB pgcpool.xml or mysqlcpool.xml for per-transaction connection routing.")
	if w.mode != "timed" && w.asyncScale {
		return errors.New("async-scale requires timed mode, as in HammerDB")
	}
	if w.duration > time.Duration(1<<63-1)-w.rampup {
		return errors.New("rampup plus duration exceeds timer capacity")
	}
	if w.asyncDelay > time.Duration(1<<63-1)/time.Duration(w.asyncClients) {
		return errors.New("async client login delay exceeds timer capacity")
	}
	clients := w.workers
	if w.asyncScale {
		if w.workers > 1000000/w.asyncClients {
			return errors.New("too many asynchronous clients")
		}
		clients *= w.asyncClients
	}
	mode := "cache_statement"
	if w.storedprocs && poolFile == "" {
		mode = "simple_protocol"
	}
	capacity := int32(max(clients+1, min(w.loadWorkers, w.warehouses)))
	driver := d.Drivers.Declare("default", bench.DriverConfig{Kind: bench.DriverPostgres, Postgres: &bench.PostgresConfig{DefaultQueryExecMode: &mode, MaxConns: &capacity}, SQL: &bench.SQLConfig{MaxOpenConns: &capacity}})
	w.mysql = driver.Kind() == bench.DriverMySQL
	if kind := driver.Kind(); kind != bench.DriverPostgres && kind != bench.DriverMySQL && kind != bench.DriverRecording {
		return fmt.Errorf("hammerdb-tpcc requires PostgreSQL or MySQL, got %s", kind)
	}
	if w.mysql && w.vacuum {
		return errors.New("vacuum is PostgreSQL-only")
	}
	if !w.mysql && (w.prepared || w.noStoredProcs || w.historyPK) {
		return errors.New("prepared, no-stored-procs and history-pk require MySQL")
	}
	for _, c := range w.storageEngine {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return errors.New("invalid storage-engine identifier")
		}
	}
	if w.storageEngine == "" {
		return errors.New("storage-engine must not be empty")
	}
	if poolFile != "" {
		var err error
		w.pool, w.connections, err = loadPool(poolFile, &d.Drivers, int32(clients), w.mysql)
		if w.mysql {
			w.prepared = true
		}
		if err != nil {
			return fmt.Errorf("connection pool: %w", err)
		}
	}
	labels := bench.LabelValues("transaction", transactionNames[:]...)
	w.attempts = d.Metrics.Counter("hammerdb_transactions_total", labels)
	w.failures = d.Metrics.Counter("hammerdb_errors_total", labels)
	w.latency = d.Metrics.Histogram("hammerdb_transaction_seconds", labels, bench.Unit("s"))
	w.nopm = d.Metrics.Gauge("hammerdb_nopm", bench.Unit("1/min"))
	w.tpm = d.Metrics.Gauge("hammerdb_tpm", bench.Unit("1/min"))
	d.Report.Contribute("hammerdb.tpcc", 1, w.report)
	d.Report.Render("hammerdb.tpcc", renderReport)
	if w.create {
		d.Execution.Step("create_schema", w.createSchema)
		d.Execution.Step("load_items", w.loadItems)
		loaders := min(w.loadWorkers, w.warehouses)
		d.Execution.Step("load_data", func(ctx context.Context, b *bench.Bench) error {
			if err := w.load(ctx, b); err != nil {
				return bench.Fatal(err)
			}
			return nil
		}, bench.SharedIterations(loaders, int64(loaders)))
		d.Execution.Step("create_indexes", w.indexes)
		d.Execution.Step("create_routines", w.routines)
		d.Execution.Step("analyze", func(ctx context.Context, b *bench.Bench) error {
			statement := "ANALYZE"
			if w.mysql {
				statement = "analyze table customer, district, history, item, new_order, orders, order_line, stock, warehouse"
			}
			return b.Exec(ctx, statement, nil)
		})
	}
	jobs := clients
	if w.mode == "timed" {
		jobs++
	}
	streams := make([]*threadRandom, w.workers)
	for i := range streams {
		streams[i] = &threadRandom{random: newRandom(w.seed + int64(i))}
	}
	terminals := make([]*terminal, clients)
	stop := make(chan struct{})
	var next atomic.Int64
	d.Execution.Step("workload", func(ctx context.Context, b *bench.Bench) error {
		index := int(next.Add(1) - 1)
		if index == clients {
			defer close(stop)
			if err := w.monitor(ctx, b); err != nil {
				return bench.Fatal(err)
			}
			return nil
		}
		owner := index
		if w.asyncScale {
			owner /= w.asyncClients
		}
		t := &terminal{random: streams[owner].random, stream: streams[owner], owner: owner, client: index}
		terminals[index] = t
		return w.runClient(ctx, b, t, stop)
	}, bench.SharedIterations(jobs, int64(jobs)))
	w.result.ClientCounts = make([][5]uint64, len(terminals))
	for i, t := range terminals {
		if t != nil {
			w.result.ClientCounts[i] = t.counts
		}
	}
	if w.vacuum && w.mode == "timed" {
		d.Execution.Step("vacuum", func(ctx context.Context, b *bench.Bench) error {
			if err := b.Exec(ctx, "VACUUM", nil); err != nil {
				return err
			}
			return b.Exec(ctx, "CHECKPOINT", nil)
		})
	}
	return d.Execution.Err()
}

type threadRandom struct {
	sync.Mutex
	*random
}

func assetSQL(name string) *bench.SQL {
	data, err := fs.ReadFile(source, name)
	if err != nil {
		panic(err)
	}
	return bench.ParseSQL(string(data))
}

func execSection(ctx context.Context, b *bench.Bench, sql *bench.SQL, section string) error {
	for _, statement := range sql.Section(section) {
		if err := b.Exec(ctx, statement, nil); err != nil {
			return err
		}
	}
	return nil
}

func (w *workload) createSchema(ctx context.Context, b *bench.Bench) error {
	if w.mysql {
		return w.createMySQLSchema(ctx, b)
	}
	for _, statement := range assetSQL("schema.sql").Section("create_schema") {
		if w.partition && w.warehouses >= 200 && strings.HasPrefix(statement, "CREATE TABLE ORDER_LINE") {
			statement += " PARTITION BY HASH (OL_W_ID)"
		}
		if err := b.Exec(ctx, statement, nil); err != nil {
			return err
		}
	}
	if w.partition && w.warehouses >= 200 {
		partitions := w.warehouses / 100
		for i := 0; i < partitions; i++ {
			statement := fmt.Sprintf("CREATE TABLE ol_%d PARTITION OF ORDER_LINE FOR VALUES WITH (MODULUS %d, REMAINDER %d)", i, partitions, i)
			if err := b.Exec(ctx, statement, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *workload) indexes(ctx context.Context, b *bench.Bench) error {
	if w.mysql {
		return nil
	}
	return execSection(ctx, b, assetSQL("schema.sql"), "create_indexes")
}

func (w *workload) routines(ctx context.Context, b *bench.Bench) error {
	if w.mysql {
		return execSection(ctx, b, assetSQL("mysql_procedures.sql"), "routines")
	}
	name := "functions.sql"
	if w.storedprocs {
		name = "procedures.sql"
	}
	return execSection(ctx, b, assetSQL(name), "routines")
}
