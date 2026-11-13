#!/usr/bin/env bash
# Change-scoped quick check: run only the tiers a change can break, so the
# fix->release loop does not wait for the whole gate on every edit.
#
# scripts/check.sh stays the authority - it runs `npm ci`, the frontend
# self-tests, `go vet ./...`, `go test -race ./...`, both binaries and every
# deploy script test, and ~/.qui-local/gate.sh additionally runs the real-core
# suite. This script only answers "did I break what I touched?" and says out loud
# when a change cannot be judged without the full gate.
#
# Usage:
#   bash scripts/quick-check.sh                       # working tree (incl. staged) vs HEAD
#   bash scripts/quick-check.sh --base <rev>          # e.g. --base HEAD~1, or a tag
#   bash scripts/quick-check.sh --dry-run             # print the plan, run nothing
#   bash scripts/quick-check.sh --allow-full-gate-later
#
# Tiers, in the order they run:
#   text       CR bytes and UTF-8 BOM in every changed text file (always on)
#   web        npm run typecheck, npm run build, every web/scripts/*-selftest.mjs
#   deploy     bash -n on the changed scripts, the deploy/linux/tests/*.sh they touch
#   go         go vet + go test on the affected packages only
#   real-core  affected packages that hold real-core tests, when cores are configured
#
# A change under migrations/ or .github/ can only be judged by the full gate: the
# script then fails with "full gate required" unless --allow-full-gate-later says
# the gate will run before the change is committed.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"

usage() {
  printf 'usage: bash scripts/quick-check.sh [--base <rev>] [--dry-run] [--allow-full-gate-later]\n'
}

base=HEAD
dry_run=0
allow_later=0
while (($#)); do
  case $1 in
    --base)
      base=${2:?--base needs a revision}
      shift 2
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    --allow-full-gate-later)
      allow_later=1
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done
git rev-parse --verify --quiet "$base^{commit}" >/dev/null || {
  printf '未知的基准修订：%s\n' "$base" >&2
  exit 2
}

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

step() { printf '\n-- %s\n' "$*"; }

# The changed set: the working tree (staged and unstaged) against the base, plus
# untracked files. state/, .tools/, .worktrees/ and web/dist/ are local scratch
# and build output - nothing in them is gated.
# A deleted file stays in the list because it still decides which package a
# change belongs to; the content checks skip it by testing for the file.
changed=()
while IFS= read -r line; do
  changed+=("$line")
done < <({ git diff --name-only "$base" --; git ls-files --others --exclude-standard; } |
  LC_ALL=C sort -u | grep -Ev '^(state/|\.tools/|\.worktrees/|web/dist/)' || true)

if ((${#changed[@]} == 0)); then
  printf '没有改动（基准 %s）。\n' "$base"
  printf 'QUICK-CHECK PASS none\n'
  exit 0
fi

docs=() web=() deploy=() scripts_sh=() gof=() gate=()
for f in "${changed[@]}"; do
  case $f in *.md) docs+=("$f") ;; esac
  case $f in web/*) web+=("$f") ;; esac
  case $f in deploy/*) deploy+=("$f") ;; esac
  case $f in scripts/*.sh) scripts_sh+=("$f") ;; esac
  case $f in *.go | go.mod | go.sum) gof+=("$f") ;; esac
  # Files a package reads at run time belong to the package that owns the
  # directory, e.g. internal/platform/server/testdata/portal_route_coverage.txt.
  if [[ $f == internal/* && $f == */testdata/* ]]; then gof+=("$f"); fi
  case $f in migrations/* | .github/*) gate+=("$f") ;; esac
done

printf '基准 %s：%s 个改动文件（web %s，deploy/assets %s，scripts %s，Go %s，docs %s，迁移/CI %s）\n' \
  "$base" "${#changed[@]}" "${#web[@]}" "${#deploy[@]}" "${#scripts_sh[@]}" "${#gof[@]}" \
  "${#docs[@]}" "${#gate[@]}"

# grep -I reports a match for every line of a text file and none for a binary
# one, so this needs nothing beyond the tools the gate already assumes.
is_text() {
  [[ ! -s $1 ]] && return 0
  LC_ALL=C grep -Iq '' -- "$1"
}

BAD=0

# --- Tier text ----------------------------------------------------------------
# LF only, no BOM: a CR in a deploy script breaks the Linux test machine, and a
# BOM breaks shell shebangs and Go tooling. This runs for every changed text file
# in every tier, including the "only Markdown changed" case.
tier_text() {
  local f
  if ((dry_run)); then
    printf '检查 %s 个改动文件的 CR／BOM（仅 Markdown 改动时只跑这一项）。\n' "${#changed[@]}"
    return 0
  fi
  for f in "${changed[@]}"; do
    [[ -f $f ]] || continue
    is_text "$f" || continue
    if LC_ALL=C grep -q $'\r' -- "$f"; then
      printf '  ERROR CR   %s：含 CR 字节，源码与脚本必须是 LF\n' "$f"
      BAD=1
    fi
    if [[ $(head -c 3 -- "$f" | od -An -tx1 | tr -d ' \n') == efbbbf ]]; then
      printf '  ERROR BOM  %s：含 UTF-8 BOM\n' "$f"
      BAD=1
    fi
  done
  if ((BAD)); then
    printf '文本检查失败：改成 LF、去掉 BOM 后重跑。\n'
    return 1
  fi
  printf 'CR／BOM 检查通过。\n'
}

# --- Tier web -----------------------------------------------------------------
# check.sh runs `npm --prefix web ci`, the build and all thirteen self-tests; a
# source edit cannot change the lockfile, the dependencies or the time tests, so
# the tier stops at typecheck, build and the self-tests. The self-tests are the
# exact commands check.sh uses (they resolve their imports relative to their own
# file, so the working directory does not matter).
tier_web() {
  local rc=0 s
  if ((dry_run)); then
    if grep -q '"typecheck"' web/package.json; then printf 'npm --prefix web run typecheck\n'; fi
    printf 'npm --prefix web run build\n'
    printf 'node %s\n' web/scripts/*-selftest.mjs
    return 0
  fi
  if grep -q '"typecheck"' web/package.json; then
    step 'npm --prefix web run typecheck'
    npm --prefix web run typecheck || rc=1
  fi
  step 'npm --prefix web run build'
  npm --prefix web run build || rc=1
  step 'frontend self-tests'
  for s in web/scripts/*-selftest.mjs; do
    printf '\n-- node %s\n' "$s"
    node "$s" || rc=1
  done
  return $rc
}

# --- Tier deploy --------------------------------------------------------------
# Which deploy test a changed file can break. A changed test runs itself;
# install.sh is exercised by the panel installer, the release layout and the
# updater test; a tuning or host-network file by the helper test of that name.
# Anything under deploy/ that is not listed here is unknown territory, so every
# test runs - the same "if unsure run all of them" rule.
pick_deploy_tests() {
  local f unsure=0
  local -A want=()
  for f in "${deploy[@]}" "${scripts_sh[@]}"; do
    [[ -e $f ]] || continue
    case $f in
      deploy/linux/tests/*.sh) want[${f##*/}]=1 ;;
      deploy/linux/install.sh) want[panel-install-test.sh]=1 want[multi-core-test.sh]=1 want[update-test.sh]=1 ;;
      deploy/linux/update.sh) want[update-test.sh]=1 want[multi-core-test.sh]=1 ;;
      deploy/linux/node-install.sh) want[node-install-test.sh]=1 ;;
      deploy/linux/panel-update-request.sh) want[panel-update-helper-test.sh]=1 ;;
      scripts/gate-scope.sh) want[gate-scope-test.sh]=1 ;;
      deploy/systemd/proxy-panel-update.*) want[panel-install-test.sh]=1 want[multi-core-test.sh]=1 ;;
      deploy/systemd/proxy-agent-host-network.* | deploy/openrc/host-network-watch) want[host-network-test.sh]=1 ;;
      *tuning*) want[tuning-helper-test.sh]=1 ;;
      *host-network*) want[host-network-test.sh]=1 ;;
      *) unsure=1 ;;
    esac
  done
  if ((unsure)); then
    for f in deploy/linux/tests/*-test.sh; do want[${f##*/}]=1; done
  fi
  deploy_tests=()
  if ((${#want[@]})); then
    mapfile -t deploy_tests < <(printf '%s\n' "${!want[@]}" | LC_ALL=C sort)
  fi
}

# A .sh that is executable on Linux has to be 100755 in the index; 100644 works
# on the Windows dev box and fails on the test machine. This only warns: the
# index is not touched.
check_index_modes() {
  local f mode
  for f in "${deploy[@]}" "${scripts_sh[@]}"; do
    [[ $f == *.sh && -f $f ]] || continue
    mode=$(git ls-files -s -- "$f" | cut -c1-6)
    case $mode in
      100755) ;;
      '')
        printf 'WARNING %s：新文件尚未跟踪，须以 100755 提交（git add --chmod=+x %s，或 git add 后 git update-index --chmod=+x %s）\n' \
          "$f" "$f" "$f"
        ;;
      *)
        printf 'WARNING %s：索引模式 %s，应为 100755（git update-index --chmod=+x %s）\n' "$f" "$mode" "$f"
        ;;
    esac
  done
}

tier_deploy() {
  local rc=0 f t log
  local -a skips=()
  if ((dry_run)); then
    for f in "${deploy[@]}" "${scripts_sh[@]}"; do
      [[ $f == *.sh && -f $f ]] || continue
      printf 'bash -n %s\n' "$f"
    done
    pick_deploy_tests
    if ((${#deploy_tests[@]})); then
      printf 'bash deploy/linux/tests/%s\n' "${deploy_tests[@]}"
    fi
    printf '（同时检查 deploy/ 与 scripts/ 下 .sh 的索引模式）\n'
    return 0
  fi
  step 'bash -n（改动的 .sh）'
  for f in "${deploy[@]}" "${scripts_sh[@]}"; do
    [[ $f == *.sh && -f $f ]] || continue
    printf 'bash -n -- %s ... ' "$f"
    if bash -n -- "$f"; then printf 'ok\n'; else printf 'FAILED\n'; rc=1; fi
  done
  step '索引模式（deploy/ 与 scripts/ 下的 .sh）'
  check_index_modes
  pick_deploy_tests
  for t in "${deploy_tests[@]}"; do
    step "bash deploy/linux/tests/$t"
    log=$work/$t.log
    # These tests need root or Linux and skip themselves elsewhere; sudo is not
    # used here on purpose, so the fast loop never asks for a password.
    if bash "deploy/linux/tests/$t" 2>&1 | tee "$log"; then :; else rc=1; fi
    while IFS= read -r line; do
      skips+=("$t: $line")
    done < <(grep -E '^(SKIP |skip - )' -- "$log" || true)
  done
  if ((${#skips[@]})); then
    printf '\n注意：以下 deploy 测试在本机（%s）自我跳过，完整门禁会在 Linux 测试机上运行它们：\n' \
      "$(uname -s)"
    printf '  %s\n' "${skips[@]}"
  fi
  return $rc
}

# --- Tier go ------------------------------------------------------------------
# The affected set is the closed dependency set of the changed packages: a
# package has to run its tests when it - or anything that imports it - changed.
# `go list ./...` never descends into .worktrees/ or .tools/ (Go ignores
# directories starting with a dot), so nothing there can leak into the list.
declare -A ip_of_rel=() dir_of_ip=() deps_of=()
go_affected_ip=()
go_affected_rel=()
go_scope_done=0
go_module_dir=

compute_go_scope() {
  ((go_scope_done)) && return 0
  go_scope_done=1
  local out line ip dir deps rel d dep seed f all=0
  if ! out=$(go list -f '{{.ImportPath}}|{{.Dir}}|{{join .Deps " "}}' ./... 2>&1); then
    printf 'go list 失败，无法确定受影响的包：\n%s\n' "$out" >&2
    exit 1
  fi
  go_module_dir=$(go list -m -f '{{.Dir}}' | tr '\\' '/')
  local -a all_ip=()
  while IFS= read -r line; do
    [[ -n $line ]] || continue
    IFS='|' read -r ip dir deps <<<"$line"
    # Windows reports C:\...\internal\x; compare and print repo-relative paths.
    dir=$(printf '%s' "$dir" | tr '\\' '/')
    rel=${dir#"$go_module_dir"}
    rel=${rel#/}
    [[ -n $rel ]] || continue
    ip_of_rel[$rel]=$ip
    dir_of_ip[$ip]=$dir
    deps_of[$ip]=$deps
    all_ip+=("$ip")
  done <<<"$out"
  # A changed file belongs to the package of its directory, or of the nearest
  # parent that is a package - that is how a testdata/ file finds its package.
  local -A affected=()
  for f in "${gof[@]}"; do
    case $f in
      go.mod | go.sum)
        all=1
        continue
        ;;
    esac
    d=$(dirname -- "$f")
    while :; do
      if [[ -n ${ip_of_rel[$d]:-} ]]; then
        seed=${ip_of_rel[$d]}
        affected[$seed]=1
        break
      fi
      [[ $d == . ]] && break
      d=$(dirname -- "$d")
    done
  done
  if ((all)); then
    for ip in "${all_ip[@]}"; do affected[$ip]=1; done
  else
    # One pass is enough: .Deps is transitive, so a package that reaches a seed
    # through another package already lists the seed itself.
    for ip in "${all_ip[@]}"; do
      [[ -n ${affected[$ip]:-} ]] && continue
      for dep in ${deps_of[$ip]}; do
        if [[ -n ${affected[$dep]:-} ]]; then
          affected[$ip]=1
          break
        fi
      done
    done
  fi
  if ((${#affected[@]} == 0)); then
    return 0
  fi
  mapfile -t go_affected_ip < <(printf '%s\n' "${!affected[@]}" | LC_ALL=C sort)
  for ip in "${go_affected_ip[@]}"; do
    rel=${dir_of_ip[$ip]#"$go_module_dir"}
    rel=${rel#/}
    go_affected_rel+=("./$rel")
  done
}

# scripts/check.sh builds this fixture and points AGENT_BINARY at it; three
# updater tests skip without it, and a quick check that let them skip would
# report a pass the gate does not agree with.
build_agent_fixture_if_needed() {
  local ip
  for ip in "${go_affected_ip[@]}"; do
    if grep -qs AGENT_BINARY "${dir_of_ip[$ip]}"/*_test.go; then
      mkdir -p bin
      go build -trimpath -ldflags '-X main.version=1.0.0-storage-test' \
        -o bin/proxy-agent-storage-test ./cmd/agent
      AGENT_BINARY=$root/bin/proxy-agent-storage-test
      export AGENT_BINARY
      printf '已构建 Agent 测试夹具：AGENT_BINARY=%s\n' "$AGENT_BINARY"
      return 0
    fi
  done
}

# Format check on the changed .go files only, with the .tools/go gofmt when
# present (the Windows checkout has no go on PATH), else the one on PATH.
gofmt_changed() {
  local f gofmt_bin out
  local -a files=()
  for f in "${changed[@]}"; do
    [[ $f == *.go && -f $f ]] && files+=("$f")
  done
  ((${#files[@]})) || return 0
  gofmt_bin=gofmt
  if [[ -x .tools/go/bin/gofmt ]]; then
    gofmt_bin=.tools/go/bin/gofmt
  elif [[ -x .tools/go/bin/gofmt.exe ]]; then
    gofmt_bin=.tools/go/bin/gofmt.exe
  fi
  step "gofmt -l（${#files[@]} 个改动 .go 文件，$gofmt_bin）"
  out=$("$gofmt_bin" -l "${files[@]}") || return 1
  if [[ -n $out ]]; then
    printf '以下文件未格式化：\n%s\n' "$out"
    return 1
  fi
}

tier_go() {
  local rc=0 label=''
  # check.sh deliberately does not pass -p 1: its race run is long and every
  # package has its own temporary directory and ports. This tier is not that run,
  # and the Windows dev box is short on memory, so it serialises there.
  local -a pflags=()
  case $(uname -s) in
    MINGW* | MSYS* | CYGWIN*) pflags=(-p 1) ;;
  esac
  if ((${#pflags[@]})); then label="${pflags[*]} "; fi
  compute_go_scope
  if ((${#go_affected_ip[@]} == 0)); then
    printf '没有受影响的 Go 包。\n'
    return 0
  fi
  if ((dry_run)); then
    printf '受影响包（%s 个）：\n' "${#go_affected_ip[@]}"
    printf '  %s\n' "${go_affected_ip[@]}"
    printf 'go vet %s\n' "${go_affected_ip[*]}"
    printf 'go test -count=1 -timeout 30m %s%s\n' "$label" "${go_affected_ip[*]}"
    return 0
  fi
  gofmt_changed || rc=1
  build_agent_fixture_if_needed
  step "go vet（${#go_affected_ip[@]} 个包）"
  go vet "${go_affected_ip[@]}" || rc=1
  step "go test -count=1 ${label}（${#go_affected_ip[@]} 个包）"
  go test -count=1 -timeout 30m "${pflags[@]}" "${go_affected_ip[@]}" || rc=1
  return $rc
}

tier_realcore() {
  local rc=0 f rel known_pkg v triggered=0 ready=1
  local -a known=() pkgs=()
  compute_go_scope
  mapfile -t known < <(bash scripts/real-core-packages.sh)
  for rel in "${go_affected_rel[@]}"; do
    for known_pkg in "${known[@]}"; do
      if [[ $known_pkg == "$rel" ]]; then pkgs+=("$rel"); fi
    done
  done
  if ((${#pkgs[@]})); then triggered=1; fi
  for f in "${changed[@]}"; do
    case $f in internal/agent/* | proto/* | pkg/contracts/*) triggered=1 ;; esac
  done
  if ((!triggered)); then
    printf '不需要真实核心测试。\n'
    return 0
  fi
  if ((${#pkgs[@]})); then
    printf '需要真实核心测试：%s\n' "${pkgs[*]}"
  else
    printf '改动触及 internal/agent、proto 或 pkg/contracts，但受影响的包里没有真实核心测试包。\n'
  fi
  # scripts/ci-real-core-test.sh fails a step when a core variable is missing but
  # a test skipped, so it may only run where all five are set.
  for v in XRAY_BINARY SINGBOX_BINARY MIHOMO_BINARY MIHOMO_HOME GOST_BINARY; do
    if [[ -z ${!v:-} || ! -e ${!v} ]]; then ready=0; fi
  done
  if ((ready && ${#pkgs[@]})); then
    if ((dry_run)); then
      printf 'bash scripts/ci-real-core-test.sh -p 4 -count=1 -timeout 60m %s\n' "${pkgs[*]}"
      return 0
    fi
    step "bash scripts/ci-real-core-test.sh -p 4 -count=1 -timeout 60m（${#pkgs[@]} 个包）"
    bash scripts/ci-real-core-test.sh -p 4 -count=1 -timeout 60m "${pkgs[@]}" || rc=1
    return $rc
  fi
  printf '真实核心未配置（XRAY_BINARY／SINGBOX_BINARY／MIHOMO_BINARY／MIHOMO_HOME／GOST_BINARY）。\n'
  printf '这些包必须在 Linux 测试机上跑：~/.qui-local/gate.sh <rev>，或完整门禁 bash scripts/check.sh。\n'
  return 0
}

# --- main ---------------------------------------------------------------------
deploy_tests=()
realcore_wanted=0
if ((${#gof[@]})); then realcore_wanted=1; fi
for f in "${changed[@]}"; do
  case $f in internal/agent/* | proto/* | pkg/contracts/*) realcore_wanted=1 ;; esac
done

tiers=()
failed=0
run_tier() {
  printf '\n===== Tier %s =====\n' "$1"
  if ! "$2"; then failed=1; fi
  tiers+=("$1")
}

run_tier text tier_text
if ((${#web[@]})); then run_tier web tier_web; fi
if ((${#deploy[@]} || ${#scripts_sh[@]})); then run_tier deploy tier_deploy; fi
if ((${#gof[@]})); then run_tier go tier_go; fi
if ((realcore_wanted)); then run_tier real-core tier_realcore; fi

tier_list=${tiers[*]}

# A migration or a CI change can only be judged by the full gate, so this fails
# unless the caller promises to run it before the change is committed.
if ((${#gate[@]})); then
  printf '\n迁移/CI 变更：必须跑完整门禁（bash scripts/check.sh 或 ~/.qui-local/gate.sh）\n'
  if ((!allow_later && !dry_run)); then
    printf 'QUICK-CHECK FAIL %s 原因：full gate required\n' "$tier_list"
    exit 1
  fi
  if ((!dry_run)); then
    printf 'WARNING 已按 --allow-full-gate-later 放行：提交前仍须跑完整门禁。\n'
  fi
fi

if ((dry_run)); then
  printf '\nQUICK-CHECK PLAN %s\n' "$tier_list"
  exit 0
fi
if ((failed)); then
  printf '\nQUICK-CHECK FAIL %s\n' "$tier_list"
  exit 1
fi
printf '\nQUICK-CHECK PASS %s\n' "$tier_list"
