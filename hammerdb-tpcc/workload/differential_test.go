//go:build integration

package workload

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	_ "github.com/stroppy-io/stroppy/v6/pkg/driver/postgres"
)

// Run against a restored copy of the same initial database used by tools/drive.tcl.
// Setting the PostgreSQL seed is confined to this oracle, never the benchmark.
func TestDifferentialClient(t *testing.T) {
	url := os.Getenv("HAMMERDB_DIFFERENTIAL_URL")
	if url == "" {
		t.Skip("set HAMMERDB_DIFFERENTIAL_URL to compare with tools/drive.tcl")
	}
	procedures := os.Getenv("HAMMERDB_DIFFERENTIAL_PROCEDURES") == "true"
	mode := "cache_statement"
	test := bench.Test{Name: "hammerdb-differential", Define: func(d *bench.Def) error {
		d.Drivers.Declare("default", bench.DriverConfig{Kind: bench.DriverPostgres, Postgres: &bench.PostgresConfig{DefaultQueryExecMode: &mode}})
		w := &workload{storedprocs: procedures}
		d.Execution.Step("queries", func(ctx context.Context, b *bench.Bench) error {
			testTerminal := &terminal{random: newRandom(1)}
			testTerminal.stream = &threadRandom{random: testTerminal.random}
			defer testTerminal.close(context.WithoutCancel(ctx))
			if err := w.initialize(ctx, b, testTerminal); err != nil {
				return err
			}
			session := testTerminal.sessions["default"]
			if err := session.Exec(ctx, "select setseed(0.5)", nil); err != nil {
				return err
			}
			for range 100 {
				kind := pickTransaction(testTerminal.random.number(1, 23))
				statement := testTerminal.statement(kind, time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local))
				if err := session.Exec(ctx, statement.text, statement.args); err != nil {
					b.RecordError(transactionNames[kind], err)
				}
			}
			return nil
		})
		return d.Execution.Err()
	}}
	_, err := bench.RunTest(t.Context(), test, bench.RunOptions{Drivers: map[string]bench.DriverConfig{"default": {Kind: bench.DriverPostgres, URL: url}}})
	if err != nil {
		t.Fatal(err)
	}
}
