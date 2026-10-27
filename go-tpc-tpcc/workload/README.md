# go-tpc-derived TPC-C

Apache-2.0 workload derived from pingcap/go-tpc at
`a9ca4818625deef91ff80f6c395a575ccae22b7c`.

The workload preserves upstream transaction SQL, statement ordering, client-side
random generation, weighted selection, and load values. It uses Stroppy SDK
sessions and metrics instead of invoking the go-tpc runner.

Use `-d mysql` or `-d pg`. Actions are `--action prepare`, `--action run`,
`--action check`, and `--action cleanup`. Cleanup drops the nine workload tables;
only use a dedicated database. Runtime defaults to run, never destructive setup.

Explicit seeds are a reproducibility extension; seed zero uses wall-clock seeds
like upstream. No transaction retry policy is added. Loader final errors are
returned instead of reproducing upstream's silent exhaustion behavior.
