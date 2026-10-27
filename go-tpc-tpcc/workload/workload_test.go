package workload

import (
	"context"
	"database/sql"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	"github.com/stroppy-io/stroppy/v6/pkg/bench/testkit"
	"github.com/stroppy-io/stroppy/v6/pkg/record"
)

func TestWeightedSelectionAndRanges(t *testing.T) {
	deck, err := makeDeck("45,43,4,4,4")
	if err != nil {
		t.Fatal(err)
	}
	var counts [5]int
	for _, kind := range deck {
		counts[kind]++
	}
	if counts != [5]int{45, 43, 4, 4, 4} {
		t.Fatal(counts)
	}
	for _, raw := range []string{"1,2", "-1,43,4,4,50", "1,1,1,1,1"} {
		if _, err := makeDeck(raw); err == nil {
			t.Fatal(raw)
		}
	}
	r := rand.New(rand.NewSource(1))
	before := r.Int63()
	r = rand.New(rand.NewSource(1))
	if randInt(r, 4, 4) != 4 || r.Int63() != before {
		t.Fatal("equal bounds consumed randomness")
	}
}
func TestBinderPreservesBatchSQL(t *testing.T) {
	for n := 5; n <= 15; n++ {
		queries := []struct {
			sql   string
			count int
		}{{newOrderSelectItemSQLs[n], n}, {newOrderSelectStockSQLs[n], n * 2}, {newOrderInsertOrderLineSQLs[n], n * 9}}
		for _, query := range queries {
			args := make([]any, query.count)
			for i := range args {
				args[i] = i
			}
			text, values, err := bind(query.sql, args)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(text, "?") || len(values) != query.count {
				t.Fatal(text)
			}
		}
	}
}
func TestScanNullableAndTypedValues(t *testing.T) {
	var id int
	var amount float64
	var name string
	var nullable sql.NullString
	if err := scan([]any{int64(5), "3.14", []byte("name"), nil}, []any{&id, &amount, &name, &nullable}); err != nil {
		t.Fatal(err)
	}
	if id != 5 || amount != 3.14 || name != "name" || nullable.Valid {
		t.Fatal(id, amount, name, nullable)
	}
	if scan([]any{nil}, []any{&name}) == nil {
		t.Fatal("NULL accepted into string")
	}
}
func TestSDKTransactionsRollbackAndNoRetry(t *testing.T) {
	r := &record.Recorder{}
	raw, _, _ := bind(stockLevelSelectDistrict, []any{1, 1})
	for range 8 {
		r.Reply(raw, record.Response{Columns: []string{"d_next_o_id"}, Rows: [][]any{{int64(3001)}}})
	}
	query, _, _ := bind(stockLevelCount, []any{1, 1, 3001, 3001, 1, 10})
	for range 8 {
		r.Reply(query, record.Response{Err: errors.New("serialization failure")})
	}
	run, err := testkit.Record(t.Context(), Test, r, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"warehouses": "1", "count": "4", "weight": "0,0,0,0,100", "ignore-error": "true", "seed": "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Errors.TerminalErrors != 4 || run.Errors.RetryAttempts != 0 {
		t.Fatal(run.Errors)
	}
	var begins, rollbacks int
	for _, op := range r.Operations() {
		if op.SQL == "BEGIN" {
			begins++
		}
		if op.SQL == "ROLLBACK" {
			rollbacks++
		}
	}
	if begins != 4 || rollbacks != 4 {
		t.Fatal(begins, rollbacks)
	}
}
func TestExpectedNewOrderRollback(t *testing.T) {
	r := &record.Recorder{}
	query, _, _ := bind(newOrderSelectCustomer, []any{1, 1, 1})
	r.Reply(query, record.Response{Columns: []string{"c_discount", "c_last", "c_credit", "w_tax"}, Rows: [][]any{{0.1, "BARBARBAR", "GC", 0.1}}})
	query, _, _ = bind(newOrderSelectDistrict, []any{1, 1})
	r.Reply(query, record.Response{Columns: []string{"d_next_o_id", "d_tax"}, Rows: [][]any{{int64(3001), 0.1}}})
	seed := int64(1)
	for ; seed < 10000; seed++ {
		random := rand.New(rand.NewSource(seed))
		randInt(random, 1, 1)
		randInt(random, 1, 10)
		randCustomerID(random, 1)
		randInt(random, 5, 15)
		if randInt(random, 1, 100) == 1 {
			break
		}
	}
	random := rand.New(rand.NewSource(seed))
	randInt(random, 1, 1)
	randInt(random, 1, 10)
	randCustomerID(random, 1)
	lines := randInt(random, 5, 15)
	randInt(random, 1, 100)
	selected := map[int]bool{}
	var itemRows [][]any
	for i := 0; i < lines-1; i++ {
		var id int
		for {
			id = randItemID(random, 0)
			if !selected[id] {
				selected[id] = true
				break
			}
		}
		randInt(random, 1, 10)
		itemRows = append(itemRows, []any{1.0, "item", "data", int64(id)})
	}
	args := make([]any, lines)
	query, _, _ = bind(newOrderSelectItemSQLs[lines], args)
	r.Reply(query, record.Response{Columns: []string{"i_price", "i_name", "i_data", "i_id"}, Rows: itemRows})
	test := bench.Test{Name: "rollback", Define: func(d *bench.Def) error {
		d.Execution.Step("transaction", func(ctx context.Context, b *bench.Bench) error {
			conn, err := openSession(ctx, b, "mysql")
			if err != nil {
				return err
			}
			defer conn.handle.Commit(ctx)
			s := &tpccState{Conn: conn, R: rand.New(rand.NewSource(seed)), Buf: &buffer{}}
			prepareState(s)
			ctx = context.WithValue(ctx, contextKey{}, s)
			w := &Workloader{cfg: &Config{Warehouses: 1, Driver: "mysql"}, constants: constants{customer: 1}, now: time.Now}
			return w.runNewOrder(ctx, 0)
		})
		return d.Execution.Err()
	}}
	if _, err := testkit.Record(t.Context(), test, r, bench.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	rolled := false
	for _, op := range r.Operations() {
		if op.SQL == "COMMIT" {
			t.Fatal("expected rollback committed")
		}
		if op.SQL == "ROLLBACK" {
			rolled = true
		}
	}
	if !rolled {
		t.Fatal("rollback not sent")
	}
}

func TestStopOnError(t *testing.T) {
	r := &record.Recorder{}
	run, err := testkit.Record(t.Context(), Test, r, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"warehouses": "1", "count": "10", "weight": "0,0,0,0,100"}}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Errors.TerminalErrors != 1 {
		t.Fatal(run.Errors)
	}
	begins := 0
	for _, op := range r.Operations() {
		if op.SQL == "BEGIN" {
			begins++
		}
	}
	if begins != 1 {
		t.Fatal("worker did not stop", begins)
	}
	if run.Status != "completed_with_errors" {
		t.Fatal(run.Status)
	}
}
func TestCountDividedPerWorker(t *testing.T) {
	r := &record.Recorder{}
	district, _, _ := bind(stockLevelSelectDistrict, []any{1, 1})
	stock, _, _ := bind(stockLevelCount, []any{1, 1, 3001, 3001, 1, 10})
	for range 4 {
		r.Reply(district, record.Response{Columns: []string{"d_next_o_id"}, Rows: [][]any{{int64(3001)}}})
		r.Reply(stock, record.Response{Columns: []string{"stock_count"}, Rows: [][]any{{int64(10)}}})
	}
	_, err := testkit.Record(t.Context(), Test, r, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"warehouses": "1", "vus": "2", "count": "5", "weight": "0,0,0,0,100"}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, op := range r.Operations() {
		if op.SQL == stock {
			calls++
		}
	}
	if calls != 4 {
		t.Fatal(calls)
	}
}
func TestDDLShapes(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		r := &record.Recorder{}
		test := bench.Test{Name: "ddl", Define: func(d *bench.Def) error {
			d.Execution.Step("create", func(ctx context.Context, b *bench.Bench) error {
				conn, err := openSession(ctx, b, driver)
				if err != nil {
					return err
				}
				defer conn.handle.Commit(ctx)
				ctx = context.WithValue(ctx, contextKey{}, &tpccState{Conn: conn})
				return newDDLManager(1, true, 2, PartitionTypeHash, true).createTables(ctx, driver)
			})
			return d.Execution.Err()
		}}
		if _, err := testkit.Record(t.Context(), test, r, bench.RunOptions{}); err != nil {
			t.Fatal(err)
		}
		creates := 0
		for _, op := range r.Operations() {
			if strings.Contains(op.SQL, "CREATE TABLE") {
				creates++
			}
		}
		if creates != 9 {
			t.Fatal(driver, creates)
		}
	}
	for _, kind := range []int{PartitionTypeHash, PartitionTypeRange, PartitionTypeListAsHash, PartitionTypeListAsRange} {
		ddl := newDDLManager(2, false, 4, kind, true).appendPartition("CREATE TABLE x (w_id INT)", "w_id")
		if !strings.Contains(ddl, "PARTITION") {
			t.Fatal(ddl)
		}
	}
}

func TestPacingCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err := testkit.Record(ctx, Test, &record.Recorder{}, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"wait": "true", "count": "1"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestLoadBatchFailureIsNotHidden(t *testing.T) {
	r := &record.Recorder{}
	r.Reply("INSERT INTO x VALUES (1)", record.Response{Err: errors.New("sentinel")})
	test := bench.Test{Name: "load-error", Define: func(d *bench.Def) error {
		d.Execution.Step("load", func(ctx context.Context, b *bench.Bench) error {
			conn, err := openSession(ctx, b, "mysql")
			if err != nil {
				return err
			}
			defer conn.handle.Commit(ctx)
			sink := newLoadSink(conn, "INSERT INTO x VALUES ", 0, 0)
			if err := sink.WriteRow(ctx, 1); err != nil {
				return err
			}
			return sink.Flush(ctx)
		})
		return d.Execution.Err()
	}}
	if _, err := testkit.Record(t.Context(), test, r, bench.RunOptions{}); err == nil {
		t.Fatal("final batch failure hidden")
	}
}
