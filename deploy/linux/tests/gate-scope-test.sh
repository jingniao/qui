#!/usr/bin/env bash
# scripts/gate-scope.sh decides whether a release may skip the Go, deploy and
# real-core parts of the gate. Each case builds a throwaway repository with a
# release tag, commits a change and checks the answer. Needs only git and bash.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
script=$root/scripts/gate-scope.sh
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

pass=0
fail=0

# new_repo creates a repository whose first commit carries tag v1.0.0 and holds
# one file in every area the scope rule distinguishes.
new_repo() {
  local dir=$work/$1
  mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" config user.email gate-scope@test
  git -C "$dir" config user.name gate-scope
  git -C "$dir" config core.autocrlf false
  mkdir -p "$dir/scripts" "$dir/web/src" "$dir/docs" "$dir/internal/x" "$dir/migrations" "$dir/.github/workflows"
  cp -- "$script" "$dir/scripts/gate-scope.sh"
  printf 'a\n' >"$dir/web/src/app.ts"
  printf 'package web\n' >"$dir/web/embed.go"
  printf 'a\n' >"$dir/docs/guide.md"
  printf 'a\n' >"$dir/AGENTS.md"
  printf 'package x\n' >"$dir/internal/x/x.go"
  printf 'a\n' >"$dir/migrations/0001_a.sql"
  printf 'a\n' >"$dir/.github/workflows/check.yml"
  printf 'module m\n' >"$dir/go.mod"
  git -C "$dir" add -A
  git -C "$dir" commit -q -m base
  git -C "$dir" tag v1.0.0
  printf '%s\n' "$dir"
}

commit_change() {
  local dir=$1
  shift
  local file
  for file in "$@"; do
    mkdir -p "$dir/$(dirname -- "$file")"
    printf 'changed %s\n' "$RANDOM" >>"$dir/$file"
  done
  git -C "$dir" add -A
  git -C "$dir" commit -q -m change
}

expect() {
  local name=$1 want=$2 dir=$3 rev=${4:-HEAD} got
  got=$(bash "$dir/scripts/gate-scope.sh" "$rev" 2>/dev/null)
  if [[ $got == "$want" ]]; then
    printf 'ok - %s: %s\n' "$name" "$got"
    pass=$((pass + 1))
  else
    printf 'not ok - %s: want %s, got %s\n' "$name" "$want" "$got"
    bash "$dir/scripts/gate-scope.sh" "$rev" >/dev/null || true
    fail=$((fail + 1))
  fi
}

d=$(new_repo web-only)
commit_change "$d" web/src/app.ts web/src/new.vue
expect 'frontend source only' web "$d"

d=$(new_repo web-and-docs)
commit_change "$d" web/src/app.ts docs/guide.md AGENTS.md
expect 'frontend source and documentation' web "$d"

d=$(new_repo docs-only)
commit_change "$d" docs/release-notes.md
expect 'documentation only' web "$d"

d=$(new_repo embed)
commit_change "$d" web/embed.go
expect 'web/embed.go is Go' full "$d"

d=$(new_repo go)
commit_change "$d" web/src/app.ts internal/x/x.go
expect 'frontend plus Go' full "$d"

d=$(new_repo gomod)
commit_change "$d" go.mod
expect 'go.mod' full "$d"

d=$(new_repo migration)
commit_change "$d" migrations/0002_b.sql
expect 'a migration' full "$d"

d=$(new_repo ci)
commit_change "$d" .github/workflows/check.yml
expect 'the workflow' full "$d"

d=$(new_repo scripts)
commit_change "$d" scripts/check.sh
expect 'a gate script' full "$d"

d=$(new_repo top-level)
commit_change "$d" package.json
expect 'a top-level file that is not Markdown' full "$d"

# The changes are summed since the release, not judged per commit: a Go change
# two commits back still needs the whole gate.
d=$(new_repo cumulative)
commit_change "$d" internal/x/x.go
commit_change "$d" web/src/app.ts
expect 'Go change earlier in the release' full "$d"

# A commit that already carries its own tag compares with the release before it.
d=$(new_repo tagged)
commit_change "$d" web/src/app.ts
git -C "$d" tag v1.0.1
expect 'tagged commit compares with the previous tag' web "$d"

# A web-only release on top of a web-only release is still web-only.
d=$(new_repo chained)
commit_change "$d" web/src/app.ts
git -C "$d" tag v1.0.1
commit_change "$d" web/src/app.ts
expect 'second web-only release' web "$d"

d=$(new_repo unchanged)
expect 'nothing since the release' full "$d"

# No release tag at all: nothing to compare with.
d=$work/untagged
mkdir -p "$d/scripts" "$d/web"
git -C "$d" init -q
git -C "$d" config user.email gate-scope@test
git -C "$d" config user.name gate-scope
git -C "$d" config core.autocrlf false
cp -- "$script" "$d/scripts/gate-scope.sh"
printf 'a\n' >"$d/web/a.ts"
git -C "$d" add -A
git -C "$d" commit -q -m one
printf 'b\n' >>"$d/web/a.ts"
git -C "$d" commit -q -am two
expect 'no release tag' full "$d"

# A git archive has no history: the gate tree on the test machine.
d=$work/archive
mkdir -p "$d/scripts"
cp -- "$script" "$d/scripts/gate-scope.sh"
got=$(cd "$d" && GIT_CEILING_DIRECTORIES=$work bash scripts/gate-scope.sh 2>/dev/null)
if [[ $got == full ]]; then
  printf 'ok - no git repository: full\n'
  pass=$((pass + 1))
else
  printf 'not ok - no git repository: want full, got %s\n' "$got"
  fail=$((fail + 1))
fi

d=$(new_repo unknown)
expect 'unknown commit' full "$d" no-such-revision

printf '%s passed, %s failed\n' "$pass" "$fail"
((fail == 0))
