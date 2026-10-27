# HammerDB PostgreSQL and MySQL TPROC-C

GPL-3.0 workload derived from HammerDB v6.1. See NOTICE and LICENSE.

The workload uses HammerDB's original PostgreSQL functions/procedures or MySQL
procedures, its 10:10:1:1:1 mix, database-side New-Order randomness, and its client
and loader distributions. It can run directly against a HammerDB-created database.
MySQL also supports HammerDB's client-side SQL variant with its original client
random draws. See the project README for reference-comparison tools.

From the project directory:

```sh
go run . -d pg -D url='postgres://tpcc:tpcc@localhost:5432/tpcc' \
  --mode timed --vus 8 --rampup 2m --duration 5m
```

`--vus` counts active worker threads; HammerDB's timed VU count includes one
additional monitor. `--async-scale=true --async-client 10` creates ten clients per
worker. `--iterations` is a per-client ceiling, including ramp-up, not an SDK
shared-iteration count. `--mode test --iterations 100` runs a finite, verbose test.

Use `--create=true --warehouses 1 --mode test --iterations 1` to create and load the
schema in a dedicated empty database. Existing tables are never dropped. Select
only setup with `--steps create_schema,load_items,load_data,create_indexes,create_routines,analyze`.
Never run setup against data you need to keep. Runtime-only runs are the default.

MySQL uses `-d mysql -D url='user:password@tcp(host:3306)/tpcc?parseTime=true'`.
`--prepared=true` selects SQL PREPARE/EXECUTE; `--no-stored-procs=true` selects
client-side transactions. `--history-pk=true` and `--storage-engine innodb` select
MySQL schema options. Create the empty database with `latin1_swedish_ci` to match
HammerDB. Clients retain `autocommit=0` and separate output-variable reads. MySQL
TPM uses global `Com_commit + Com_rollback`. XML `mysqlcpool.xml` routing forces
prepared procedure calls; driver overrides configure credentials/socket/TLS.

Parameters mirror native PostgreSQL HammerDB options: `--storedprocs`,
`--keyandthink`, `--allwarehouse`, `--raiseerror`, `--timeprofile`, `--partition`,
`--vacuum`, `--async-scale`, `--async-client`, `--async-delay`, `--async-verbose`,
`--connect-pool PATH` (HammerDB pgcpool.xml), `--load-workers`, and `--seed`.
Run `go run . --help` for defaults and descriptions. Durations use Go syntax;
HammerDB ramp-up and duration values in minutes become `2m` and `5m`.
Boolean parameters need explicit values, for example `--storedprocs=true`.
Connection credentials and PostgreSQL execution mode use Stroppy's `-d`/`-D`.
Functions default to `cache_statement` for prepared execution; requested
procedures default to `simple_protocol`, matching HammerDB's text `CALL` path.
When connecting to a procedure-only database, pass `--storedprocs=true` even
though routine detection also selects the correct SQL automatically. Changing
`postgres.defaultQueryExecMode` is an explicit transport change for comparisons.

The default pool allows one persistent session per client plus the monitor.
An operator-supplied `-D postgres.maxConns=N` overrides it and must leave enough
capacity; too few connections can block login or counter snapshots. Pool routing
needs that capacity separately for every named connection, plus default metadata
connections. XML secrets are not copied into reports.

HammerDB v6.1 quirks are preserved, including literal `'$8'`/`'$5'` arguments in
prepared Payment/Order-Status calls, asymmetric order-customer shuffle, customer
credit distribution, inclusive string replacement, and function exception
handlers that issue `ROLLBACK`. Default errors are counted and clients continue;
`--raiseerror` stops the affected client. No transaction retries are added.

NOPM and TPM come from the same database counter queries as HammerDB. Snapshots
occur while clients remain active, before stopping/draining them. Native Stroppy
metrics cover the full workload step, including ramp-up and drain; they are not
HammerDB's measured NOPM/TPM. Client counts are attempts, not confirmed commits.

The port does not provide Citus/EDB dialects, DRITA snapshots, host/database/user
provisioning, or HammerDB primary/replica orchestration. Go goroutines replace
Tcl asynchronous clients; OS scheduling and concurrent random draw order are not
promised identical. The seed reproduces Tcl's sequential random stream but does
not seed PostgreSQL `random()`; compare restored databases with matching server
settings, routine mode, client shape, and measurement windows.
