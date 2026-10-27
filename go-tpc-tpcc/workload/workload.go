package workload

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
)

//go:embed *.go LICENSE NOTICE README.md
var source embed.FS
var Test = bench.Test{Name: "go-tpc-tpcc", Define: define, Source: source, SourcePackage: "github.com/stroppy-io/stroppy-contrib/go-tpc-tpcc/workload"}

func init() { bench.Register(Test) }

const (
	PartitionTypeHash = iota + 1
	PartitionTypeRange
	PartitionTypeListAsHash
	PartitionTypeListAsRange
)

var tables = []string{tableItem, tableCustomer, tableDistrict, tableHistory, tableNewOrder, tableOrderLine, tableOrders, tableStock, tableWareHouse}

type Config struct {
	Driver                                               string
	Threads, Warehouses, Parts, PartitionType, Isolation int
	UseFK, UseClusteredIndex, CheckAll                   bool
	PrepareRetryCount                                    int
	PrepareRetryInterval                                 time.Duration
}
type constants struct{ last, item, customer int }
type Workloader struct {
	cfg          *Config
	constants    constants
	initLoadTime string
	now          func() time.Time
}
type contextKey struct{}
type tpccState struct {
	Conn                                                                         *session
	R, global                                                                    *rand.Rand
	Buf                                                                          *buffer
	newOrderStmts, paymentStmts, orderStatusStmts, deliveryStmts, stockLevelStmt map[string]*statement
}

func getTPCCState(ctx context.Context) *tpccState { return ctx.Value(contextKey{}).(*tpccState) }

type buffer struct {
	data   []byte
	offset int
}

func (b *buffer) Reset() { b.offset = 0 }
func (b *buffer) Alloc(n int) []byte {
	if len(b.data)-b.offset < n {
		b.data = make([]byte, max(1024, max(n, 2*(len(b.data)-b.offset))))
		b.offset = 0
	}
	start := b.offset
	b.offset += n
	return b.data[start:b.offset]
}
func quiet(string, ...any) {}

var names = []string{"new_order", "payment", "order_status", "delivery", "stock_level"}
var keying = []float64{18, 3, 2, 2, 2}
var thinking = []float64{12, 12, 10, 5, 5}

type counters struct {
	success, failed   [5]uint64
	started, finished time.Time
}

func define(d *bench.Def) error {
	cfg := &Config{}
	cfg.Threads, _ = d.Param.Int("vus", 1, "Active upstream worker threads.", bench.Min(1), bench.Max(1000000))
	cfg.Warehouses, _ = d.Param.Int("warehouses", 10, "Number of warehouses, matching the prepared database.", bench.Min(1))
	cfg.Parts, _ = d.Param.Int("parts", 1, "Number of warehouse partitions.", bench.Min(1))
	cfg.PartitionType, _ = d.Param.Int("partition-type", 1, "Upstream partition type: 1 hash, 2 range, 3 list-as-hash, 4 list-as-range.", bench.Min(1), bench.Max(4))
	cfg.Isolation, _ = d.Param.Int("isolation", 0, "database/sql isolation: 0 default, 1 read-uncommitted, 2 read-committed, 4 repeatable-read, 6 serializable.")
	cfg.UseFK, _ = d.Param.Bool("use-fk", false, "Create upstream foreign keys.")
	cfg.UseClusteredIndex, _ = d.Param.Bool("use-clustered-index", true, "TiDB clustered-index hints.")
	cfg.CheckAll, _ = d.Param.Bool("check-all", false, "Include the expensive consistency condition 3.3.2.11.")
	cfg.PrepareRetryCount, _ = d.Param.Int("retry-count", 50, "Load-batch retry count; transaction errors are never retried.", bench.Min(0))
	cfg.PrepareRetryInterval, _ = d.Param.Duration("retry-interval", 10*time.Second, "Interval between load-batch retries.", bench.Min(time.Duration(0)))
	action, _ := d.Param.String("action", "run", "Upstream action.", bench.OneOf("prepare", "run", "check", "cleanup"))
	count, _ := d.Param.Int64("count", 0, "Total iteration budget divided equally between workers; zero runs until duration.", bench.Min(int64(0)))
	duration, _ := d.Param.Duration("duration", time.Minute, "Run timeout; in-flight queries are canceled at expiry.", bench.Min(time.Nanosecond))
	wait, _ := d.Param.Bool("wait", false, "Apply upstream keying and capped exponential think time.")
	maxLatency, _ := d.Param.Duration("max-measure-latency", 16*time.Second, "Upper bound for recorded transaction latency, matching upstream histogram clipping.", bench.Min(time.Microsecond))
	ignore, _ := d.Param.Bool("ignore-error", false, "Continue the worker after a transaction error.")
	noCheck, _ := d.Param.Bool("no-check", false, "Skip post-prepare population checks.")
	seed, _ := d.Param.Int64("seed", 0, "Explicit test seed; zero uses wall-clock seeds like upstream.", bench.Min(int64(0)))
	weight, _ := d.Param.String("weight", "45,43,4,4,4", "Weights: NewOrder, Payment, OrderStatus, Delivery, StockLevel.")
	refresh, _ := d.Param.Duration("conn-refresh-interval", 0, "Reacquire worker connection at this interval.", bench.Min(time.Duration(0)))
	timeout, _ := d.Param.Duration("query-timeout", 0, "Per-query timeout; zero preserves upstream timeout behavior.", bench.Min(time.Duration(0)))
	d.Drivers.QueryTimeout(timeout)
	if cfg.Parts > cfg.Warehouses {
		return errors.New("parts must not exceed warehouses")
	}
	if _, ok := map[int]bool{0: true, 1: true, 2: true, 4: true, 6: true}[cfg.Isolation]; !ok {
		return errors.New("unsupported isolation")
	}
	deck, err := makeDeck(weight)
	if err != nil {
		return err
	}
	mode := "cache_statement"
	capacity := int32(cfg.Threads + 1)
	ref := d.Drivers.Declare("default", bench.DriverConfig{Kind: bench.DriverMySQL, Postgres: &bench.PostgresConfig{DefaultQueryExecMode: &mode, MaxConns: &capacity}, SQL: &bench.SQLConfig{MaxOpenConns: &capacity}})
	cfg.Driver = "mysql"
	if ref.Kind() == bench.DriverPostgres {
		cfg.Driver = "postgres"
	} else if ref.Kind() != bench.DriverMySQL && ref.Kind() != bench.DriverRecording {
		return errors.New("go-tpc-tpcc requires MySQL or PostgreSQL")
	}
	base := seed
	if base == 0 {
		base = time.Now().UnixNano()
	}
	cr := rand.New(rand.NewSource(base))
	w := &Workloader{cfg: cfg, constants: constants{last: cr.Intn(256), item: cr.Intn(1024), customer: cr.Intn(8192)}, now: time.Now, initLoadTime: time.Now().Format(timeFormat)}
	stats := make([]counters, cfg.Threads)
	attempts := d.Metrics.Counter("go_tpc_transactions_total", bench.LabelValues("transaction", names...))
	failures := d.Metrics.Counter("go_tpc_errors_total", bench.LabelValues("transaction", names...))
	latency := d.Metrics.Histogram("go_tpc_transaction_seconds", bench.LabelValues("transaction", names...), bench.Unit("s"))
	d.Report.Contribute("go-tpc.tpcc", 1, func(bench.ReportContext) (bench.ReportContribution, error) {
		return bench.ReportContribution{Data: summary(stats, cfg.Warehouses)}, nil
	})
	d.Report.Render("go-tpc.tpcc", render)
	if action == "prepare" {
		d.Execution.Step("create_schema", func(ctx context.Context, b *bench.Bench) (err error) {
			conn, err := openSession(ctx, b, cfg.Driver)
			if err != nil {
				return err
			}
			defer conn.handle.Commit(context.WithoutCancel(ctx))
			s := &tpccState{Conn: conn}
			ctx = context.WithValue(ctx, contextKey{}, s)
			return newDDLManager(cfg.Parts, cfg.UseFK, cfg.Warehouses, cfg.PartitionType, cfg.UseClusteredIndex).createTables(ctx, cfg.Driver)
		})
	}
	var next atomic.Int64
	d.Execution.Step(action, func(ctx context.Context, b *bench.Bench) (err error) {
		index := int(next.Add(1) - 1)
		workerSeed := base + int64(index)
		if seed == 0 {
			workerSeed = time.Now().UnixNano()
		}
		s := &tpccState{R: rand.New(rand.NewSource(workerSeed)), global: rand.New(rand.NewSource(workerSeed)), Buf: &buffer{}}
		conn, err := openSession(ctx, b, cfg.Driver)
		if err != nil {
			return bench.Fatal(err)
		}
		s.Conn = conn
		ctx = context.WithValue(ctx, contextKey{}, s)
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			err = errors.Join(err, s.Conn.handle.Commit(cleanup))
		}()
		manager := newDDLManager(cfg.Parts, cfg.UseFK, cfg.Warehouses, cfg.PartitionType, cfg.UseClusteredIndex)
		switch action {
		case "cleanup":
			if index == 0 {
				if err := manager.dropTable(ctx); err != nil {
					return bench.Fatal(err)
				}
			}
			return nil
		case "check":
			if err := w.check(ctx, index, cfg.CheckAll); err != nil {
				return bench.Fatal(err)
			}
			return nil
		case "prepare":
			if err := prepareWorkload(ctx, w, cfg.Threads, cfg.Warehouses, index); err != nil {
				return bench.Fatal(err)
			}
			return nil
		}
		runCtx, cancel := context.WithTimeout(ctx, duration)
		defer cancel()
		ctx = context.WithValue(runCtx, contextKey{}, s)
		prepareState(s)
		localDeck := append([]int(nil), deck...)
		// Upstream never advances its deck index; it samples the weighted deck with replacement.
		lastRefresh := time.Now()
		stats[index].started = time.Now()
		defer func() { stats[index].finished = time.Now() }()
		defer func() {
			if value := recover(); value != nil {
				err = fmt.Errorf("panic in go-tpc worker %d: %v", index, value)
			}
		}()
		limit := count / int64(cfg.Threads)
		for iteration := int64(0); limit <= 0 || iteration < limit; iteration++ {
			if ctx.Err() != nil {
				return nil
			}
			if refresh > 0 && time.Since(lastRefresh) >= refresh {
				if err := s.Conn.handle.Commit(ctx); err != nil {
					return bench.Fatal(err)
				}
				conn, err := openSession(ctx, b, cfg.Driver)
				if err != nil {
					return bench.Fatal(err)
				}
				s.Conn = conn
				prepareState(s)
				lastRefresh = time.Now()
			}
			s.Buf.Reset()
			kind := localDeck[s.R.Intn(len(localDeck))]
			if wait {
				if err := pause(ctx, time.Duration(keying[kind]*float64(time.Second))); err != nil {
					return nil
				}
			}
			started := time.Now()
			operations := []func(context.Context, int) error{w.runNewOrder, w.runPayment, w.runOrderStatus, w.runDelivery, w.runStockLevel}
			err := b.LogicalOperation(func() error { return operations[kind](ctx, index) })
			latency.Record(ctx, min(time.Since(started), maxLatency).Seconds(), bench.LabelValue("transaction", names[kind]))
			attempts.Add(ctx, 1, bench.LabelValue("transaction", names[kind]))
			if err == nil {
				stats[index].success[kind]++
			} else {
				stats[index].failed[kind]++
				failures.Add(ctx, 1, bench.LabelValue("transaction", names[kind]))
			}
			if wait {
				delay := math.Min(-math.Log(s.global.Float64())*thinking[kind], thinking[kind]*10)
				if pause(ctx, time.Duration(delay*float64(time.Second))) != nil {
					return nil
				}
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				if !ignore {
					return err
				}
				b.RecordError(names[kind], err)
			}
		}
		return nil
	}, bench.SharedIterations(cfg.Threads, int64(cfg.Threads)))
	if action == "prepare" && !noCheck {
		var nextCheck atomic.Int64
		d.Execution.Step("check_prepare", func(ctx context.Context, b *bench.Bench) (err error) {
			index := int(nextCheck.Add(1) - 1)
			conn, err := openSession(ctx, b, cfg.Driver)
			if err != nil {
				return bench.Fatal(err)
			}
			defer conn.handle.Commit(context.WithoutCancel(ctx))
			ctx = context.WithValue(ctx, contextKey{}, &tpccState{Conn: conn})
			if err := w.check(ctx, index, true); err != nil {
				return bench.Fatal(err)
			}
			return nil
		}, bench.SharedIterations(cfg.Threads, int64(cfg.Threads)))
	}
	return d.Execution.Err()
}

func openSession(ctx context.Context, b *bench.Bench, driver string) (*session, error) {
	handle, err := b.Begin(ctx, bench.BeginOpts{Isolation: bench.IsoConn, Name: "go_tpc_session"})
	if err != nil {
		return nil, err
	}
	return &session{handle: handle, driver: driver}, nil
}
func makeDeck(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 5 {
		return nil, errors.New("weight requires five values")
	}
	var deck []int
	for kind, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 100 {
			return nil, errors.New("weights must be 0..100")
		}
		for range n {
			deck = append(deck, kind)
		}
	}
	if len(deck) != 100 {
		return nil, errors.New("weights must sum to 100")
	}
	return deck, nil
}
func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func prepareState(s *tpccState) {
	groups := []struct {
		target  *map[string]*statement
		queries []string
	}{
		{&s.newOrderStmts, []string{newOrderSelectCustomer, newOrderSelectDistrict, newOrderUpdateDistrict, newOrderInsertOrder, newOrderInsertNewOrder, newOrderUpdateStock}},
		{&s.paymentStmts, []string{paymentUpdateWarehouse, paymentSelectWarehouse, paymentUpdateDistrict, paymentSelectDistrict, paymentSelectCustomerListByLast, paymentSelectCustomerForUpdate, paymentSelectCustomerData, paymentUpdateCustomerWithData, paymentUpdateCustomer, paymentInsertHistory}},
		{&s.orderStatusStmts, []string{orderStatusSelectCustomerCntByLast, orderStatusSelectCustomerByLast, orderStatusSelectCustomerByID, orderStatusSelectLatestOrder, orderStatusSelectOrderLine}},
		{&s.deliveryStmts, []string{deliverySelectNewOrder, deliveryDeleteNewOrder, deliveryUpdateOrder, deliverySelectOrders, deliveryUpdateOrderLine, deliverySelectSumAmount, deliveryUpdateCustomer}},
		{&s.stockLevelStmt, []string{stockLevelSelectDistrict, stockLevelCount}},
	}
	for _, group := range groups {
		*group.target = map[string]*statement{}
		for _, query := range group.queries {
			(*group.target)[query] = prepareStmt("", context.Background(), s.Conn, query)
		}
	}
	for i := 5; i <= 15; i++ {
		for _, query := range []string{newOrderSelectItemSQLs[i], newOrderSelectStockSQLs[i], newOrderInsertOrderLineSQLs[i]} {
			s.newOrderStmts[query] = prepareStmt("", context.Background(), s.Conn, query)
		}
	}
}
func summary(stats []counters, warehouses int) any {
	var success, failed [5]uint64
	var first, last time.Time
	for _, s := range stats {
		for i := range success {
			success[i] += s.success[i]
			failed[i] += s.failed[i]
		}
		if first.IsZero() || s.started.Before(first) {
			first = s.started
		}
		if s.finished.After(last) {
			last = s.finished
		}
	}
	seconds := last.Sub(first).Seconds()
	var tpm float64
	if seconds > 0 {
		tpm = float64(success[0]) * 60 / seconds
	}
	return struct {
		Reference  string    `json:"reference"`
		Successful [5]uint64 `json:"successful"`
		Failed     [5]uint64 `json:"failed"`
		Seconds    float64   `json:"seconds"`
		TPMC       float64   `json:"tpmC"`
	}{"pingcap/go-tpc@a9ca4818625deef91ff80f6c395a575ccae22b7c", success, failed, seconds, tpm}
}
func render(out io.Writer, data json.RawMessage) error {
	_, err := fmt.Fprintf(out, "go-tpc TPC-C: %s\n", data)
	return err
}
