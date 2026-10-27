# Local MySQL smoke comparison — 2026-10-27

This is a short execution comparison, not a stable performance benchmark. The
results vary enough to reverse the apparent winner. **No defensible throughput
ranking or percentage-overhead claim follows from these runs.**

## Setup

- Workload commit: `e5cd067b9ea864495a973ebb5f20b8a779e51cac`.
- HammerDB v6.1: `2442b32756a484de4206bb0061d9bb28869beaf0`.
- MySQL Community Server 8.0.46, Docker Desktop on macOS 27.0.1 / arm64.
- Docker VM: 6 CPUs, 6,210,306,048 bytes RAM. Both load clients ran in the same
  Debian bookworm / Linux arm64 container, against the same separate MySQL
  container over its Docker bridge IP. The SDK binary was cross-built with
  Go 1.27.2, `GOOS=linux GOARCH=arm64 CGO_ENABLED=0`, and `-trimpath`.
- Reference runtime: Tcl 8.6.13, mysqltcl 3.052, Thread 2.8.8. This is the actual
  upstream synchronous timed driver executed through `tools/mysql_timed.tcl`,
  not the complete HammerDB CLI distribution. The launcher replaces GUI thread
  identity lookup and supplies explicit client seeds; transaction SQL and timing
  loop remain upstream code.
- One warehouse; 298,934 initial order lines. Each case restored a new database
  from the same Tcl-generated snapshot, with the same procedures, `latin1`
  charset, and `latin1_swedish_ci` collation. No two cases ran concurrently.
- Snapshot SHA-256:
  `e23ea24c1a3d0818bed525bef1e1753b33385705d2f0d8bd922d23997e81454e`.
- Two active synchronous clients plus one monitor. Fixed home warehouse;
  key/think time off; client seed 1 plus worker index. Database random draws were
  not fixed, matching normal benchmark operation.
- Warm-up 6 seconds, measurement 12 seconds. HammerDB accepts these as 0.1 and
  0.2 minutes and retains its six-second polling loop. SDK uses `6s` and `12s`.
- Durability unchanged: `innodb_flush_log_at_trx_commit=1`, `sync_binlog=1`;
  server `max_connections=200`. Logging was suppressed for both timed clients.
  Error-raising was enabled on both sides.

## Results

[All twelve cases, in execution order](local-mysql-20261027.csv) are retained.
NOPM and TPM use each implementation's HammerDB-style database counters, not
Stroppy's native logical TPS. All cases completed their timing windows with
**zero reported SQL errors**.

Initial series used a 128 MiB InnoDB buffer pool, with no explicit wait for restore
checkpoint activity. Text NOPM was 20,605 / 11,925 for HammerDB and 10,830 / 8,080
for Stroppy. Prepared NOPM was 18,525 / 11,475 for HammerDB and 10,995 / 27,780 for
Stroppy. The paired order was reversed for the second repeat.

A follow-up text series enlarged the buffer pool to 512 MiB and waited for restore
dirty pages to fall below 16. Dirty-page targets were temporarily set to zero
while settling the restore, then restored to 90% / 10% before each measured case.
The measured order was HammerDB, Stroppy, Stroppy, HammerDB. NOPM was
6,630 / 8,240 for HammerDB and 27,615 / 10,405 for Stroppy. Restore dirty-page
counts were 0 / 9 / 0 / 0 in execution order.

These swings persisted after the settling attempt. The short windows, shared
Docker VM, database background activity, and uncontrolled OS/runtime scheduling
make this an **inconclusive speed comparison**. Restore/checkpoint interference
was a hypothesis, not an established cause. No profiling was performed.

## Reproduction shape

Restore two independent database copies first. Run one client at a time from the
same Linux container, against the same MySQL server:

```sh
MYSQL_PWD='<password>' tclsh tools/mysql_timed.tcl /path/to/HammerDB \
  mysql-host 3306 user reference_copy text 2 0.1 0.2

./stroppy -d mysql -D 'url=user:password@tcp(mysql-host:3306)/sdk_copy?parseTime=true' \
  --mode=timed --vus=2 --rampup=6s --duration=12s \
  --raiseerror=true --log-level=warn --report-file=result.json
```

For prepared calls, replace `text` with `prepared` and add `--prepared=true` to
the SDK invocation. Restore before every case, reverse paired run order, and
compare `hammerdb.tpcc` NOPM/TPM. Longer windows and a quiet dedicated host are
needed before interpreting performance differences. The earlier deterministic
loader/transaction-state parity checks remain separate evidence; this smoke
comparison does not replace them. PostgreSQL performance was not compared here.
