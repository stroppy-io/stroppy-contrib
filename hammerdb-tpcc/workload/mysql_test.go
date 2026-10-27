package workload

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	"github.com/stroppy-io/stroppy/v6/pkg/bench/testkit"
	"github.com/stroppy-io/stroppy/v6/pkg/record"
)

func TestMySQLClientDrawsMatchPostgres(t *testing.T) {
	for kind := range transactionNames {
		pg := &terminal{random: newRandom(17), home: 2, district: 3, warehouses: 5}
		my := &terminal{random: newRandom(17), home: 2, district: 3, warehouses: 5}
		pg.statement(kind, time.Now())
		call := my.mysqlStatement(kind, time.Now())
		if pg.random.state != my.random.state {
			t.Fatal(kind, pg.random.state, my.random.state)
		}
		if !strings.HasPrefix(call.text, "CALL ") {
			t.Fatal(call.text)
		}
	}
}

func TestMySQLSQLAssetsPreserveBackendQuirks(t *testing.T) {
	procedures := assetSQL("mysql_procedures.sql").Section("routines")
	if len(procedures) != 5 {
		t.Fatal(len(procedures))
	}
	if !strings.Contains(procedures[0], "FLOOR(1 + (RAND() * 99))") || !strings.Contains(procedures[0], "100001") {
		t.Fatal("server-side random/rollback changed")
	}
	if !strings.Contains(procedures[2], "p_c_balance + p_h_amount") {
		t.Fatal("MySQL payment sign changed")
	}
	schema := assetSQL("mysql_schema.sql")
	if len(schema.Section("create_schema")) != 9 || !strings.Contains(schema.Require("create_schema", "order_line").Text, "`ol_amount` INT NULL") {
		t.Fatal("MySQL schema changed")
	}
	if len(assetSQL("mysql_prepare.sql").Section("prepare")) != 5 {
		t.Fatal("prepared calls missing")
	}
}

func TestMySQLCallsKeepSessionRoundTrips(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		for kind := range transactionNames {
			terminal := &terminal{random: newRandom(1), home: 1, district: 2, warehouses: 3, prepared: prepared}
			call := terminal.mysqlStatement(kind, time.Unix(0, 0))
			test := bench.Test{Name: "mysql-call", Define: func(d *bench.Def) error {
				d.Execution.Step("call", func(ctx context.Context, b *bench.Bench) error {
					session, err := b.Begin(ctx, bench.BeginOpts{Isolation: bench.IsoConn})
					if err != nil {
						return err
					}
					defer session.Commit(ctx)
					return call.run(ctx, session)
				})
				return d.Execution.Err()
			}}
			recorder := &record.Recorder{}
			if _, err := testkit.Record(t.Context(), test, recorder, bench.RunOptions{}); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, operation := range recorder.Operations() {
				if operation.SQL != "" {
					got = append(got, operation.SQL)
				}
			}
			var want []string
			if call.setup != "" {
				want = append(want, call.setup)
			}
			want = append(want, call.text)
			if call.output != "" {
				want = append(want, call.output)
			}
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Fatal(got, want)
			}
		}
	}
}

func TestMySQLFailedCallSkipsOutputRead(t *testing.T) {
	c := mysqlCall{setup: "SET @x=1", text: "CALL broken()", output: "SELECT @x"}
	test := bench.Test{Name: "mysql-error", Define: func(d *bench.Def) error {
		d.Execution.Step("call", func(ctx context.Context, b *bench.Bench) error {
			session, err := b.Begin(ctx, bench.BeginOpts{Isolation: bench.IsoConn})
			if err != nil {
				return err
			}
			defer session.Commit(ctx)
			return c.run(ctx, session)
		})
		return d.Execution.Err()
	}}
	r := &record.Recorder{}
	r.Reply(c.text, record.Response{Err: errors.New("sentinel")})
	if _, err := testkit.Record(t.Context(), test, r, bench.RunOptions{}); err == nil {
		t.Fatal("missing error")
	}
	for _, operation := range r.Operations() {
		if operation.SQL == c.output {
			t.Fatal("output read after failed call")
		}
	}
}
