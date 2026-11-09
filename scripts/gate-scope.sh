#!/usr/bin/env bash
# Decide how much of the release gate a commit needs: `web` or `full`.
#
#   bash scripts/gate-scope.sh [<commit>]      # default HEAD
#
# Prints exactly one word on stdout and the reason on stderr. The answer is `web`
# only when every file changed between the previous release tag and <commit> is
# frontend source or documentation:
#
#   web/**        except any *.go file (web/embed.go decides what the panel embeds)
#   docs/**
#   *.md          at the top level (AGENTS.md, README.md)
#
# Everything else - Go, go.mod/go.sum, migrations, proto, deploy scripts, scripts/,
# .github/, configuration - and every case this script cannot judge (no git, no
# earlier v* tag, an unknown commit) answers `full`. It never answers `web` by
# default: a doubt always means the whole gate.
#
# The previous release is the nearest v* tag reachable from the commit's first
# parent, so a commit that already carries its own tag still compares against the
# release before it. That release passed its own gate, and nothing outside web/
# and docs/ changed since, so the Go code, the deploy scripts and the cores
# integration are the code that gate already tested.
set -uo pipefail

commit=${1:-HEAD}

full() {
  printf 'full\n'
  printf 'gate scope full: %s\n' "$*" >&2
  exit 0
}

# The repository is the one this script lives in; GATE_SCOPE_REPO points at
# another checkout for a caller that runs the script from a copy (gate.sh runs
# the copy stored in the commit it gates).
root=${GATE_SCOPE_REPO:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)} || full 'cannot locate the repository'
cd -- "$root" || full 'cannot enter the repository'

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || full 'no git repository (for example a git archive)'
sha=$(git rev-parse --verify --quiet "$commit^{commit}") || full "unknown commit $commit"
git rev-parse --verify --quiet "$sha^" >/dev/null || full 'the commit has no parent'
base=$(git describe --tags --abbrev=0 --match 'v[0-9]*' "$sha^" 2>/dev/null) || full 'no earlier v* release tag is reachable'

changed=$(git diff --name-only "$base" "$sha" --) || full "cannot diff $base..$sha"
if [[ -z $changed ]]; then
  full "nothing changed since $base"
fi

count=0
while IFS= read -r file; do
  [[ -n $file ]] || continue
  count=$((count + 1))
  case $file in
    *.go) full "$file is Go code (changed since $base)" ;;
    web/* | docs/*) ;;
    */*) full "$file is outside web/ and docs/ (changed since $base)" ;;
    *.md) ;;
    *) full "$file is outside web/ and docs/ (changed since $base)" ;;
  esac
done <<<"$changed"

printf 'web\n'
printf 'gate scope web: %s file(s) changed since %s, all under web/ or docs/ or top-level Markdown\n' "$count" "$base" >&2
