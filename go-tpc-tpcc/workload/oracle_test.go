//go:build integration

package workload

import (
	"context"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	_ "github.com/stroppy-io/stroppy/v6/pkg/driver/mysql"
	_ "github.com/stroppy-io/stroppy/v6/pkg/driver/postgres"
)

func TestDifferentialOracle(t *testing.T) {
	dsn := os.Getenv("GO_TPC_DSN")
	if dsn == "" {
		t.Skip("GO_TPC_DSN selects a disposable oracle database")
	}
	driver := os.Getenv("GO_TPC_DRIVER")
	kind := bench.DriverMySQL
	if driver == "postgres" {
		kind = bench.DriverPostgres
	}
	warehouses := 2
	if os.Getenv("GO_TPC_ONE_WAREHOUSE") == "true" {
		warehouses = 1
	}
	test := bench.Test{Name: "go-tpc-oracle", Define: func(d *bench.Def) error {
		mode := "cache_statement"
		d.Drivers.Declare("default", bench.DriverConfig{Kind: kind, Postgres: &bench.PostgresConfig{DefaultQueryExecMode: &mode}})
		cfg := &Config{Driver: driver, Threads: 1, Warehouses: warehouses, Parts: 1, PartitionType: PartitionTypeHash}
		r := rand.New(rand.NewSource(1))
		w := &Workloader{cfg: cfg, constants: constants{r.Intn(256), r.Intn(1024), r.Intn(8192)}, initLoadTime: "2020-01-02 03:04:05", now: func() time.Time { return time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local) }}
		d.Execution.Step("oracle", func(ctx context.Context, b *bench.Bench) (err error) {
			conn, err := openSession(ctx, b, driver)
			if err != nil {
				return err
			}
			defer conn.handle.Commit(context.WithoutCancel(ctx))
			s := &tpccState{Conn: conn, R: rand.New(rand.NewSource(1)), global: rand.New(rand.NewSource(1)), Buf: &buffer{}}
			ctx = context.WithValue(ctx, contextKey{}, s)
			if os.Getenv("GO_TPC_ACTION") == "prepare" {
				if err := newDDLManager(1, false, warehouses, PartitionTypeHash, true).createTables(ctx, driver); err != nil {
					return err
				}
				return prepareWorkload(ctx, w, 1, warehouses, 0)
			}
			prepareState(s)
			deck, _ := makeDeck("45,43,4,4,4")
			operations := []func(context.Context, int) error{w.runNewOrder, w.runPayment, w.runOrderStatus, w.runDelivery, w.runStockLevel}
			for range 100 {
				if err := operations[deck[s.R.Intn(100)]](ctx, 0); err != nil {
					return err
				}
			}
			return nil
		})
		return d.Execution.Err()
	}}
	if _, err := bench.RunTest(t.Context(), test, bench.RunOptions{Drivers: map[string]bench.DriverConfig{"default": {Kind: kind, URL: dsn}}}); err != nil {
		t.Fatal(err)
	}
}
