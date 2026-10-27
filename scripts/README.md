# Rolling collection releases

`.github/workflows/release.yml` builds one complete Stroppy collection, using
Stroppy's own public author tools. It never generates a replacement main package,
imports internal build APIs, or assembles workload plugins by hand.

## Build and validation

1. Resolve `github.com/stroppy-io/stroppy/v6@v6` to one stable exact version. All
   jobs use that resolved version; a new v7 cannot enter this pipeline.
2. Run four native jobs: `ubuntu-24.04` (linux/amd64), `ubuntu-24.04-arm`
   (linux/arm64), `macos-15-intel` (darwin/amd64), and `macos-15` (darwin/arm64).
3. Discover tracked workload modules from repository `*/go.mod` files. A new
   workload project is included automatically after merge. Auxiliary Go modules
   that are not workload projects are not supported by this discovery convention.
4. Test each module with the selected SDK using a temporary modfile, preserving
   its committed pin. Then run `stroppy build PATH` into a private empty catalog.
   Every project must add at least one workload; duplicates or build failures stop
   the job rather than silently omitting a project.
5. Run `stroppy export --all`. Reject an export whose linked SDK version differs
   from the selected version, including accidental upgrades from a workload pin.
6. Execute the exported artifact with a clean home/cwd, no builder catalog, and
   no source-root flag. Compare its entire catalog with built-ins plus discovered
   project names; probe every entry and perform filtered execution of every
   contrib workload with all declared steps disabled. This validates embedded
   assets/definition replay without touching a database. An inline noop query
   additionally exercises the exported runner's real query path.
7. Package only verified native artifacts, with exact provenance, notices,
   checksums, and corresponding sources. Publish after every target succeeds.

Workload-owned unit/recording tests run with the selected SDK. Existing real-DB
integration jobs remain separate; generic release smoke testing cannot safely
execute arbitrary database-mutating workload steps. A package's passing filtered
smoke is not a benchmark or a substitute for its own database parity checks.

The bootstrap uses the official Stroppy module and the ordinary Go install path.
The exported binary includes official built-in workloads but does not add the
upstream release-only `pgnoop_embed` build tag; baseline may use Stroppy's normal
cache/download resolution for pg-noop.

## Publication

Pull requests run the complete build/validation matrix but have read-only
permissions and never publish. Main pushes, manual main runs, and a weekly main
schedule can update the rolling collection; the schedule picks up stable v6 SDK
updates. Workflows from forks cannot publish to upstream.

Only the publish job receives `contents: write`. Before touching `latest`, it
checks all four archives/checksums and requires identical contrib commit, exact
SDK, project names, and workload names across targets. It then updates the
existing release and tag, uploads the four archives, and uploads `SHA256SUMS`
last. A first release is created as a draft until assets are ready. Failed build
jobs leave the previous release unchanged. GitHub asset replacement is not an
atomic multi-file transaction: users downloading during publication can see a
checksum mismatch and should retry. No release is deleted to publish an update.

Publication is serialized rather than canceling an in-flight asset upload.
A completed run whose commit is no longer upstream main skips publication, so
queued old work cannot roll back a newer collection.

## Local native build

Requirements: Go 1.27+, native C compiler for the race detector, Git, Bash, jq,
Go module network/cache access, tar, and shasum. Run from any directory:

```sh
version=$(go list -m -json github.com/stroppy-io/stroppy/v6@v6 | jq -r .Version)
bash scripts/build-release.sh "$version" /tmp/stroppy-contrib-dist-new
```

The output directory must not exist. Build/export use a private temporary HOME,
but retain the caller's Go caches and inherited module access configuration.
No personal Stroppy workload catalog or installed executable is modified.
The script refuses cross-target builds because the release contract requires
executing each target artifact before upload.

`sources.tar.gz` includes linked Go dependency source trees, workload compiler
snapshots, an index of module paths/versions, and tracked contrib sources.
The collection's combined license obligations depend on its projects; individual
LICENSE/NOTICE files are not rewritten into an Apache-only claim. Go toolchain
source is available from the version recorded in the manifest/build info.
