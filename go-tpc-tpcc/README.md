# go-tpc-derived TPC-C

Apache-2.0 standalone Stroppy SDK workload derived from
[pingcap/go-tpc](https://github.com/pingcap/go-tpc), pinned at
`a9ca4818625deef91ff80f6c395a575ccae22b7c`. See [NOTICE](workload/NOTICE).
It does not import or invoke the upstream runner in production.

## Run

Against a database already populated by go-tpc:

```sh
go run . -d mysql -D url='root:password@tcp(localhost:3306)/tpcc' \
  --warehouses 2 --vus 2 --duration 60s

go run . -d pg -D url='postgres://postgres:password@localhost:5432/tpcc?sslmode=disable' \
  --warehouses 2 --vus 2 --duration 60s
```

Prepare a dedicated empty database with `--action prepare`. Preparation never
automatically drops existing data. `--action cleanup` explicitly drops all nine
TPC-C tables and must not target a database containing data you need. Other
actions are `run` (default) and `check`.

```sh
go run . -d mysql -D url='root:password@tcp(localhost:3306)/new_tpcc' \
  --action prepare --warehouses 2 --vus 1
```

Default post-prepare checks include all upstream consistency conditions and can
be slow. `--no-check=true` explicitly omits those checks; it is not implicit.
`--action check` normally excludes expensive condition 3.3.2.11; `--check-all=true`
includes it. The workload carries the actual upstream check SQL.

## Shape and controls

- `--warehouses` defaults to 10; it must match the loaded database.
- `--vus` defaults to 1. `--count` is divided by workers with integer division,
  matching go-tpc's count loop; zero (or a divided count of zero) runs until timeout.
- `--duration` defaults to 1 minute as a local safety default. Upstream defaults
  to effectively unbounded runtime. Time expiry cancels active query contexts.
- `--weight 45,43,4,4,4` selects New-Order, Payment, Order-Status, Delivery,
  Stock-Level. Upstream samples the weighted deck with replacement; its unused
  deck-index/shuffle behavior is not replaced with a different scheduler.
- `--wait=true` enables upstream keying and capped exponential think times.
  This includes upstream means: Order-Status 10 seconds, Delivery 5 seconds.
- `--isolation` uses database/sql numbers: 0 default, 1 read-uncommitted,
  2 read-committed, 4 repeatable-read, 6 serializable.
- `--ignore-error=true` continues after transaction errors. Default stops the
  affected worker on its first terminal error, while other workers continue,
  matching go-tpc. SDK reports completed-with-errors; no transaction retry is added.
- `--parts`, `--partition-type`, `--use-fk`, and `--use-clustered-index` retain
  upstream DDL behavior and database restrictions. PostgreSQL partitioning and
  TiDB-specific hints are not generalized into unrelated SQL.
- `--retry-count` and `--retry-interval` apply to load batches only. Final load
  errors are returned rather than reproducing upstream's silent retry exhaustion.
- `--conn-refresh-interval` reacquires worker sessions. A pooled session can be
  reused; configure pool connection lifetime if physical reconnect is required.
- `--seed` is a reproducibility extension. Zero uses wall-clock seeds like
  upstream; a positive value fixes worker streams, NURand constants, and the
  loader's global permutation stream. No database-side random is substituted.

Boolean flags need explicit values. CLI, process environment, and typed config
are handled by the Stroppy SDK. `go run . --help` lists parameter mappings.

The schema, row generator, SQL templates and transaction algorithms are derived
from upstream sources under `workload/upstream_*.go`. Quirks remain, including
New-Order's home-warehouse stock lookup/update for remotely supplied lines,
`randItemID`/`randCustomerID` expression precedence, and batched Delivery calls.
The port does not reuse Stroppy's built-in TPC-C implementation.

## Comparison limits

Use matching snapshots, warehouses, weights, wait mode, isolation, concurrency,
and time/count limits. Restore before each comparison; never run both clients
against the same mutable database simultaneously. Preserve original server
settings and run order in the evidence.

Transactions and SQL effects match the pinned implementation, but transport is
not identical. go-tpc holds explicit database/sql prepared statements. The SDK
exposes query operations, not reusable prepared handles: PostgreSQL uses pgx's
statement cache, while MySQL uses the SDK driver's parameter path. Connection
health checks and eager statement preparation are not reproduced as wire-level
operations. This port therefore compares the complete SDK-backed execution path,
not a pure scheduler substitution with identical driver overhead. Reusable SDK
prepared-statement support is tracked in
[stroppy#233](https://github.com/stroppy-io/stroppy/issues/233).

The workload reports successful/failed transaction counts and client-window
New-Order throughput. Upstream's displayed tpmC uses per-operation histogram
creation times; the SDK report uses its explicit workload window. These denominators
are not claimed identical. Neither output is an audited TPC-C result. Use database
counter deltas or the same externally measured window for strict performance
comparisons. Native SDK iterations represent worker sessions, not transactions;
`go_tpc_transactions_total` and logical-operation counters count transactions.

The load sink preserves upstream SQL row formatting, ordering and 1024-row batches,
using SDK execution. It deliberately reports final load failure instead of
silently accepting an incomplete dataset. CSV-export tooling, upstream CLI output
formats, Prometheus server, pprof, and multi-target routing remain outside this
standalone workload; Stroppy owns reporting and driver configuration.

## Verification

```sh
go test -race ./...
go vet ./...
GO_TPC_TEST_DRIVER=mysql \
GO_TPC_TEST_DSN='root:password@tcp(localhost:3306)/mysql' \
  go test -race -tags=integration ./workload -run '^TestDatabaseProfiles$'
GO_TPC_TEST_DRIVER=postgres \
GO_TPC_TEST_DSN='postgres://postgres:password@localhost:5432/postgres?sslmode=disable' \
  go test -race -tags=integration ./workload -run '^TestDatabaseProfiles$'
```

Tagged tests require database-create privileges. They create uniquely named
scratch databases and remove only those databases afterward. They load **two
warehouses**, compare all nine table counts/non-timestamp hashes with actual
upstream output, execute 100 deterministic transactions, and compare all nine
post-run hashes again. They also exercise concurrent timed runs and connection
refresh. MySQL 8.0.46 and PostgreSQL 16 were used locally.

For the independent reference, `tools/reference.py` copies the pinned checkout
into a new directory and adds test-only clock/random controls. It does not rewrite
transaction SQL or algorithms. The reference test invokes upstream `Prepare` and
`Run` directly. Generated SDK counterparts use the same controls:

```sh
python3 -I tools/reference.py /path/to/go-tpc /tmp/go-tpc-oracle-new
GO_TPC_DRIVER=mysql GO_TPC_DSN='root:password@tcp(localhost:3306)/reference_db' \
GO_TPC_ACTION=prepare \
  go -C /tmp/go-tpc-oracle-new test ./tpcc -run '^TestContribOracle$'
GO_TPC_DRIVER=mysql GO_TPC_DSN='root:password@tcp(localhost:3306)/sdk_db' \
GO_TPC_ACTION=prepare \
  go test -tags=integration ./workload -run '^TestDifferentialOracle$'
```

Both database names must be separate and empty. Repeat commands without
`GO_TPC_ACTION=prepare` for the 100-transaction comparison. Use `postgres` and
PostgreSQL URLs for that backend. `tools/mysql_fingerprint.py` and
`tools/fingerprint.sql` generate corresponding fingerprints. Expected output is
committed under `tools/expected/`.

Regenerate adapted sources without executing the reference checkout:

```sh
python3 -I tools/port.py /path/to/go-tpc workload
gofmt -w workload/upstream_*.go
git diff -- workload/upstream_*.go
```
