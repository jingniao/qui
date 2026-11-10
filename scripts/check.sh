#!/usr/bin/env bash
# The gate every change has to pass. It runs unchanged in two places:
#
#   * on the local Linux test machine, for every change (bash scripts/check.sh);
#   * in the `check` job of .github/workflows/check.yml, for a manual
#     (workflow_dispatch) or weekly (schedule) run on a clean runner.
#
# A v* tag does not run it again: the `release` job only builds when an earlie
# manual or weekly run already passed `check` on exactly that commit.
#
# A commit that changes only frontend source and documentation since the previous
# release tag runs the shorter `web` gate (see "Scope" below and
# scripts/gate-scope.sh); the weekly run always runs the whole gate.
#
# Needs Go (the version in go.mod), Node 22 with npm, and - for the deploy script
# tests - root or sudo. Nothing here depends on GitHub: GITHUB_ACTIONS=true only
# adds ::error:: annotations, and RUNNER_TEMP falls back to a temporary directory.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"

if [[ -n ${RUNNER_TEMP:-} ]]; then
  work=$RUNNER_TEMP
  cleanup=
else
  work=$(mktemp -d)
  cleanup=$work
fi
trap 'if [[ -n $cleanup ]]; then rm -rf -- "$cleanup"; fi' EXIT

annotate=0
[[ ${GITHUB_ACTIONS:-} == true ]] && annotate=1

step() { printf '\n== %s\n' "$*"; }

# Scope (scripts/gate-scope.sh): a release that changes only web/ (no Go file) and
# documentation since the previous release tag runs the `web` gate - the frontend
# steps below plus a panel build that proves the new assets are embedded - and
# skips go vet, go test -race, the Agent build and the deploy script tests, whose
# inputs did not change since the gate of that release. Everything else runs the
# whole gate. QUI_GATE_SCOPE=full forces the whole gate (the weekly run sets it);
# QUI_GATE_SCOPE=web is only for a caller that computed the scope itself where git
# history is available (~/.qui-local/gate.sh streams a git archive, which has none).
# Without the variable the scope is computed here, and a tree without git history
# answers `full`.
scope=${QUI_GATE_SCOPE:-}
if [[ -z $scope ]]; then
  scope=$(bash scripts/gate-scope.sh HEAD)
fi
case $scope in
  web | full) ;;
  *)
    printf 'QUI_GATE_SCOPE must be web or full, not %s.\n' "$scope" >&2
    exit 2
    ;;
esac
printf 'GATE SCOPE %s\n' "$scope"

# The deploy script tests activate packaged units, so the whole gate needs root.
# The CI runner has sudo; the test machine runs as root and has none. The web gate
# runs no deploy test and needs neither.
as_root=()
if [[ $scope == full ]]; then
  if command -v sudo >/dev/null 2>&1; then
    as_root=(sudo)
  elif ((EUID != 0)); then
    printf 'The deploy script tests need root or sudo.\n' >&2
    exit 1
  fi
fi

step 'npm --prefix web ci'
npm --prefix web ci

step 'npm --prefix web run build'
npm --prefix web run build

step 'npm run test:time'
npm run test:time

# The QR encoder is decoded again with jsQR, and the client policy simulator's
# address math is checked; both transpile the TypeScript sources in memory. The
# import refresh self-test runs the subscription page's real refresh actions
# against injected API and reactive-state fixtures.
step 'frontend self-tests'
node web/scripts/qr-selftest.mjs
node web/scripts/client-policy-selftest.mjs
node web/scripts/portal-selftest.mjs
node web/scripts/node-metrics-selftest.mjs
node web/scripts/latency-chart-selftest.mjs
node web/scripts/node-editor-selftest.mjs
node web/scripts/import-refresh-selftest.mjs
node web/scripts/tunnel-editor-selftest.mjs
node web/scripts/tunnels-selftest.mjs
node web/scripts/u1b-selftest.mjs
node web/scripts/server-detail-selftest.mjs
node web/scripts/ui-regression-selftest.mjs
node web/scripts/canvas-library-selftest.mjs
node web/scripts/node-address-selftest.mjs
node web/scripts/certificates-selftest.mjs
node web/scripts/publish-status-selftest.mjs

# The web gate ends here: build the panel exactly as a release does (the embed in
# web/embed.go reads web/dist) and check that every asset index.html references is
# inside the binary, so a frontend release cannot ship a panel that serves a stale
# or missing UI. embed.FS stores file names verbatim, so a plain byte search finds
# them.
if [[ $scope == web ]]; then
  step 'build the panel with the new frontend embedded'
  mkdir -p bin
  go build -o bin/proxy-panel ./cmd/panel
  mapfile -t embedded < <(grep -oE 'assets/[A-Za-z0-9._-]+\.(js|css)' web/dist/index.html | sort -u)
  if ((${#embedded[@]} == 0)); then
    printf 'web/dist/index.html references no assets/*.js or assets/*.css.\n' >&2
    exit 1
  fi
  for asset in "${embedded[@]}"; do
    if ! LC_ALL=C grep -aqF -- "${asset#assets/}" bin/proxy-panel; then
      printf 'bin/proxy-panel does not embed %s from web/dist/index.html.\n' "$asset" >&2
      exit 1
    fi
  done
  printf 'bin/proxy-panel embeds the %s assets web/dist/index.html references.\n' "${#embedded[@]}"
  printf '\nGATE SCOPE web: skipped go vet, go test -race, the Agent build and the deploy script tests (no file outside web/ and docs/ changed since the previous release).\n'
  exit 0
fi

step 'go vet ./...'
go vet ./...

# The Agent command-line and storage tests run against a real binary built with a
# fixed version string. AGENT_CANDIDATE_BINARY, AGENT_RELEASE_BINARY and
# PANEL_BINARY have no remaining consumer, so no other fixture is built.
step 'build the Agent fixture'
mkdir -p bin
go build -trimpath -ldflags '-X main.version=1.0.0-storage-test' -o bin/proxy-agent-storage-test ./cmd/agent

# platform/server took 34 min under -race at 141 tests (2026-10-28); there are 152
# now. Job logs need a signed-in viewer, so failing tests, panics and races are
# also raised as annotations (GitHub shows 10 per step), which the run page shows
# to anyone; locally the log stays in the temporary directory.
# go test already runs up to GOMAXPROCS packages at a time, and every package
# here keeps its own temporary directory and its own ephemeral ports, so do not
# add -p 1: it would serialise the ~26-minute race run.
step 'go test -race ./...'
test_log=$work/go-test.log
status=0
AGENT_BINARY=$root/bin/proxy-agent-storage-test go test -race -timeout 55m ./... 2>&1 | tee "$test_log" || status=$?
if ((status != 0)) && ((annotate)); then
  grep -E -- '^(--- FAIL|FAIL|panic:|WARNING: DATA RACE)|_test\.go:[0-9]+:' "$test_log" |
    head -n 10 | while IFS= read -r line; do echo "::error::$line"; done
fi
((status == 0)) || exit "$status"

step 'build the panel'
go build -o bin/proxy-panel ./cmd/panel

step 'build the agent'
go build -o bin/proxy-agent ./cmd/agent

# The deploy scripts are the only way an operator installs, upgrades or removes a
# host, so a syntax error in one of them is caught here rather than at the first
# install. The POSIX scripts are parsed by sh (dash on the CI runner) one file at
# a time, because sh -n only reads its first operand, and they also have to run
# under busybox ash on Alpine. update-test.sh exercises the update and recovery
# paths under QUI_UPDATE_TEST_ROOT and only needs the scripts, so its absence is
# not a failure while it lives in a separate change. host-network-test.sh runs the
# enforced-source units of the release against a fake helper below /run and skips
# itself where neither systemd nor OpenRC with busybox inotifyd is available.
# core-asset-test.sh extracts the architecture and libc helpers of install.sh and
# node-install.sh and needs no privileges, so it runs without sudo.
step 'deploy script checks'
for script in scripts/build-singbox.sh scripts/build-release.sh scripts/real-core-packages.sh \
  scripts/ci-real-core-test.sh scripts/gate-scope.sh deploy/linux/*.sh; do bash -n -- "$script"; done
for script in deploy/linux/update-recovery.sh deploy/openrc/host-network-watch \
  deploy/openrc/tuning-watch deploy/openrc/proxycontrol-update-recovery; do
  sh -n -- "$script"
done
if [ -f deploy/linux/tests/update-test.sh ]; then
  bash -n -- deploy/linux/tests/update-test.sh
  "${as_root[@]}" bash deploy/linux/tests/update-test.sh
fi
if [ -f deploy/linux/tests/host-network-test.sh ]; then
  bash -n -- deploy/linux/tests/host-network-test.sh
  "${as_root[@]}" bash deploy/linux/tests/host-network-test.sh
fi
if [ -f deploy/linux/tests/core-asset-test.sh ]; then
  bash -n -- deploy/linux/tests/core-asset-test.sh
  bash deploy/linux/tests/core-asset-test.sh
fi
# panel-install-test.sh exercises the one-command panel installer's argument and
# host validation, its configuration-template rewrite, its self-signed certificate
# generation and its offline release verification. Like core-asset-test.sh it needs
# no privileges and never installs anything.
if [ -f deploy/linux/tests/panel-install-test.sh ]; then
  bash -n -- deploy/linux/tests/panel-install-test.sh
  bash deploy/linux/tests/panel-install-test.sh
fi
# node-install-test.sh covers the one-command node installer's argument checks and
# the two network checks that used to fail late and vaguely: the panel certificate
# download (classified by curl's HTTP status and exit code) and the gRPC preflight
# before enrollment. It stubs curl and install below a temporary directory and needs
# no privileges.
if [ -f deploy/linux/tests/node-install-test.sh ]; then
  bash -n -- deploy/linux/tests/node-install-test.sh
  bash deploy/linux/tests/node-install-test.sh
fi
# tuning-helper-test.sh activates the packaged tuning units against a fake helpe
# below /run and never writes host sysctls; the other two are rootless.
"${as_root[@]}" bash deploy/linux/tests/tuning-helper-test.sh
# panel-update-helper-test.sh covers the root helper behind "update the panel from the
# web UI" against a fake installation and fake programs; it needs no privileges.
bash -n -- deploy/linux/tests/panel-update-helper-test.sh
bash deploy/linux/tests/panel-update-helper-test.sh
bash deploy/linux/tests/multi-core-test.sh
bash deploy/linux/tests/release-version-test.sh
# gate-scope-test.sh builds throwaway repositories with release tags and checks
# which changes scripts/gate-scope.sh lets through the shorter web gate.
bash deploy/linux/tests/gate-scope-test.sh
