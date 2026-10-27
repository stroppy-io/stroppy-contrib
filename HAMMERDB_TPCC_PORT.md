# HammerDB TPROC-C port assessment

Prepared 2026-10-23. This records the original preparation and sizing assessment. PostgreSQL implementation and current validation now live in [hammerdb-tpcc/README.md](hammerdb-tpcc/README.md); the historical estimates and preparation status below are not the current implementation status.

## Workspace

All paths below are relative to the parent of the Stroppy repository:

- `stroppy-contrib/`: clone of `https://github.com/stroppy-io/stroppy-contrib`, on `main` at `9c136b8b3e0ae755f6a855a524d536a92956c6ca`.
- `stroppy-contrib-hammerdb-tpcc/`: linked worktree of that repository, on `port/hammerdb-tpcc`, starting at the same commit.
- `references/HammerDB/`: clone of `https://github.com/TPC-Council/HammerDB`, detached at release `v6.1`, commit `2442b32756a484de4206bb0061d9bb28869beaf0`.

The inspected PostgreSQL/MySQL workload sources, shared TPROC-C helpers, and default configuration files are identical between that release and the cloned master tip `483014b`. Existing Stroppy files and unrelated untracked files were not changed. No implementation commits, pushes, or PRs were made.

HammerDB v6.1 tracks both `DocBook/docs/images/ch10ws-1d.PNG` and `DocBook/docs/images/ch10ws-1d.png` with different contents. On this case-insensitive macOS filesystem they collide, leaving the uppercase documentation-image path reported as modified after checkout. The inspected workload sources are unchanged; the image collision does not block this assessment.

## Recommended target

Create a separate, self-contained `hammerdb-tpcc/` Go module in contrib, following its existing `main.go` plus `workload/` layout. Keep HammerDB routines as workload-owned SQL assets; port the Tcl client and loader to Go using the public Stroppy SDK. Do not substitute Stroppy's built-in `tpcc/procs`: its SQL and workload semantics differ.

Start with PostgreSQL's default synchronous, function-based profile. That is a recommendation for implementation order, not a reduction of the requested behavioral parity. PostgreSQL procedures, MySQL, asynchronous-client profiles, connection-pool profiles, and other database dialects need explicit coverage before claiming they are supported.

"Behaviorally identical" should mean matching the selected HammerDB release/profile's schema, data distributions, transaction inputs, SQL, transaction boundaries, error policy, terminal state, pacing, and measurement rules. It cannot mean identical throughput, scheduler timing, or concurrent execution order across Tcl and Go. Identical seeded random sequences would be an additional requirement: HammerDB generates random values in both its client and database routines.

## Source map

Paths here are relative to `references/HammerDB/`:

- `src/postgresql/pgoltp.tcl:485`: PostgreSQL procedures.
- `src/postgresql/pgoltp.tcl:1029`: default PostgreSQL functions and database-side random generation.
- `src/postgresql/pgoltp.tcl:1746`: schema, indexes, and relational loader.
- `src/postgresql/pgoltp.tcl:2972`: timed driver generation.
- `src/postgresql/pgoltp.tcl:3140`: measurement snapshots and NOPM/TPM calculation.
- `src/postgresql/pgoltp.tcl:3238`: synchronous transaction input generation and invocation.
- `src/postgresql/pgoltp.tcl:3437`: terminal initialization and transaction selection.
- `src/mysql/mysqloltp.tcl:37`: MySQL routines, including their own transaction control.
- `modules/tpcccommon-1.0.tm:172`: NURand, customer names, keying, and think times.
- `src/generic/gentpcc.tcl:1`: all-warehouse assignment and selection.
- `config/postgresql.xml:28`, `config/mysql.xml:30`: default driver profiles.

These Tcl files embed generated Tcl and SQL inside strings. Graph indexing reports substantial parse gaps; the assessment uses direct source inspection rather than treating graph results as complete.

## Parity requirements and traps

- **Transaction mix:** draw uniformly from 1–23. New-Order and Payment each receive 10 slots; Delivery, Stock-Level, and Order-Status receive one each. This is approximately 43.478/43.478/4.348/4.348/4.348%, not Stroppy's built-in 45/43/4/4/4 mix.
- **Terminal state:** choose a random home warehouse once per terminal, plus a fixed Stock-Level district. Other transaction districts are selected per transaction. Keep terminal state continuous across ramp-up and measurement. HammerDB's all-warehouse mode selects within a VU-assigned warehouse list, not uniformly across all warehouses for every worker.
- **Client inputs:** the inspected PostgreSQL client uses uniform customer IDs, `NURand(255, 0, 999, 123)` for last names, 60% by-name lookup, a 15% remote-Payment branch, and integer Payment amounts from 1 to 5000.
- **Server inputs:** New-Order item IDs, quantities, remote-supply decisions, and intentional invalid-item selection are generated inside HammerDB's database routines. Moving those draws into Go changes the test.
- **Schema and population:** preserve HammerDB's nine-table schema, column types, index order, and initial values/distributions. For example, its PostgreSQL loader selects customer credit with a 50/50 draw. Reusing the built-in TPC-C generator without comparing every field would not establish parity. Loader transfer chunking and transaction boundaries also matter if load behavior is included.
- **Transaction boundaries:** preserve autocommit function calls and procedure-owned transaction control. Do not wrap every routine in an SDK SQL transaction. Stroppy provides connection-only mode if a sequence needs one session without issuing `BEGIN`; session defaults and prepared calls still need verification.
- **Errors:** HammerDB defaults `raiseerror=false` and does not use Stroppy's standard retry policy. Do not silently add retries or convert all routine returns into confirmed logical successes. The default PostgreSQL functions contain `ROLLBACK` in exception handlers; their actual server behavior must be tested, not repaired during extraction without an explicit parity decision.
- **Pacing:** keying times are 18/3/2/2/2 seconds for New-Order/Payment/Delivery/Stock-Level/Order-Status. Think times use rounded exponential draws with means 12/12/10/5/5 seconds. Pacing defaults off.
- **Timed shape:** default ramp-up is two minutes, followed by five minutes of measurement. HammerDB's timed run reserves one configured VU as the monitor; Stroppy workers are active clients. HammerDB also has a per-client iteration ceiling, unlike SDK shared-iterations.
- **Metrics and stop behavior:** PostgreSQL NOPM comes from the change in `sum(d_next_o_id)`; TPM comes from the change in cluster-wide `sum(xact_commit + xact_rollback)`. Neither is Stroppy's logical TPS multiplied by 60. The monitor samples while clients are still running, then signals them to stop; a post-drain snapshot would measure a different interval.

## SDK fit

The existing public SDK supplies workload registration, typed parameters, embedded SQL loading, worker identity through `Bench.Worker()`, typed batch insertion, constant-worker execution, connection-only transactions, custom metrics, and report contributions. Most work should live in contrib rather than changing the SDK.

The current local Stroppy checkout is `ee64ca68`, 98 commits behind its local `origin/main` reference. Existing contrib samples pin SDK `v6.1.2-0.20261007182951-c287e601db9b`. Select and pin the implementation SDK deliberately; do not update the user's Stroppy checkout implicitly. Relevant SDK authoring documentation is `stroppy/docs/workload-authoring-api.md`.

No external library setup or HammerDB runtime was executed during this assessment. Go 1.27.2 is available locally. Database services, Tcl extensions, and an executable HammerDB installation have not been checked.

## Estimated effort

Estimates are focused engineering days for one person, including directly relevant parity tests, with working local databases and a runnable reference installation. Licensing review and infrastructure delays are excluded.

- **PostgreSQL adapter against an existing HammerDB-created database:** 2–3 days. Includes the five default-function transactions, terminal state/input generation, pacing, ramp-up, monitor counters, and focused validation. It is not a self-contained loader port.
- **Self-contained PostgreSQL default-profile port:** 5–8 days total. Adds extracted DDL/routines, the nine-table generator/loader, recording tests, real-database checks, and differential validation against the reference.
- **PostgreSQL and MySQL default synchronous profiles:** 8–13 days total. MySQL requires its own SQL, session/output-variable handling, error semantics, and database validation.
- **Broad HammerDB feature/backend parity:** several weeks beyond the default profiles. Procedure/function variants, async clients, connection pools, partitioning, Citus/Oracle-compatible PostgreSQL, and databases without Stroppy drivers are separate substantial work. No defensible full estimate until those profiles are selected.

SQL extraction and SDK registration are straightforward. Most uncertainty lies in loader fidelity, transaction/error behavior, and proving that the measured interval and terminal behavior match.

## Validation needed before claiming parity

Compare extracted SQL with the pinned source; check transaction selection and client input distributions with scripted draws; validate loader cardinalities, types, initial values, and relational relationships; test routine commits, expected rollback, and error paths on real databases. For database-state comparisons, use matched initial snapshots and controlled database-side randomness or invariants where identical random draws cannot be imposed. Compare timed measurement boundaries and database-derived NOPM/TPM separately from Stroppy's native metrics.

The contrib CI matrix currently lists only `select1` and `selectfile` and assumes both can run against noop. A new database-reading workload needs explicit matrix integration and an appropriate recording or real-database smoke path; adding its name alone is insufficient.

## Licensing decision before copying code

HammerDB declares GPLv3; contrib declares Apache-2.0. Copied or translated HammerDB SQL/code cannot simply be relabeled Apache-2.0. A derived workload needs an appropriate GPLv3 licensing arrangement, preserved upstream notices, and clear repository-level license boundaries. Apache-2.0 SDK code can be combined with GPLv3 code under the applicable terms; this does not relicense the SDK itself. Confirm the distribution arrangement before copying implementation into contrib. No HammerDB implementation has been copied into this worktree.
