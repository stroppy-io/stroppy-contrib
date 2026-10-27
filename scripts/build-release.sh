#!/usr/bin/env bash
set -euo pipefail

# A private catalog prevents personal workloads from entering the download.
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version=${1:?usage: build-release.sh STROPPY_VERSION OUTPUT_DIRECTORY}
output=${2:?usage: build-release.sh STROPPY_VERSION OUTPUT_DIRECTORY}
[[ "$version" =~ ^v6\.[0-9]+\.[0-9]+$ ]] || { printf 'expected a stable Stroppy v6 version\n' >&2; exit 1; }
[[ ! -e "$output" ]] || { printf 'output already exists: %s\n' "$output" >&2; exit 1; }
[[ $(go env GOOS) == "$(go env GOHOSTOS)" && $(go env GOARCH) == "$(go env GOHOSTARCH)" ]] || {
  printf 'release validation requires native execution\n' >&2; exit 1;
}
export CGO_ENABLED=0 GOWORK=off
GOMODCACHE=$(go env GOMODCACHE)
GOCACHE=$(go env GOCACHE)
GOPATH=$(go env GOPATH)
export GOMODCACHE GOCACHE GOPATH
state=$(mktemp -d)
trap 'chmod -R u+w "$state"; rm -rf "$state"' EXIT
mkdir -p "$state/home" "$state/bootstrap" "$state/package/licenses" "$state/sources" "$output"
output=$(cd "$output" && pwd)
export HOME="$state/home"
GOBIN="$state/bootstrap" go install "github.com/stroppy-io/stroppy/v6/cmd/stroppy@$version"
stroppy="$state/bootstrap/stroppy"
"$stroppy" version --json | jq -e --arg version "$version" '.stroppy == $version' >/dev/null
"$stroppy" list -o json | jq -S 'map(.name) | sort' > "$state/builtin-names.json"
printf '[]\n' > "$state/projects.json"
projects=()
while IFS= read -r file; do projects+=("${file%/go.mod}"); done < <(git -C "$root" ls-files -- '*/go.mod' | LC_ALL=C sort)
[[ ${#projects[@]} -gt 0 ]] || { printf 'no workload modules found\n' >&2; exit 1; }
index=0
for project in "${projects[@]}"; do
  directory="$root/$project"
  index=$((index + 1))
  testmod="$state/test-$index.mod"
  cp "$directory/go.mod" "$testmod"
  if [[ -f "$directory/go.sum" ]]; then cp "$directory/go.sum" "$state/test-$index.sum"; fi
  go -C "$directory" mod edit -modfile="$testmod" -require="github.com/stroppy-io/stroppy/v6@$version"
  CGO_ENABLED=1 go -C "$directory" test -mod=mod -modfile="$testmod" -race ./...
  "$stroppy" list -o json | jq 'map(select(.origin == "custom") | .name) | sort' > "$state/before.json"
  "$stroppy" build "$directory" -y
  "$stroppy" list -o json | jq 'map(select(.origin == "custom") | .name) | sort' > "$state/after.json"
  jq -s '.[1] - .[0]' "$state/before.json" "$state/after.json" > "$state/added.json"
  jq -e 'length > 0' "$state/added.json" >/dev/null
  module=$(go -C "$directory" list -m)
  jq --arg path "$project" --arg module "$module" --slurpfile names "$state/added.json" \
    '. + [{path:$path,module:$module,names:$names[0]}]' "$state/projects.json" > "$state/next.json"
  mv "$state/next.json" "$state/projects.json"
done
"$stroppy" list -o json | jq -S 'map(.name) | sort' > "$state/expected-names.json"
"$stroppy" export --all -o "$state/package/stroppy" -y > "$state/export.txt"
cat "$state/export.txt"
digest=$(cut -f5 "$state/export.txt")
"$stroppy" cache inspect "$digest" -o json > "$state/package/export-manifest.json"
jq -e --arg version "$version" '.stroppy_module_version == $version and .include_built_ins == true' "$state/package/export-manifest.json" >/dev/null
go version -m -json "$state/package/stroppy" > "$state/package/go-buildinfo.json"
jq -e --arg version "$version" '[.Deps[] | select(.Path == "github.com/stroppy-io/stroppy/v6" and .Version == $version)] | length == 1' "$state/package/go-buildinfo.json" >/dev/null

# Exercise the exact exported artifact outside its catalog and build directory.
mkdir -p "$state/clean-home" "$state/verify"
(cd "$state/verify" && HOME="$state/clean-home" "$state/package/stroppy" list -o json) > "$state/actual.json"
jq -S 'map(.name) | sort' "$state/actual.json" > "$state/actual-names.json"
cmp "$state/expected-names.json" "$state/actual-names.json"
while IFS= read -r name; do
  (cd "$state/verify" && HOME="$state/clean-home" "$state/package/stroppy" probe "$name" -o json) > "$state/probe.json"
  jq -e --arg name "$name" '.workloads | any(.name == $name)' "$state/probe.json" >/dev/null
done < <(jq -r '.[]' "$state/expected-names.json")
while IFS= read -r name; do
  (cd "$state/verify" && HOME="$state/clean-home" "$state/package/stroppy" probe "$name" -o json) > "$state/probe.json"
  steps=$(jq -r '[.workloads[].steps[].name] | unique | join(",")' "$state/probe.json")
  [[ -n "$steps" ]] || { printf 'workload declares no smoke-testable steps: %s\n' "$name" >&2; exit 1; }
  # Schema-only execution checks embedded assets without requiring a database.
  (cd "$state/verify" && HOME="$state/clean-home" "$state/package/stroppy" run "$name" --no-steps "$steps" --no-report)
done < <(jq -r '.[].names[]' "$state/projects.json")
(cd "$state/verify" && HOME="$state/clean-home" "$state/package/stroppy" run 'SELECT 1' -d noop --iterations 2 --no-report)

while IFS= read -r file; do
  mkdir -p "$state/package/licenses/$(dirname "$file")"
  cp "$root/$file" "$state/package/licenses/$file"
done < <(git -C "$root" ls-files -- '*LICENSE*' '*NOTICE*')

# Keep sources for every linked module alongside a combined-license executable.
cp "$state/package/go-buildinfo.json" "$state/sources/modules.json"
index=0
while IFS=$'\t' read -r module module_version source_path source_version; do
  index=$((index + 1))
  if [[ "$source_version" == - ]]; then
    directory="$source_path"
  else
    directory=$(go mod download -json "$source_path@$source_version" | jq -er .Dir)
  fi
  [[ -d "$directory" ]] || { printf 'missing source for %s\n' "$module" >&2; exit 1; }
  mkdir -p "$state/sources/module-$index"
  cp -R "$directory/." "$state/sources/module-$index/"
  printf '%s\t%s\t%s\n' "module-$index" "$module" "$module_version" >> "$state/sources/INDEX.tsv"
done < <(jq -r '.Deps[] | [.Path,.Version,(.Replace.Path // .Path),(if .Replace then (if (.Replace.Path | startswith("/")) then "-" else .Replace.Version end) else .Version end)] | @tsv' "$state/package/go-buildinfo.json")
cp "$state/package/export-manifest.json" "$state/sources/export-manifest.json"
mkdir -p "$state/sources/contrib"
while IFS= read -r file; do
  mkdir -p "$state/sources/contrib/$(dirname "$file")"
  cp "$root/$file" "$state/sources/contrib/$file"
done < <(git -C "$root" ls-files)
chmod -R u+w "$state/sources"
tar -C "$state/sources" -czf "$state/package/sources.tar.gz" .
commit=$(git -C "$root" rev-parse HEAD)
os=$(go env GOOS)
arch=$(go env GOARCH)
jq -n --arg commit "$commit" --arg sdk "$version" --arg os "$os" --arg arch "$arch" \
  --arg go "$(go version)" --slurpfile projects "$state/projects.json" --slurpfile names "$state/expected-names.json" \
  '{schema:1,contrib_commit:$commit,stroppy_sdk:$sdk,os:$os,arch:$arch,go:$go,projects:$projects[0],workloads:$names[0]}' > "$state/package/manifest.json"
cp "$root/README.md" "$state/package/README.md"
sdk_source=$(go mod download -json "github.com/stroppy-io/stroppy/v6@$version" | jq -er .Dir)
mkdir -p "$state/package/licenses/stroppy"
cp "$sdk_source/LICENSE" "$state/package/licenses/stroppy/LICENSE"
archive="stroppy-contrib_${os}_${arch}.tar.gz"
tar -C "$state/package" -czf "$output/$archive" .
mkdir -p "$state/unpacked" "$state/unpacked-home"
tar -xzf "$output/$archive" -C "$state/unpacked"
(cd "$state/verify" && HOME="$state/unpacked-home" "$state/unpacked/stroppy" list -o json) | jq -S 'map(.name) | sort' > "$state/unpacked-names.json"
cmp "$state/expected-names.json" "$state/unpacked-names.json"
(cd "$state/verify" && HOME="$state/unpacked-home" "$state/unpacked/stroppy" run 'SELECT 1' -d noop --iterations 2 --no-report)
(cd "$output" && shasum -a 256 "$archive") > "$output/$archive.sha256"
printf 'Verified release package: %s\n' "$output/$archive"
