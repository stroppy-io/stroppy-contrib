#!/usr/bin/env python3
"""Refresh the mechanically adapted go-tpc workload sources from the pinned checkout."""
import argparse
from pathlib import Path
import re
import subprocess

REVISION = "a9ca4818625deef91ff80f6c395a575ccae22b7c"


def clean(text):
    text = re.sub(r"^\s*//.*\n", "", text, flags=re.M)
    return text.replace("package tpcc", "package workload", 1)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("reference", type=Path)
    parser.add_argument("destination", type=Path)
    args = parser.parse_args()
    revision = subprocess.check_output(["git", "-C", str(args.reference), "rev-parse", "HEAD"], text=True).strip()
    if revision != REVISION:
        raise ValueError(f"expected {REVISION}, got {revision}")
    for name in ("new_order", "payment", "order_status", "delivery", "stock_level", "ddl", "check", "prepare"):
        text = clean((args.reference / "tpcc" / f"{name}.go").read_text())
        text = text.replace("(*sql.Tx, error)", "(*transaction, error)")
        text = text.replace("time.Now()", "w.now()")
        if name in ("new_order", "payment", "delivery"):
            text = text.replace('"time"', '')
        (args.destination / f"upstream_{name}.go").write_text(text)
    text = clean((args.reference / "tpcc/load.go").read_text())
    text = text.replace('"github.com/pingcap/go-tpc/pkg/sink"', '')
    text = text.replace("sink.NewSQLSink(w.db, hint, w.cfg.PrepareRetryCount, w.cfg.PrepareRetryInterval)", "newLoadSink(getTPCCState(ctx).Conn, hint, w.cfg.PrepareRetryCount, w.cfg.PrepareRetryInterval)")
    text = text.replace('"math/rand"', '').replace('"fmt"', '')
    text = text.replace("rand.Perm(orderPerDistrict)", "s.global.Perm(orderPerDistrict)")
    text = text.replace("fmt.Printf(", "quiet(")
    (args.destination / "upstream_load.go").write_text(text)
    text = clean((args.reference / "tpcc/rand.go").read_text())
    text = text.replace('"github.com/pingcap/go-tpc/pkg/util"', '')
    text = text.replace("util.BufAllocator", "buffer").replace("util.String", "string")
    text = re.sub(r"func convertToPQ\(query string, driver string\) string \{.*?\n\}", "func convertToPQ(query string, driver string) string { return query }", text, flags=re.S)
    text = text.replace('"fmt"', '').replace('"strings"', '')
    text = text.replace('"time"', '')
    text = re.sub(r"var \(\s*cLoad\s+int\s+cCustomerID\s+int\s+cItemID\s+int\s*\)", "", text)
    text = re.sub(r"func init\(\) \{.*?\n\}", "", text, flags=re.S)
    text = text.replace("func randCLast(r *rand.Rand, b *buffer)", "func randCLast(r *rand.Rand, b *buffer, cLoad int)")
    text = text.replace("func randCustomerID(r *rand.Rand)", "func randCustomerID(r *rand.Rand, cCustomerID int)")
    text = text.replace("func randItemID(r *rand.Rand)", "func randItemID(r *rand.Rand, cItemID int)")
    (args.destination / "upstream_rand.go").write_text(text)
    for path in args.destination.glob("upstream_*.go"):
        text = path.read_text().replace("randCLast(s.R, s.Buf)", "randCLast(s.R, s.Buf, w.constants.last)")
        text = text.replace("randCustomerID(s.R)", "randCustomerID(s.R, w.constants.customer)").replace("randItemID(s.R)", "randItemID(s.R, w.constants.item)")
        text = text.replace("fmt.Printf(", "quiet(")
        path.write_text("\n".join(line.rstrip() for line in text.splitlines()) + "\n")


if __name__ == "__main__":
    main()
