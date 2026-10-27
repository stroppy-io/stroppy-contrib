#!/usr/bin/env python3
"""Copy the pinned upstream into a fresh directory and install deterministic test controls."""
import argparse
from pathlib import Path
import shutil
import subprocess

REVISION = "a9ca4818625deef91ff80f6c395a575ccae22b7c"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("reference", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    revision = subprocess.check_output(["git", "-C", str(args.reference), "rev-parse", "HEAD"], text=True).strip()
    if revision != REVISION:
        raise ValueError(revision)
    shutil.copytree(args.reference, args.output, ignore=shutil.ignore_patterns(".git"))
    path = args.output / "tpcc/load.go"
    path.write_text(path.read_text().replace("rand.Perm(orderPerDistrict)", "oracleGlobal.Perm(orderPerDistrict)").replace('"math/rand"', ''))
    for name in ("new_order.go", "payment.go", "delivery.go"):
        path = args.output / "tpcc" / name
        text = path.read_text().replace("time.Now()", "oracleNow()")
        path.write_text(text.replace('\n\t"time"', ''))
    path = args.output / "tpcc/oracle_test.go"
    text = (Path(__file__).parent / "reference_test.go.txt").read_text()
    text += '\nvar oracleGlobal *rand.Rand\nfunc oracleNow() time.Time { return time.Date(2020,1,2,3,4,5,0,time.Local) }\n'
    path.write_text(text)


if __name__ == "__main__":
    main()
