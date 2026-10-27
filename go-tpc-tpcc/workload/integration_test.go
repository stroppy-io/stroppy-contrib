//go:build integration

package workload

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/stroppy-io/stroppy/v6/pkg/bench"
)

func TestDatabaseProfiles(t *testing.T) {
	driver := os.Getenv("GO_TPC_TEST_DRIVER")
	dsn := os.Getenv("GO_TPC_TEST_DSN")
	if dsn == "" {
		t.Fatal("GO_TPC_TEST_DRIVER and GO_TPC_TEST_DSN are required")
	}
	name := fmt.Sprintf("gotpc_port_%d", time.Now().UnixNano())
	kind := bench.DriverMySQL
	var pg *pgx.Conn
	var my *sql.DB
	if driver == "postgres" {
		kind = bench.DriverPostgres
		admin, err := pgx.Connect(t.Context(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close(context.Background())
		if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+name); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
				t.Error(err)
			}
		}()
		cfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Database = name
		dsn = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", cfg.Host, cfg.Port, cfg.User, cfg.Password, name)
		pg, err = pgx.Connect(t.Context(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer pg.Close(context.Background())
	} else if driver == "mysql" {
		admin, err := sql.Open("mysql", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close()
		if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := admin.ExecContext(context.Background(), "DROP DATABASE "+name); err != nil {
				t.Error(err)
			}
		}()
		cfg, err := gomysql.ParseDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		cfg.DBName = name
		dsn = cfg.FormatDSN()
		my, err = sql.Open("mysql", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer my.Close()
	} else {
		t.Fatal("driver must be mysql or postgres")
	}
	test := bench.Test{Name: "gotpc-integration", Define: func(d *bench.Def) error {
		mode := "cache_statement"
		d.Drivers.Declare("default", bench.DriverConfig{Kind: kind, Postgres: &bench.PostgresConfig{DefaultQueryExecMode: &mode}})
		cfg := &Config{Driver: driver, Threads: 1, Warehouses: 2, Parts: 1, PartitionType: PartitionTypeHash}
		r := rand.New(rand.NewSource(1))
		w := &Workloader{cfg: cfg, constants: constants{r.Intn(256), r.Intn(1024), r.Intn(8192)}, now: func() time.Time { return time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local) }, initLoadTime: "2020-01-02 03:04:05"}
		d.Execution.Step("load", func(ctx context.Context, b *bench.Bench) error {
			conn, err := openSession(ctx, b, driver)
			if err != nil {
				return err
			}
			defer conn.handle.Commit(context.WithoutCancel(ctx))
			ctx = context.WithValue(ctx, contextKey{}, &tpccState{Conn: conn, R: rand.New(rand.NewSource(1)), global: rand.New(rand.NewSource(1)), Buf: &buffer{}})
			if err := newDDLManager(1, false, 2, PartitionTypeHash, true).createTables(ctx, driver); err != nil {
				return err
			}
			return prepareWorkload(ctx, w, 1, 2, 0)
		})
		return d.Execution.Err()
	}}
	options := bench.RunOptions{Drivers: map[string]bench.DriverConfig{"default": {Kind: kind, URL: dsn}}, Logger: bench.NewLogger(io.Discard)}
	if _, err := bench.RunTest(t.Context(), test, options); err != nil {
		t.Fatal(err)
	}
	checkFingerprints(t, driver, "load", pg, my)
	test.Define = func(d *bench.Def) error {
		mode := "cache_statement"
		d.Drivers.Declare("default", bench.DriverConfig{Kind: kind, Postgres: &bench.PostgresConfig{DefaultQueryExecMode: &mode}})
		cfg := &Config{Driver: driver, Threads: 1, Warehouses: 2}
		r := rand.New(rand.NewSource(1))
		w := &Workloader{cfg: cfg, constants: constants{r.Intn(256), r.Intn(1024), r.Intn(8192)}, now: func() time.Time { return time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local) }}
		d.Execution.Step("run", func(ctx context.Context, b *bench.Bench) error {
			conn, err := openSession(ctx, b, driver)
			if err != nil {
				return err
			}
			defer conn.handle.Commit(context.WithoutCancel(ctx))
			s := &tpccState{Conn: conn, R: rand.New(rand.NewSource(1)), global: rand.New(rand.NewSource(1)), Buf: &buffer{}}
			prepareState(s)
			ctx = context.WithValue(ctx, contextKey{}, s)
			operations := []func(context.Context, int) error{w.runNewOrder, w.runPayment, w.runOrderStatus, w.runDelivery, w.runStockLevel}
			deck, _ := makeDeck("45,43,4,4,4")
			for range 100 {
				if err := operations[deck[s.R.Intn(100)]](ctx, 0); err != nil {
					return err
				}
			}
			return nil
		})
		return d.Execution.Err()
	}
	if _, err := bench.RunTest(t.Context(), test, options); err != nil {
		t.Fatal(err)
	}
	checkFingerprints(t, driver, "run", pg, my)
	options.Report = &bench.ReportOptions{}
	options.Params.CLI = map[string]string{"warehouses": "2", "vus": "2", "duration": "200ms", "ignore-error": "true", "seed": "7", "conn-refresh-interval": "50ms"}
	if _, err := bench.RunTest(t.Context(), Test, options); err != nil {
		t.Fatal(err)
	}
}

func checkFingerprints(t *testing.T, driver, phase string, pg *pgx.Conn, my *sql.DB) {
	t.Helper()
	expected, err := os.ReadFile(filepath.Join("../tools/expected", driver+"-"+phase+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	if pg != nil {
		query, err := os.ReadFile("../tools/fingerprint.sql")
		if err != nil {
			t.Fatal(err)
		}
		rows, err := pg.Query(t.Context(), string(query))
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got strings.Builder
		for rows.Next() {
			var name, hash string
			var count int64
			if err := rows.Scan(&name, &count, &hash); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&got, "%s|%d|%s\n", name, count, hash)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if got.String() != string(expected) {
			t.Fatalf("PostgreSQL %s fingerprints differ:\n%s", phase, got.String())
		}
		return
	}
	order := map[string]string{"customer": "c_w_id,c_d_id,c_id", "district": "d_w_id,d_id", "history": "h_c_w_id,h_c_d_id,h_c_id,h_amount,h_data", "item": "i_id", "new_order": "no_w_id,no_d_id,no_o_id", "order_line": "ol_w_id,ol_d_id,ol_o_id,ol_number", "orders": "o_w_id,o_d_id,o_id", "stock": "s_w_id,s_i_id", "warehouse": "w_id"}
	for _, line := range strings.Split(strings.TrimSpace(string(expected)), "\n") {
		want := strings.Split(line, "|")
		table := want[0]
		columns, err := my.QueryContext(t.Context(), "SELECT column_name FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? ORDER BY ordinal_position", table)
		if err != nil {
			t.Fatal(err)
		}
		var expressions []string
		for columns.Next() {
			var col string
			if err := columns.Scan(&col); err != nil {
				t.Fatal(err)
			}
			if col == "id" || col == "c_since" || col == "h_date" || col == "o_entry_d" || col == "ol_delivery_d" {
				continue
			}
			expressions = append(expressions, "IFNULL(HEX(`"+col+"`),'NULL')")
		}
		columns.Close()
		rows, err := my.QueryContext(t.Context(), "SELECT CONCAT_WS('|',"+strings.Join(expressions, ",")+") FROM `"+table+"` ORDER BY "+order[table])
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.New()
		var count int
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(hash, row)
			count++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if fmt.Sprint(count) != want[1] || hex.EncodeToString(hash.Sum(nil)) != want[2] {
			t.Fatalf("MySQL %s %s fingerprint differs", phase, table)
		}
	}
}
