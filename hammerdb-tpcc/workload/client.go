package workload

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
)

var transactionNames = [...]string{"new_order", "payment", "delivery", "stock_level", "order_status"}
var keySeconds = [...]int{18, 3, 2, 2, 2}
var thinkSeconds = [...]int{12, 12, 10, 5, 5}

// The quoted $8 and $5 are literal strings in HammerDB v6.1's prepared SQL.
// The corresponding client draws still occur, although those arguments are unused.
var functionSQL = [...]string{
	`select neword(:warehouse::integer, :maximum::integer, :district::integer, :customer::integer, :lines::integer,0)`,
	`select payment(:warehouse::integer, :district::integer, :customer_warehouse::integer, :customer_district::integer, :customer::integer, :byname::integer, :amount::numeric,'$8','0',0)`,
	`select delivery(:warehouse::integer, :carrier::integer)`,
	`select slev(:warehouse::integer, :district::integer, :threshold::integer)`,
	`select * from ostat(:warehouse::integer, :district::integer, :customer::integer, :byname::integer,'$5') as (ol_i_id INTEGER, ol_supply_w_id INTEGER, ol_quantity SMALLINT, ol_amount NUMERIC, ol_delivery_d TIMESTAMP WITH TIME ZONE, out_os_c_id INTEGER, out_os_c_last CHARACTER VARYING, os_c_first CHARACTER VARYING, os_c_middle CHARACTER VARYING, os_c_balance NUMERIC, os_o_id INTEGER, os_entdate TIMESTAMP, os_o_carrier_id INTEGER)`,
}

const detectSQL = `SELECT p.prokind, count(*) AS cnt FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'public' AND p.proname IN ('neword','payment','delivery','ostat','slev') GROUP BY p.prokind`

type terminal struct {
	random          *random
	stream          *threadRandom
	client          int
	home            int
	district        int
	warehouses      int
	procedures      bool
	counts          [5]uint64
	sessions        map[string]*bench.Tx
	owner           int
	mysql, prepared bool
	preparedSQL     map[*bench.Tx][]string
}

func pickTransaction(choice int) int {
	switch {
	case choice <= 10:
		return 0
	case choice <= 20:
		return 1
	case choice == 21:
		return 2
	case choice == 22:
		return 3
	default:
		return 4
	}
}

type call struct {
	text string
	args map[string]any
}

func (t *terminal) statement(kind int, now time.Time) call {
	r := t.random
	stamp := "TO_TIMESTAMP('" + now.Format("20060102150405") + "','YYYYMMDDHH24MISS')::timestamp without time zone"
	switch kind {
	case 0:
		district, customer, lines := r.number(1, 10), r.number(1, 3000), r.number(5, 15)
		if t.procedures {
			return call{text: fmt.Sprintf("call neword(%d,%d,%d,%d,%d,0.0,'','',0.0,0.0,0,%s)", t.home, t.warehouses, district, customer, lines, stamp)}
		}
		return call{functionSQL[kind], map[string]any{"warehouse": t.home, "maximum": t.warehouses, "district": district, "customer": customer, "lines": lines}}
	case 1:
		district, x, y := r.number(1, 10), r.number(1, 100), r.number(1, 100)
		customerDistrict, customerWarehouse := district, t.home
		if x > 85 {
			customerDistrict, customerWarehouse = r.number(1, 10), r.number(1, t.warehouses)
			for customerWarehouse == t.home && t.warehouses != 1 {
				customerWarehouse = r.number(1, t.warehouses)
			}
		}
		name, customer := lastname(r.nurand()), r.number(1, 3000)
		byname := 1
		if y > 60 {
			byname, name = 0, ""
		}
		amount := r.number(1, 5000)
		if t.procedures {
			return call{text: fmt.Sprintf("call payment(%d,%d,%d,%d,%d,%d,'0','%s',%d,'','','','','','','','','','','','','','','','','','',%s,0.0,0.0,0.0,'',%s)", t.home, district, customerWarehouse, customerDistrict, byname, amount, name, customer, stamp, stamp)}
		}
		return call{functionSQL[kind], map[string]any{"warehouse": t.home, "district": district, "customer_warehouse": customerWarehouse, "customer_district": customerDistrict, "customer": customer, "byname": byname, "amount": amount}}
	case 2:
		carrier := r.number(1, 10)
		if t.procedures {
			return call{text: fmt.Sprintf("call delivery(%d,%d,%s)", t.home, carrier, stamp)}
		}
		return call{functionSQL[kind], map[string]any{"warehouse": t.home, "carrier": carrier}}
	case 3:
		threshold := r.number(10, 20)
		if t.procedures {
			return call{text: fmt.Sprintf("call slev(%d,%d,%d,0)", t.home, t.district, threshold)}
		}
		return call{functionSQL[kind], map[string]any{"warehouse": t.home, "district": t.district, "threshold": threshold}}
	default:
		district := r.number(1, 10)
		name, customer, y := lastname(r.nurand()), r.number(1, 3000), r.number(1, 100)
		byname := 1
		if y > 60 {
			byname, name = 0, ""
		}
		if t.procedures {
			return call{text: fmt.Sprintf("call ostat(%d,%d,%d,%d,'%s','','',0.0,0,%s,0,'')", t.home, district, customer, byname, name, stamp)}
		}
		return call{functionSQL[kind], map[string]any{"warehouse": t.home, "district": district, "customer": customer, "byname": byname}}
	}
}

func (w *workload) connect(ctx context.Context, b *bench.Bench, name string) (*bench.Tx, error) {
	database := b
	if name != "default" {
		database = b.Database(w.connections[name]).Bench
	}
	session, err := database.Begin(ctx, bench.BeginOpts{Isolation: bench.IsoConn, Name: "hammerdb_session"})
	if err != nil {
		return nil, err
	}
	setting := "set CLIENT_MIN_MESSAGES TO 'ERROR'"
	if w.mysql {
		setting = "set autocommit=0"
	}
	if err = session.Exec(ctx, setting, nil); err != nil {
		return nil, errors.Join(err, session.Rollback(context.WithoutCancel(ctx)))
	}
	return session, nil
}

func detectMode(ctx context.Context, b *bench.Bench, session *bench.Tx, requested bool) bool {
	type routineCount struct {
		Kind  string `db:"prokind"`
		Count int64  `db:"cnt"`
	}
	rows, err := session.QueryValues[routineCount](ctx, detectSQL, nil)
	if err != nil {
		b.Log.Warn("Routine detection failed; keeping requested mode", "error", err)
		return requested
	}
	var functions, procedures int64
	for _, row := range rows {
		if row.Kind == "f" {
			functions = row.Count
		} else if row.Kind == "p" {
			procedures = row.Count
		}
	}
	if functions == 5 && procedures == 0 {
		return false
	}
	if procedures == 5 && functions == 0 {
		return true
	}
	return requested
}

func (w *workload) initialize(ctx context.Context, b *bench.Bench, t *terminal) error {
	t.mysql, t.prepared = w.mysql, w.prepared
	t.sessions = make(map[string]*bench.Tx)
	names := []string{"default"}
	if w.pool != nil {
		names = w.pool.names
	}
	for _, name := range names {
		session, err := w.connect(ctx, b, name)
		if err != nil {
			return err
		}
		t.sessions[name] = session
	}
	first := t.sessions[names[0]]
	if w.mysql {
		if w.prepared {
			t.preparedSQL = make(map[*bench.Tx][]string)
			for _, session := range t.sessions {
				for _, statement := range assetSQL("mysql_prepare.sql").Section("prepare") {
					if err := session.Exec(ctx, statement, nil); err != nil {
						return err
					}
					t.preparedSQL[session] = append(t.preparedSQL[session], strings.Fields(statement)[1])
				}
			}
		}
	} else {
		t.procedures = detectMode(ctx, b, first, w.storedprocs)
		if w.pool != nil {
			t.procedures = false
		}
	}
	metadata := first
	if w.pool != nil {
		var err error
		metadata, err = w.connect(ctx, b, "default")
		if err != nil {
			return err
		}
		t.sessions["default"] = metadata
	}
	var err error
	t.warehouses, err = metadata.QueryValue[int](ctx, "select max(w_id) from warehouse", nil)
	if err != nil {
		return err
	}
	if t.warehouses < 1 {
		return errors.New("HammerDB schema has no warehouses")
	}
	t.stream.Lock()
	t.home = t.random.number(1, t.warehouses)
	t.stream.Unlock()
	districts, err := metadata.QueryValue[int](ctx, "select max(d_id) from district", nil)
	if err != nil {
		return err
	}
	if districts < 1 {
		return errors.New("HammerDB schema has no districts")
	}
	t.stream.Lock()
	t.district = t.random.number(1, districts)
	t.stream.Unlock()
	return nil
}

func (t *terminal) close(ctx context.Context) error {
	var err error
	for _, session := range t.sessions {
		if t.mysql {
			for _, name := range t.preparedSQL[session] {
				err = errors.Join(err, session.Exec(ctx, "deallocate prepare "+name, nil))
			}
			err = errors.Join(err, session.Exec(ctx, "ROLLBACK", nil), session.Exec(ctx, "set autocommit=1", nil))
		}
		err = errors.Join(err, session.Commit(ctx))
	}
	return err
}

func allWarehouses(owner, workers, warehouses int) []int {
	count := (warehouses + workers - 1) / workers
	assigned := make([]int, count)
	for i := range assigned {
		assigned[i] = (owner+i*workers)%warehouses + 1
	}
	return assigned
}

func (w *workload) runClient(ctx context.Context, b *bench.Bench, t *terminal, stop <-chan struct{}) (err error) {
	if w.asyncScale {
		client := t.client%w.asyncClients + 1
		if err := sleep(ctx, time.Duration(client)*w.asyncDelay); err != nil {
			return err
		}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		err = errors.Join(err, t.close(cleanup))
	}()
	if err := w.initialize(ctx, b, t); err != nil {
		return err
	}
	assigned := allWarehouses(t.owner, w.workers, t.warehouses)
	for iteration := int64(0); iteration < w.iterations; iteration++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		default:
		}
		t.stream.Lock()
		if w.allwarehouse {
			t.home = assigned[t.random.number(1, len(assigned))-1]
		}
		kind := pickTransaction(t.random.number(1, 23))
		t.stream.Unlock()
		if w.keyandthink {
			if err := sleep(ctx, time.Duration(keySeconds[kind])*time.Second); err != nil {
				return err
			}
		}
		name := transactionNames[kind]
		if w.mode == "test" || w.asyncVerbose {
			b.Log.Info("HammerDB transaction", "transaction", name, "warehouse", t.home)
		}
		t.stream.Lock()
		session := t.sessions["default"]
		if w.pool != nil {
			session = t.sessions[w.pool.pick(kind, t.counts[kind], t.random)]
		}
		var statement call
		var mysqlCall mysqlCall
		if w.mysql {
			mysqlCall = t.mysqlStatement(kind, time.Now())
		} else {
			statement = t.statement(kind, time.Now())
		}
		t.stream.Unlock()
		started := time.Now()
		err := b.LogicalOperation(func() error {
			if w.mysql {
				if w.noStoredProcs && w.pool == nil {
					return w.mysqlTransaction(ctx, session, t, kind, mysqlCall)
				}
				return mysqlCall.run(ctx, session)
			}
			return session.Exec(ctx, statement.text, statement.args)
		})
		t.counts[kind]++
		w.attempts.Add(ctx, 1, bench.LabelValue("transaction", name))
		if w.timeprofile {
			w.latency.Record(ctx, time.Since(started).Seconds(), bench.LabelValue("transaction", name))
		}
		if err != nil {
			w.failures.Add(ctx, 1, bench.LabelValue("transaction", name))
			if ctx.Err() != nil {
				return err
			}
			if w.raiseerror || w.mysql && w.noStoredProcs && w.pool == nil {
				return err
			}
			b.RecordError(name, err)
		}
		if w.keyandthink {
			t.stream.Lock()
			delay := t.random.think(thinkSeconds[kind])
			t.stream.Unlock()
			if err := sleep(ctx, delay); err != nil {
				return err
			}
		}
	}
	return nil
}

func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
