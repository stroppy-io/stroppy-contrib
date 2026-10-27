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

## Download the complete collection

The [rolling latest release](https://github.com/stroppy-io/stroppy-contrib/releases/tag/latest)
provides a normal `stroppy` executable containing the official built-ins and every
workload project merged into this repository. No Go compiler, source checkout,
or local workload catalog is needed to run the download.

Targets match Stroppy's base release platforms: Linux and macOS, each for amd64
and arm64. Archives are named `stroppy-contrib_<os>_<arch>.tar.gz`; each contains
`stroppy`, `manifest.json`, Go/export provenance, license notices, and
`sources.tar.gz` with the matching linked dependency and project sources.

Download the archive for your platform and `SHA256SUMS` from the same release,
verify its checksum, then unpack and run:

```sh
grep '  stroppy-contrib_linux_amd64.tar.gz$' SHA256SUMS | shasum -a 256 -c -
tar -xzf stroppy-contrib_linux_amd64.tar.gz
./stroppy list
./stroppy run selectfile -d pg -D url='postgres://user@localhost:5432/postgres' --iterations 5
```

The release pins the **Stroppy v6 major**, resolves the newest stable v6 once per
workflow run, and records the exact SDK version plus contrib commit in every
archive. The same `latest` release/tag and asset names are updated after a
successful main-branch build; they are not immutable version pins. Keep the
manifest and checksum if you need to reproduce a downloaded build. During an
asset update, retry if checksums disagree. The release checksum file is uploaded
last as the completion marker.

Custom workloads are trusted native code. Source files and per-project licenses
travel with the download; a derived workload's license can add obligations to the
combined binary. Do not assume every future collection is Apache-only.

See [release pipeline details](scripts/README.md) for validation and local builds.
The first download appears only after the release workflow is merged and runs
successfully on upstream main.

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

`.github/workflows/ci.yml` does two things:

- **examples** — on every push and pull request, builds and tests each project
  against its pinned SDK, which is what a reader of this repository gets.
- **contract** — on a schedule and on demand, compiles the same projects against
  Stroppy `main` by replacing the SDK with a checkout. That is the run that
  notices when an upstream change breaks the authoring contract these samples
  depend on, without waiting for a release.

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

Apache 2.0, matching Stroppy.
