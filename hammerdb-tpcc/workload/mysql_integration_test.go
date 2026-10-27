//go:build integration

package workload

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	_ "github.com/stroppy-io/stroppy/v6/pkg/driver/mysql"
)

func TestMySQLProfiles(t *testing.T) {
	dsn := os.Getenv("HAMMERDB_MYSQL_URL")
	if dsn == "" {
		t.Fatal("HAMMERDB_MYSQL_URL is required for MySQL integration")
	}
	config, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("hammerdb_port_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE `"+name+"` CHARACTER SET latin1 COLLATE latin1_swedish_ci"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE `"+name+"`"); err != nil {
			t.Error(err)
		}
	}()
	config.DBName, config.ParseTime = name, true
	database, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	options := bench.RunOptions{Report: &bench.ReportOptions{}, Logger: bench.NewLogger(io.Discard), Drivers: map[string]bench.DriverConfig{"default": {Kind: bench.DriverMySQL, URL: config.FormatDSN()}}, Params: bench.ParamInputs{CLI: map[string]string{"create": "true", "mode": "test", "iterations": "1"}}, Steps: []string{"create_schema", "load_items", "load_data", "create_indexes", "create_routines", "analyze"}}
	if _, err := bench.RunTest(t.Context(), Test, options); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"customer":   "e16ae1f7bb2f7cff28f35c22ec2da77c0098db8536dd7b130a5a2bfdf1343ee2",
		"district":   "b3e6825116fd3d0bd2b18f1bbfc5e0d3516c0f712e0e81b7191dd06704720e66",
		"history":    "c4d3eb93b09e5dc4ec1b351b3cbe21ebbae537572dbfcf3331f3e7639a715e7f",
		"item":       "56c8d0e3be58529d41620faed1bee5aa826d3565315f0cdfc8fa9bf5e79d71a2",
		"new_order":  "56eacffc4e94d1ec120ccd458305db4cc1967912c8d23b8ea7f2be039838ccc5",
		"order_line": "f69c2ef6c5b1f2d801d0445154f9e01dd3184efaac6211ff49010ef030fe75f7",
		"orders":     "22fcfae74952a4acf1fd26ea93263d409709197ce906cf4bd22e6c22684f72d7",
		"stock":      "5101aa68919b066ccca0bd8f7dc99fd720a9b20d592f1154819569c6bee890a4",
		"warehouse":  "f27cd9ede2d61fc36ea3981774d21a13f437a5c49b43b934ad1e6f6efc57eacd",
	}
	orders := map[string]string{"warehouse": "w_id", "district": "d_w_id,d_id", "item": "i_id", "stock": "s_w_id,s_i_id", "customer": "c_w_id,c_d_id,c_id", "history": "h_c_w_id,h_c_d_id,h_c_id,h_amount,h_data", "orders": "o_w_id,o_d_id,o_id", "new_order": "no_w_id,no_d_id,no_o_id", "order_line": "ol_w_id,ol_d_id,ol_o_id,ol_number"}
	for table, want := range expected {
		columns, err := database.QueryContext(t.Context(), "SELECT COLUMN_NAME FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? ORDER BY ORDINAL_POSITION", table)
		if err != nil {
			t.Fatal(err)
		}
		var expressions []string
		for columns.Next() {
			var column string
			if err := columns.Scan(&column); err != nil {
				t.Fatal(err)
			}
			if column == "c_since" || column == "h_date" || column == "o_entry_d" || column == "ol_delivery_d" {
				continue
			}
			expressions = append(expressions, "IFNULL(HEX(`"+column+"`),'NULL')")
		}
		columns.Close()
		rows, err := database.QueryContext(t.Context(), "SELECT CONCAT_WS('|',"+strings.Join(expressions, ",")+") FROM `"+table+"` ORDER BY "+orders[table])
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.New()
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(hash, value)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if got := hex.EncodeToString(hash.Sum(nil)); got != want {
			t.Fatalf("%s differs from HammerDB Tcl seed=1: %s", table, got)
		}
	}
	for _, profile := range []string{"text", "prepared", "client"} {
		t.Run(profile, func(t *testing.T) {
			options.Steps = nil
			options.Params.CLI = map[string]string{"mode": "test", "iterations": "100", "prepared": fmt.Sprint(profile == "prepared"), "no-stored-procs": fmt.Sprint(profile == "client")}
			run, err := bench.RunTest(t.Context(), Test, options)
			if err != nil {
				t.Fatal(err)
			}
			if run.Errors.TerminalErrors != 0 || run.Errors.RetryAttempts != 0 {
				t.Fatal(run.Errors)
			}
		})
	}
	options.Params.CLI = map[string]string{"mode": "timed", "vus": "2", "rampup": "10ms", "duration": "100ms", "async-scale": "true", "async-client": "2", "async-delay": "0s", "allwarehouse": "true"}
	run, err := bench.RunTest(t.Context(), Test, options)
	if err != nil {
		t.Fatal(err)
	}
	if run.Errors.TerminalErrors != 0 {
		t.Fatal(run.Errors)
	}
	for _, report := range run.WorkloadReports {
		if report.Kind == "hammerdb.tpcc" && !strings.Contains(string(report.Data), `"valid":true`) {
			t.Fatal(string(report.Data))
		}
	}
	path := filepath.Join(t.TempDir(), "mysqlcpool.xml")
	xml := poolXML("127.0.0.1", "5432", "root", "hammerdb", name)
	xml = strings.ReplaceAll(xml, "pg_", "mysql_")
	xml = strings.ReplaceAll(xml, "<mysql_sslmode>disable</mysql_sslmode>", "<mysql_socket>null</mysql_socket><mysql_ssl_options>-ssl false</mysql_ssl_options>")
	// Explicit named driver settings avoid embedding integration credentials in XML.
	xml = strings.ReplaceAll(xml, "<mysql_port>5432</mysql_port>", "<mysql_port>3306</mysql_port>")
	if err := os.WriteFile(path, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	options.Drivers["c1"] = bench.DriverConfig{Kind: bench.DriverMySQL, URL: config.FormatDSN()}
	options.Drivers["c2"] = bench.DriverConfig{Kind: bench.DriverMySQL, URL: config.FormatDSN()}
	options.Params.CLI = map[string]string{"mode": "test", "iterations": "100", "connect-pool": path}
	run, err = bench.RunTest(t.Context(), Test, options)
	if err != nil {
		t.Fatal(err)
	}
	if run.Errors.TerminalErrors != 0 {
		t.Fatal(run.Errors)
	}
	setup := bench.Test{Name: "mysql-schema-options", Define: func(d *bench.Def) error {
		d.Drivers.Declare("default", bench.DriverConfig{Kind: bench.DriverMySQL})
		d.Execution.Step("schema", func(ctx context.Context, b *bench.Bench) error {
			for _, table := range []string{"customer", "district", "history", "item", "new_order", "orders", "order_line", "stock", "warehouse"} {
				if err := b.Exec(ctx, "DROP TABLE `"+table+"`", nil); err != nil {
					return err
				}
			}
			return (&workload{mysql: true, historyPK: true, partition: true, warehouses: 200, storageEngine: "innodb"}).createMySQLSchema(ctx, b)
		})
		return d.Execution.Err()
	}}
	if _, err := bench.RunTest(t.Context(), setup, bench.RunOptions{Drivers: options.Drivers, Logger: bench.NewLogger(io.Discard)}); err != nil {
		t.Fatal(err)
	}
	var partitions int
	if err := database.QueryRowContext(t.Context(), "SELECT count(*) FROM information_schema.partitions WHERE table_schema=DATABASE() AND table_name='order_line' AND partition_name IS NOT NULL").Scan(&partitions); err != nil {
		t.Fatal(err)
	}
	if partitions != 2 {
		t.Fatal("partition count", partitions)
	}
	var invisible string
	if err := database.QueryRowContext(t.Context(), "SELECT extra FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='history' AND column_name='id'").Scan(&invisible); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(invisible, "INVISIBLE") {
		t.Fatal(invisible)
	}
}
