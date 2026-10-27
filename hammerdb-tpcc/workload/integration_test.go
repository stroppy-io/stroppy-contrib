//go:build integration

package workload

import (
	"context"
	"fmt"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	_ "github.com/stroppy-io/stroppy/v6/pkg/driver/postgres"
)

// The tagged suite needs a disposable PostgreSQL server with CREATEDB privileges.
// Every database created here has a unique name and is removed by its own cleanup.
func TestPostgresProfiles(t *testing.T) {
	url := os.Getenv("HAMMERDB_PG_URL")
	if url == "" {
		t.Fatal("HAMMERDB_PG_URL is required for the integration suite")
	}
	admin, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	for _, procedures := range []bool{false, true} {
		t.Run(fmt.Sprintf("procedures=%v", procedures), func(t *testing.T) {
			name := fmt.Sprintf("hammerdb_port_%d", time.Now().UnixNano())
			if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
					t.Error(err)
				}
			}()
			connection, err := pgx.ParseConfig(url)
			if err != nil {
				t.Fatal(err)
			}
			connection.Database = name
			connection.RuntimeParams["application_name"] = "hammerdb-port-integration"
			connectionString := connection.ConnString()
			if strings.Contains(connectionString, "://") {
				parsed, err := urlpkg.Parse(connectionString)
				if err != nil {
					t.Fatal(err)
				}
				parsed.Path = "/" + name
				query := parsed.Query()
				query.Del("dbname")
				parsed.RawQuery = query.Encode()
				connectionString = parsed.String()
			} else {
				connectionString += " dbname='" + name + "'"
			}
			options := bench.RunOptions{Report: &bench.ReportOptions{}, Drivers: map[string]bench.DriverConfig{"default": {Kind: bench.DriverPostgres, URL: connectionString}}, Logger: bench.NewLogger(os.Stderr), Params: bench.ParamInputs{CLI: map[string]string{"create": "true", "storedprocs": fmt.Sprint(procedures), "mode": "test", "iterations": "1"}}, Steps: []string{"create_schema", "load_items", "load_data", "create_indexes", "create_routines", "analyze"}}
			if _, err := bench.RunTest(t.Context(), Test, options); err != nil {
				t.Fatal(err)
			}
			database, err := pgx.ConnectConfig(t.Context(), connection)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close(context.Background())
			for table, expected := range map[string]int64{"warehouse": 1, "district": 10, "item": 100000, "stock": 100000, "customer": 30000, "history": 30000, "orders": 30000, "new_order": 9000, "order_line": 298934} {
				var count int64
				if err := database.QueryRow(t.Context(), "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != expected {
					t.Fatalf("%s=%d, want %d", table, count, expected)
				}
			}
			hashes := map[string]string{
				"warehouse": "5b0469db8c29d55c3099e81f0daabb6c", "district": "9d4efad2c043180dfd2c8ce844181fbf",
				"item": "a2e923d05907cd187c39f41bc2a33442", "stock": "3e4435310750c5e052862505b6c9410d",
				"customer": "e8b54c0742844e230aa3802784cffeeb", "history": "7108ed7f77ce0c5c72f48bddd6fe01c6",
				"orders": "a37e76bd65a01dd3e764dec40597c2f4", "new_order": "13ee8ed10cb8b5621bc08189b4ba2636",
				"order_line": "6dd4402a32a4e75d9dda979ee272adda",
			}
			queries, err := os.ReadFile("../tools/fingerprint.sql")
			if err != nil {
				t.Fatal(err)
			}
			rows, err := database.Query(t.Context(), string(queries))
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var table, hash string
				var count int64
				if err := rows.Scan(&table, &count, &hash); err != nil {
					t.Fatal(err)
				}
				if hash != hashes[table] {
					t.Fatalf("%s differs from HammerDB v6.1 seed=1: %s", table, hash)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			options.Steps = nil
			options.Params.CLI = map[string]string{"mode": "test", "iterations": "100", "storedprocs": fmt.Sprint(procedures), "timeprofile": "true"}
			run, err := bench.RunTest(t.Context(), Test, options)
			if err != nil {
				t.Fatal(err)
			}
			if run.Errors.RetryAttempts != 0 {
				t.Fatal("unexpected retry", run.Errors)
			}
			options.Params.CLI = map[string]string{"mode": "timed", "storedprocs": fmt.Sprint(procedures), "vus": "2", "rampup": "20ms", "duration": "100ms", "async-scale": "true", "async-client": "2", "async-delay": "1ms", "allwarehouse": "true"}
			if _, err := bench.RunTest(t.Context(), Test, options); err != nil {
				t.Fatal(err)
			}
			if !procedures {
				path := filepath.Join(t.TempDir(), "pgcpool.xml")
				if err := os.WriteFile(path, []byte(poolXML(connection.Host, strconv.Itoa(int(connection.Port)), connection.User, connection.Password, name)), 0600); err != nil {
					t.Fatal(err)
				}
				options.Params.CLI = map[string]string{"mode": "test", "iterations": "100", "connect-pool": path}
				if _, err := bench.RunTest(t.Context(), Test, options); err != nil {
					t.Fatal(err)
				}
			}
			// Each persistent client session must be released before the next invocation.
			var active int
			deadline := time.Now().Add(time.Second)
			for {
				if err := database.QueryRow(t.Context(), "SELECT count(*) FROM pg_stat_activity WHERE datname=$1 AND pid<>pg_backend_pid()", name).Scan(&active); err != nil {
					t.Fatal(err)
				}
				if active == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("client sessions leaked", active)
				}
				time.Sleep(time.Millisecond)
			}
			if procedures {
				for kind := range transactionNames {
					terminal := &terminal{random: newRandom(7), home: 1, district: 1, warehouses: 1, procedures: true}
					statement := terminal.statement(kind, time.Now())
					if !strings.HasPrefix(statement.text, "call ") {
						t.Fatal(statement.text)
					}
					if _, err := database.Exec(t.Context(), statement.text); err != nil {
						t.Fatalf("%s: %v", transactionNames[kind], err)
					}
				}
			}
		})
	}
}
