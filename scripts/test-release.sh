#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
state=$(mktemp -d)
trap 'rm -rf "$state"' EXIT
mkdir -p "$state/bin" "$state/artifacts" "$state/package"
export GH_CALLS="$state/gh-calls"
cat > "$state/bin/gh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$GH_CALLS"
if [[ "$1 $2" == 'release view' && "${GH_MISSING:-0}" == 1 ]]; then exit 1; fi
if [[ "$1" == api && "$2" == *'/releases?per_page=100' ]]; then printf '[]\n'; fi
if [[ "$1" == api && "$2" == *'/git/matching-refs/tags/latest' ]]; then printf '[]\n'; fi
if [[ "$1" == api && "$2" == *'/git/ref/heads/main' ]]; then printf '%s\n' "${GH_HEAD:-$GITHUB_SHA}"; fi
STUB
chmod +x "$state/bin/gh"
export BASH_ENV=/dev/null
export PATH="$state/bin:$PATH" GITHUB_REPOSITORY=example/contrib GITHUB_SHA=0123456789012345678901234567890123456789 STROPPY_VERSION=v6.2.1
package() {
  local os=$1 arch=$2 commit=$3
  jq -n --arg os "$os" --arg arch "$arch" --arg commit "$commit" --arg version "$STROPPY_VERSION" \
    '{os:$os,arch:$arch,contrib_commit:$commit,stroppy_sdk:$version,projects:[{path:"test",names:["test"]}],workloads:["test"]}' > "$state/package/manifest.json"
  tar -C "$state/package" -czf "$state/artifacts/stroppy-contrib_${os}_${arch}.tar.gz" .
  (cd "$state/artifacts" && shasum -a 256 "stroppy-contrib_${os}_${arch}.tar.gz") > "$state/artifacts/stroppy-contrib_${os}_${arch}.tar.gz.sha256"
}
for os in linux darwin; do for arch in amd64 arm64; do package "$os" "$arch" "$GITHUB_SHA"; done; done
bash "$root/scripts/publish-release.sh" "$state/artifacts"
grep -q '^release upload latest .*SHA256SUMS' "$GH_CALLS"
[[ $(grep -c '^release upload' "$GH_CALLS") == 2 ]]
[[ $(grep '^release upload' "$GH_CALLS" | sed -n '$p') == *SHA256SUMS* ]]
# Both updates end with a checksum upload before the release metadata is finalized.
: > "$GH_CALLS"
GH_MISSING=1 bash "$root/scripts/publish-release.sh" "$state/artifacts"
grep -q '^release create latest .*--draft' "$GH_CALLS"
grep -q '^api --method POST .*git/refs' "$GH_CALLS"
: > "$GH_CALLS"
package linux arm64 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
if bash "$root/scripts/publish-release.sh" "$state/artifacts" > "$state/error" 2>&1; then
  printf 'publisher accepted a mismatched target commit\n' >&2; exit 1
fi
[[ ! -s "$GH_CALLS" ]]
package linux arm64 "$GITHUB_SHA"
GH_HEAD=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa bash "$root/scripts/publish-release.sh" "$state/artifacts"
[[ $(wc -l < "$GH_CALLS" | tr -d ' ') == 1 ]]
: > "$GH_CALLS"
printf 'corruption' >> "$state/artifacts/stroppy-contrib_linux_arm64.tar.gz"
if bash "$root/scripts/publish-release.sh" "$state/artifacts" > "$state/error" 2>&1; then
  printf 'publisher accepted a broken checksum\n' >&2; exit 1
fi
[[ ! -s "$GH_CALLS" ]]
printf 'Release publication guards passed\n'
