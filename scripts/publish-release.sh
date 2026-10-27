#!/usr/bin/env bash
set -euo pipefail

repository=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
commit=${GITHUB_SHA:?GITHUB_SHA is required}
version=${STROPPY_VERSION:?STROPPY_VERSION is required}
artifacts=${1:?usage: publish-release.sh ARTIFACT_DIRECTORY}
[[ "$commit" =~ ^[0-9a-f]{40}$ && "$version" =~ ^v6\.[0-9]+\.[0-9]+$ ]] || exit 1
state=$(mktemp -d)
trap 'rm -rf "$state"' EXIT
mkdir "$state/assets"
for os in linux darwin; do
  for arch in amd64 arm64; do
    name="stroppy-contrib_${os}_${arch}.tar.gz"
    archive="$artifacts/$name"
    [[ -f "$archive" && -f "$archive.sha256" ]] || { printf 'missing target %s\n' "$name" >&2; exit 1; }
    (cd "$artifacts" && shasum -a 256 -c "$name.sha256")
    tar -xOf "$archive" ./manifest.json > "$state/manifest.json"
    jq -e --arg os "$os" --arg arch "$arch" --arg commit "$commit" --arg version "$version" \
      '.os == $os and .arch == $arch and .contrib_commit == $commit and .stroppy_sdk == $version and (.projects | length > 0)' "$state/manifest.json" >/dev/null
    jq -S '{contrib_commit,stroppy_sdk,projects,workloads}' "$state/manifest.json" > "$state/collection.json"
    if [[ -f "$state/expected.json" ]]; then cmp "$state/expected.json" "$state/collection.json"; else cp "$state/collection.json" "$state/expected.json"; fi
    cp "$archive" "$state/assets/$name"
  done
done
(cd "$state/assets" && shasum -a 256 ./*.tar.gz | sed 's|  ./|  |') > "$state/assets/SHA256SUMS"
{
  printf '# Stroppy contrib — rolling latest\n\n'
  printf 'Contrib commit: %s\n\nStroppy SDK: %s (major pinned to v6)\n\n' "$commit" "$version"
  printf 'These archives contain one stroppy executable with the official built-ins and every workload module from this contrib revision. All four targets passed native tests, catalog/probe checks, and database-free packaged-binary smoke checks before publication.\n\n'
  printf '## Included contrib projects\n\n'
  jq -r '.projects[] | "- `" + .path + "`: " + (.names | map("`" + . + "`") | join(", "))' "$state/expected.json"
  printf '\nEach archive includes exact provenance, license notices, and corresponding dependency/project sources. Verify downloads with SHA256SUMS.\n\n'
  printf 'The latest tag and assets are replaced after successful main-branch builds; URLs remain stable. Save checksums and manifests to pin a particular build. Custom workloads are trusted native code.\n'
} > "$state/notes.md"

# A queued older run must not replace a collection built from newer main.
head=$(gh api "repos/$repository/git/ref/heads/main" --jq .object.sha)
if [[ "$head" != "$commit" ]]; then
  printf 'Skip stale release: built %s, main is %s\n' "$commit" "$head"
  exit 0
fi

# All artifacts are checked before the existing rolling release is touched.
if gh release view latest --repo "$repository" --json tagName > "$state/release.json" 2> "$state/release-error"; then
  gh api --method PATCH "repos/$repository/git/refs/tags/latest" -f sha="$commit" -F force=true >/dev/null
else
  # Distinguish an absent release from authentication or connectivity failures.
  gh api "repos/$repository/releases?per_page=100" > "$state/releases.json"
  jq -e 'all(.[]; .tag_name != "latest")' "$state/releases.json" >/dev/null
  gh api "repos/$repository/git/matching-refs/tags/latest" > "$state/tags.json"
  if jq -e 'any(.[]; .ref == "refs/tags/latest")' "$state/tags.json" >/dev/null; then
    gh api --method PATCH "repos/$repository/git/refs/tags/latest" -f sha="$commit" -F force=true >/dev/null
  else
    gh api --method POST "repos/$repository/git/refs" -f ref=refs/tags/latest -f sha="$commit" >/dev/null
  fi
  gh release create latest --repo "$repository" --verify-tag --title 'Stroppy contrib — latest' --notes-file "$state/notes.md" --draft
fi
gh release upload latest "$state/assets/"*.tar.gz --repo "$repository" --clobber
# The checksum file is the completion marker for a rolling asset update.
gh release upload latest "$state/assets/SHA256SUMS" --repo "$repository" --clobber
gh release edit latest --repo "$repository" --title 'Stroppy contrib — latest' --notes-file "$state/notes.md" --draft=false --latest
