package workload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	"github.com/stroppy-io/stroppy/v6/pkg/bench/testkit"
	"github.com/stroppy-io/stroppy/v6/pkg/record"
)

func oneValue(n int64) record.Response {
	return record.Response{Columns: []string{"sum"}, Rows: [][]any{{n}}}
}

func poolXML(host, port, user, password, database string) string {
	connections := ""
	for _, name := range []string{"c1", "c2"} {
		connections += fmt.Sprintf("<%s><pg_host>%s</pg_host><pg_port>%s</pg_port><pg_sslmode>disable</pg_sslmode><pg_user>%s</pg_user><pg_pass>%s</pg_pass><pg_dbase>%s</pg_dbase></%s>", name, host, port, user, password, database, name)
	}
	routes := ""
	for _, name := range []string{"neworder", "payment", "delivery", "stocklevel", "orderstatus"} {
		routes += fmt.Sprintf("<%s><connections>c1 c2</connections><policy>round_robin</policy></%s>", name, name)
	}
	return "<connpool><connections>" + connections + "</connections><sprocs>" + routes + "</sprocs></connpool>"
}

func TestPoolPolicies(t *testing.T) {
	p := &poolConfig{}
	for policy, want := range map[string][]string{"first_named": {"c1", "c1", "c1"}, "last_named": {"c3", "c3", "c3"}, "round_robin": {"c1", "c2", "c3"}, "unknown": {"c1", "c1", "c1"}} {
		p.routes[0] = poolRoute{Connections: "c1 c2 c3", Policy: policy}
		for index, expected := range want {
			if got := p.pick(0, uint64(index), newRandom(1)); got != expected {
				t.Fatal(policy, got)
			}
		}
	}
}

func TestPoolXMLRoutingAndCredentialIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pgcpool.xml")
	if err := os.WriteFile(path, []byte(poolXML("localhost", "5432", "tpcc", "secret-value", "tpcc")), 0600); err != nil {
		t.Fatal(err)
	}
	r := recorder()
	run, err := testkit.Record(t.Context(), Test, r, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"mode": "test", "iterations": "100", "connect-pool": path}}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Errors.TerminalErrors != 0 {
		t.Fatal(run.Errors)
	}
	var counts [5]int
	for _, operation := range r.Operations() {
		for kind, sql := range functionSQL {
			if operation.SQL != sql {
				continue
			}
			want := []string{"c1", "c2"}[counts[kind]%2]
			if operation.Scope.Database != want {
				t.Fatalf("%s routed to %s, want %s", transactionNames[kind], operation.Scope.Database, want)
			}
			counts[kind]++
		}
	}
	if strings.Contains(fmt.Sprint(run.WorkloadReports), "secret-value") {
		t.Fatal("pool credential in report")
	}
}

func TestAsyncClientsShareThreadStreamWithoutRaces(t *testing.T) {
	r := recorder()
	for range 2 {
		r.Reply(transactionCountSQL, oneValue(10))
		r.Reply(newOrderCountSQL, oneValue(30010))
	}
	run, err := testkit.Record(t.Context(), Test, r, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"vus": "2", "async-scale": "true", "async-client": "3", "async-delay": "0s", "iterations": "20", "rampup": "30ms", "duration": "10ms"}}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Errors.TerminalErrors != 0 {
		t.Fatal(run.Errors)
	}
	count := 0
	for _, operation := range r.Operations() {
		for _, sql := range functionSQL {
			if operation.SQL == sql {
				count++
			}
		}
	}
	if count != 120 {
		t.Fatal(count)
	}
}
