package workload

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
	"github.com/stroppy-io/stroppy/v6/pkg/bench/testkit"
	"github.com/stroppy-io/stroppy/v6/pkg/record"
)

func recorder() *record.Recorder { return modeRecorder("f") }

func modeRecorder(kind string) *record.Recorder {
	r := &record.Recorder{}
	for range 32 {
		r.Reply("select max(w_id) from warehouse", record.Response{Columns: []string{"max"}, Rows: [][]any{{int64(5)}}})
		r.Reply("select max(d_id) from district", record.Response{Columns: []string{"max"}, Rows: [][]any{{int64(10)}}})
		r.Reply(detectSQL, record.Response{Columns: []string{"prokind", "cnt"}, Rows: [][]any{{kind, int64(5)}}})
	}
	return r
}

func TestRecordingKeepsAutocommitAndPerClientIterations(t *testing.T) {
	r := recorder()
	run, err := testkit.Record(t.Context(), Test, r, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"mode": "test", "vus": "3", "iterations": "100"}}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Errors.TerminalErrors != 0 {
		t.Fatal(run.Errors)
	}
	var counts [5]int
	for _, operation := range r.Operations() {
		if strings.EqualFold(operation.SQL, "BEGIN") {
			t.Fatal("SQL transaction wrapped HammerDB routine")
		}
		for kind, sql := range functionSQL {
			if operation.SQL == sql {
				counts[kind]++
			}
		}
	}
	total := 0
	for _, n := range counts {
		total += n
		if n == 0 {
			t.Fatal(counts)
		}
	}
	if total != 300 {
		t.Fatalf("%d attempts, want 100 per client", total)
	}
}

func TestFunctionCallsKeepDatabaseRandomnessAndQuotedNames(t *testing.T) {
	for kind := range transactionNames {
		terminal := &terminal{random: newRandom(7), home: 2, district: 8, warehouses: 10}
		statement := terminal.statement(kind, time.Unix(0, 0))
		if statement.text != functionSQL[kind] {
			t.Fatal(statement.text)
		}
		for _, parameter := range []string{"item", "quantity", "supply", "rollback"} {
			if _, exists := statement.args[parameter]; exists {
				t.Fatal("database random moved to client", parameter)
			}
		}
	}
	if !strings.Contains(functionSQL[1], "'$8'") || !strings.Contains(functionSQL[4], "'$5'") {
		t.Fatal("quoted upstream parameters lost")
	}
}

func TestRoutineDetectionOverridesRequestedMode(t *testing.T) {
	r := modeRecorder("p")
	_, err := testkit.Record(t.Context(), Test, r, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"mode": "test", "iterations": "100"}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, operation := range r.Operations() {
		if strings.HasPrefix(operation.SQL, "call ") {
			calls++
		}
	}
	if calls != 100 {
		t.Fatal(calls)
	}
}

func TestNativeSQLAssets(t *testing.T) {
	for _, name := range []string{"functions.sql", "procedures.sql"} {
		routines := assetSQL(name).Section("routines")
		if len(routines) != 6 {
			t.Fatal(name, len(routines))
		}
		if !strings.Contains(routines[1], "DBMS_RANDOM") || !strings.Contains(routines[1], "100001") {
			t.Fatal("database random or rollback path missing")
		}
		if !strings.Contains(routines[1], "THEN ROLLBACK") {
			t.Fatal("upstream exception handler changed")
		}
	}
	if len(assetSQL("schema.sql").Section("create_schema")) != 9 {
		t.Fatal("schema table count")
	}
}

func TestTimedSnapshotBeforeClientStop(t *testing.T) {
	r := recorder()
	for range 2 {
		r.Reply(transactionCountSQL, record.Response{Columns: []string{"sum"}, Rows: [][]any{{int64(10)}}})
		r.Reply(newOrderCountSQL, record.Response{Columns: []string{"sum"}, Rows: [][]any{{int64(30010)}}})
	}
	_, err := testkit.Record(t.Context(), Test, r, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"mode": "timed", "iterations": "2", "rampup": "1ms", "duration": "1ms"}}})
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []string
	for _, operation := range r.Operations() {
		if operation.SQL == transactionCountSQL || operation.SQL == newOrderCountSQL {
			snapshots = append(snapshots, operation.SQL)
		}
	}
	want := []string{transactionCountSQL, newOrderCountSQL, transactionCountSQL, newOrderCountSQL}
	if strings.Join(snapshots, "|") != strings.Join(want, "|") {
		t.Fatal(snapshots)
	}
}

func TestCancellationInterruptsPacingAndMonitor(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := testkit.Record(ctx, Test, recorder(), bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"keyandthink": "true", "rampup": "2m"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
}

func TestSQLFailurePolicyDoesNotRetry(t *testing.T) {
	for _, raise := range []string{"true", "false"} {
		t.Run(raise, func(t *testing.T) {
			r := recorder()
			for _, sql := range functionSQL {
				for range 10 {
					r.Reply(sql, record.Response{Err: errors.New("sentinel SQL failure")})
				}
			}
			run, err := testkit.Record(t.Context(), Test, r, bench.RunOptions{Params: bench.ParamInputs{CLI: map[string]string{"mode": "test", "iterations": "10", "raiseerror": raise}}})
			if err != nil {
				t.Fatal(err)
			}
			want := uint64(10)
			if raise == "true" {
				want = 1
			}
			if run.Errors.TerminalErrors != want || run.Errors.RetryAttempts != 0 {
				t.Fatal(run.Errors)
			}
		})
	}
}

func TestReportPayloadRoundTrip(t *testing.T) {
	w := &workload{mode: "timed", workers: 3, seed: 7, result: measurement{Valid: true, NOPM: 123, TPM: 456}}
	contribution, err := w.report(bench.ReportContext{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(contribution.Data)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	if err := renderReport(&text, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "123 NOPM / 456 PostgreSQL TPM") {
		t.Fatal(text.String())
	}
}
