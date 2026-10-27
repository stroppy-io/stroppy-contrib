//go:build integration

package workload

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	_ "github.com/stroppy-io/stroppy/v6/pkg/driver/mysql"
)

func TestMySQLDifferentialClient(t *testing.T) {
	url := os.Getenv("HAMMERDB_MYSQL_DIFFERENTIAL_URL")
	if url == "" {
		t.Skip("set HAMMERDB_MYSQL_DIFFERENTIAL_URL for the Tcl client oracle")
	}
	profile := os.Getenv("HAMMERDB_MYSQL_DIFFERENTIAL_PROFILE")
	test := bench.Test{Name: "mysql-differential", Define: func(d *bench.Def) error {
		d.Drivers.Declare("default", bench.DriverConfig{Kind: bench.DriverMySQL})
		w := &workload{mysql: true, prepared: profile == "prepared", noStoredProcs: profile == "client"}
		d.Execution.Step("queries", func(ctx context.Context, b *bench.Bench) error {
			testTerminal := &terminal{random: newRandom(1)}
			testTerminal.stream = &threadRandom{random: testTerminal.random}
			defer testTerminal.close(context.WithoutCancel(ctx))
			if err := w.initialize(ctx, b, testTerminal); err != nil {
				return err
			}
			session := testTerminal.sessions["default"]
			if err := session.Exec(ctx, "set session rand_seed1=123,rand_seed2=456", nil); err != nil {
				return err
			}
			for range 100 {
				kind := pickTransaction(testTerminal.random.number(1, 23))
				statement := testTerminal.mysqlStatement(kind, time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local))
				var err error
				if w.noStoredProcs {
					err = w.mysqlTransaction(ctx, session, testTerminal, kind, statement)
				} else {
					err = statement.run(ctx, session)
				}
				if err != nil {
					return err
				}
			}
			return nil
		})
		return d.Execution.Err()
	}}
	if _, err := bench.RunTest(t.Context(), test, bench.RunOptions{Drivers: map[string]bench.DriverConfig{"default": {Kind: bench.DriverMySQL, URL: url}}}); err != nil {
		t.Fatal(err)
	}
}
