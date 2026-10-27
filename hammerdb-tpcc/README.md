# HammerDB-derived PostgreSQL and MySQL TPROC-C

Standalone Stroppy SDK workload. See [workload/README.md](workload/README.md)
for modes, flags, semantics, and known upstream quirks. This entire module is
GPL-3.0, not the repository's default Apache-2.0; see [LICENSE](LICENSE) and
[NOTICE](workload/NOTICE).

```sh
go test -race ./...
go run . -d pg -D url='postgres://tpcc:tpcc@localhost:5432/tpcc' \
  --mode timed --vus 8 --rampup 2m --duration 5m
```

No setup runs by default, so the same command accepts a database created by
HammerDB v6.1. To create a dedicated empty database's full one-warehouse dataset:

```sh
go run . -d pg -D url='postgres://tpcc:tpcc@localhost:5432/new_tpcc' \
  --create=true --warehouses 1 \
  --steps create_schema,load_items,load_data,create_indexes,create_routines,analyze
```

Use `--storedprocs=true` on both setup and run to select PostgreSQL procedures.
Without it, the default is HammerDB's function mode. `--mode test --iterations 100`
runs 100 transactions **per client**. `--async-scale=true --async-client 10`
creates ten concurrent clients per active worker. `--keyandthink=true` enables
HammerDB's pacing. Other parameters and process-environment names appear in
`go run . --help`.

## MySQL

```sh
go run . -d mysql -D url='tpcc:tpcc@tcp(localhost:3306)/tpcc?parseTime=true' \
  --mode timed --vus 8 --rampup 2m --duration 5m
```

MySQL uses HammerDB's original procedures by default; `--storedprocs` only
selects PostgreSQL's routine style. `--prepared=true` uses HammerDB's SQL
`PREPARE`/`EXECUTE ... USING` sequence. `--no-stored-procs=true` selects its
client-side SQL variant, including client-side item/quantity draws in that mode.
The default procedure variants retain all item/quantity/rollback randomness in
MySQL `RAND()`. Connection routing uses `--connect-pool mysqlcpool.xml` and forces
prepared procedure calls, as HammerDB does. Pool routing takes precedence over
`--no-stored-procs`.

MySQL clients use persistent sessions with `autocommit=0`; the timed monitor uses
`autocommit=1`. Output-variable SELECTs remain separate round trips, even in timed
mode. No SDK transaction wraps the procedure and no retry is added. Session
cleanup rolls back unfinished work before returning the connection to the pool.

Create a dedicated empty database with `CHARACTER SET latin1 COLLATE
latin1_swedish_ci` to match HammerDB's schema provisioning. Pass `--create=true`
with the same setup steps shown above. The loader shares HammerDB's matching
value generation but rounds `order_line.ol_amount` to MySQL's upstream integer
column. `--storage-engine innodb`, `--history-pk=true`, and `--partition=true`
mirror MySQL setup options; invisible history keys require MySQL 8.0.30+.
Credentials, Unix sockets, and TLS use Stroppy driver options/DSNs. XML TLS option
lists are rejected rather than silently ignored; use named driver overrides.

MySQL TPM uses global `Com_commit + Com_rollback`, not PostgreSQL counters or
Stroppy logical successes. Default procedure mode and client-side mode can produce
different SQL semantics because those differences already exist in HammerDB.

## Side-by-side comparisons

Use the pinned upstream reference:

```sh
git clone https://github.com/TPC-Council/HammerDB ../HammerDB
git -C ../HammerDB switch --detach 2442b32756a484de4206bb0061d9bb28869beaf0
```

Create and snapshot the dataset once. Restore it before each run; do not let both
clients modify the same live database concurrently. Match PostgreSQL version,
settings, storage, schema/routines, warehouse count, warm cache policy, and host
resources. Use the same function/procedure profile, pacing, warehouse mode,
iteration ceiling, and measurement window. The Stroppy `--vus 8` setting matches
**nine** configured HammerDB VUs in a timed run: eight active workers plus one
monitor. For async mode, also match clients per worker and login delay.

Compare `hammerdb.tpcc` report NOPM/TPM (or `hammerdb_nopm`/`hammerdb_tpm` gauges),
not Stroppy's native TPS. PostgreSQL TPM is cluster-wide, includes rollbacks and
monitor queries, and can include unrelated activity. Native SDK metrics include
ramp-up and client drain. One side's native summary is not the other's throughput
score. Whole-minute results retain HammerDB's integer division; shorter durations
are supported for smoke testing.

SQL routines remain upstream SQL. Client input generation uses Tcl's Park-Miller
stream with explicit `--seed`; server-side `random()` is not seeded or replaced
in production. Client scheduling, binary/text result transport, SDK telemetry,
and Go/Tcl runtime overhead differ. Those are performance comparison subjects,
not claims of identical wire traffic or scheduling.

A [short local MySQL comparison](comparisons/local-mysql-20261027.md) completed
with zero reported SQL errors, but throughput varied enough to reverse the winner.
Its full case results are included; it does not establish a speed ranking.

The upstream timed MySQL driver can run without its GUI using the launcher:

```sh
MYSQL_PWD='<password>' tclsh tools/mysql_timed.tcl ../HammerDB \
  localhost 3306 user restored_copy text 2 0.1 0.2
```

The last values are active clients, warm-up minutes, and measurement minutes.
Use `prepared` for SQL-prepared calls. Timings must be multiples of 0.1 minutes
because the original driver polls every six seconds. This launcher uses the
upstream timed transaction bodies, unlike the fixed-iteration state oracle.
Its runtime dependencies are Tcl, Thread, and mysqltcl.

## Verification

Unit/recording tests require no database. The Tcl generator oracle runs when
`tclsh` is installed; CI installs it. Tagged PostgreSQL tests require CREATEDB
permission, create uniquely named disposable databases, and remove only those:

```sh
HAMMERDB_PG_URL='host=localhost port=5432 user=postgres password=postgres dbname=postgres sslmode=disable' \
  go test -race -tags=integration ./workload -run '^TestPostgresProfiles$'
```

MySQL's tagged suite creates/removes a disposable database and checks all nine
loader fingerprints, text/prepared/client transaction profiles, timed async mode,
and connection routing:

```sh
HAMMERDB_MYSQL_URL='root:mysql@tcp(localhost:3306)/mysql?parseTime=true' \
  go test -race -tags=integration ./workload -run '^TestMySQLProfiles$'
```

`tools/reference.tcl` invokes the actual pinned HammerDB loader without its GUI;
it needs Tcl, Thread, and Pgtcl. `tools/fingerprint.sql` hashes all nine tables
excluding wall-clock timestamps. Running both loaders with seed 1 produced equal
row counts and non-timestamp hashes for every table, including 298,934 order lines.

```sh
tclsh tools/reference.tcl ../HammerDB \
  'host=localhost user=postgres dbname=reference_empty' functions
psql 'postgres://postgres@localhost/reference_empty' -Atf tools/fingerprint.sql
psql 'postgres://postgres@localhost/stroppy_empty' -Atf tools/fingerprint.sql
```

`tools/drive.tcl` and `TestDifferentialClient` provide a controlled 100-transaction
oracle. Restore two copies of the same snapshot first. Both use client seed 1
and database `setseed(0.5)` **only in this oracle**, plus a fixed procedure input
timestamp. Function and procedure runs produced equal nine-table non-timestamp
fingerprints after 100 transactions.

```sh
tclsh tools/drive.tcl ../HammerDB \
  'host=localhost user=postgres dbname=reference_copy' functions 100
HAMMERDB_DIFFERENTIAL_URL='host=localhost user=postgres dbname=stroppy_copy' \
  go test -tags=integration ./workload -run '^TestDifferentialClient$'
```

For procedures, use `procedures` in the Tcl command and add
`HAMMERDB_DIFFERENTIAL_PROCEDURES=true` to the Go command. Both copies must have
procedure routines installed. Debian Pgtcl 3.0.1 exposes a shorter prepared-call
signature than HammerDB's bundled binding; append `legacy-pgtcl` to the Tcl
command for that binding. The adapter removes only the two empty format lists.
It does not alter SQL or transaction inputs.

MySQL reference tools require Tcl, Thread, and mysqltcl. The loader comparison
uses `tools/mysql_reference.tcl`; `tools/mysql_fingerprint.py` runs the mysql CLI
and hashes all non-timestamp columns. Text, prepared, and client-side 100-transaction
comparisons all matched nine-table fingerprints on MySQL 8.0. Client and server
seeds are controlled only inside these test oracles, never production runs.

```sh
tclsh tools/mysql_reference.tcl ../HammerDB localhost 3306 root mysql reference_empty
python3 -I tools/mysql_fingerprint.py reference_empty mysql -uroot -hlocalhost
python3 -I tools/mysql_fingerprint.py stroppy_empty mysql -uroot -hlocalhost

# Restore two copies first; supply MYSQL_PWD to mysql CLI if needed.
tclsh tools/mysql_drive.tcl ../HammerDB localhost 3306 root mysql reference_copy prepared 100
HAMMERDB_MYSQL_DIFFERENTIAL_URL='root:mysql@tcp(localhost:3306)/stroppy_copy?parseTime=true' \
HAMMERDB_MYSQL_DIFFERENTIAL_PROFILE=prepared \
  go test -tags=integration ./workload -run '^TestMySQLDifferentialClient$'
```

Use `text` or `client` in both commands for those profiles. The MySQL oracle sets
`rand_seed1=123,rand_seed2=456` and fixes input timestamps. Restore snapshots before
each comparison; never reuse an already modified copy.

SQL extraction is reproducible and does not execute downloaded Tcl:

```sh
python3 -I tools/extract.py ../HammerDB workload
git diff -- workload/schema.sql workload/functions.sql workload/procedures.sql
```

## Scope

Native PostgreSQL and MySQL: PostgreSQL functions/procedures, MySQL text/prepared
procedures and client-side transactions, finite/timed runs, fixed/all-warehouse
terminals, pacing, async clients, HammerDB XML connection-routing policies,
partitioned order-line setup, and PostgreSQL post-run vacuum/checkpoint. The loader
uses SDK COPY (PostgreSQL) or bulk INSERT (MySQL) with HammerDB values and ordering;
it does not reproduce
HammerDB's exact load-time commit/transfer chunking or implicit timestamp timing.
Load performance is not part of the runtime parity claim. Multi-loader warehouse
ranges match HammerDB; independent worker seeds are explicit in the Go workload.

Citus, EDB/Oracle compatibility, DRITA, remote primary/replica orchestration,
GUI/Tcl control commands, and automatic user/database/tablespace provisioning
are not implemented. The workload never drops existing tables. Peak performance
comparison has not been benchmarked as part of the parity validation.
