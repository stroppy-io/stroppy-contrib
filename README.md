# Stroppy contributions

Community and reference workloads for [Stroppy](https://github.com/stroppy-io/stroppy),
a Go-native database stress-testing CLI. Each directory is one self-contained
project, created with `stroppy init` and edited — exactly what the
[Custom workloads](https://stroppy.io/docs/next/custom-workloads) guide walks
through.

These two are the reference samples from that guide:

| Project | What it demonstrates |
|---|---|
| [`select1`](./select1) | The smallest workload worth writing: one statement, and an answer that is checked rather than discarded. |
| [`selectfile`](./selectfile) | A statement loaded from a workload-owned SQL file, typed parameters (`--expected`, `--label`, `--sql-file`), report data, and the positional SQL override. |

Both run against a real database, against
[pg-noop](https://github.com/stroppy-io/pg-noop) on loopback, or against the
recording driver in their own tests — no server needed for `go test ./...`.

[`hammerdb-tpcc`](./hammerdb-tpcc) ports HammerDB v6.1's PostgreSQL and MySQL TPROC-C
workload to the Stroppy SDK, retaining its SQL, generation, transaction mix,
and database-derived NOPM/TPM. It has a separate GPL-3.0 license. Its unit tests
use recording; its tagged integration suites use PostgreSQL and MySQL.

## Try one

```bash
git clone https://github.com/stroppy-io/stroppy-contrib
cd stroppy-contrib/selectfile

go test ./...                       # it passes with no database at all
go run . -d pg -D url='postgres://user@localhost:5432/postgres' --iterations 5
go run . -d pg -D url='…' --expected 2 --iterations 3   # fails: the file returns 1

stroppy build .                     # register it, then run it by name
stroppy run selectfile -D url='…' --iterations 5
```

`selectfile` accepts a replacement SQL file positionally, next to the workload
name and before any flag:

```bash
go run . ./queries.sql -d pg -D url='…'
```

## Requirements

- Stroppy `init`, `build`, `run`, `eject`, and friends ship with the next v6
  release; use a build that has them.
- Go 1.27 or newer.

Each project's `go.mod` pins the SDK version it was written against. Updates are
ordinary module updates:

```bash
go get github.com/stroppy-io/stroppy/v6@latest
go test ./...
```

## Continuous integration

`.github/workflows/ci.yml` runs:

- **examples** — on every push and pull request, builds and tests each project
  against its pinned SDK, which is what a reader of this repository gets.
- **contract** — on a schedule and on demand, compiles the same projects against
  Stroppy `main` by replacing the SDK with a checkout. That is the run that
  notices when an upstream change breaks the authoring contract these samples
  depend on, without waiting for a release.
- **hammerdb-postgres** — loads a full warehouse and tests function, procedure,
  timed, and asynchronous profiles against PostgreSQL 17.
- **hammerdb-mysql** — checks full-loader parity and text, prepared, client-side,
  timed/asynchronous, and routed profiles against MySQL 8.0.

The second job is deliberately separate: it is expected to fail when upstream
breaks the contract, and that failure is the signal, not an accident.

## Contributing a workload

A workload belongs here when it demonstrates something a reader can learn from —
an authoring pattern, a driver, a checking strategy — and passes on its own.
Keep `go test ./...` meaningful with the recording driver, so contributors do
not need a database to check their work.

Workloads that are built into Stroppy itself live in the
[main repository](https://github.com/stroppy-io/stroppy/tree/main/workloads).

## License

Apache 2.0, matching Stroppy, except `hammerdb-tpcc/`, which is GPL-3.0 because
it includes HammerDB-derived code and SQL. See that module's LICENSE and
workload/NOTICE. The Stroppy SDK retains its Apache-2.0 license.
