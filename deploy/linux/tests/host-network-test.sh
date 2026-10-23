#!/usr/bin/env bash
# Regression test for the enforced-source units the node Agent publishes to:
# deploy/systemd/proxy-agent-host-network.service together with its .path unit,
# and the OpenRC watcher deploy/openrc/host-network-watch.
#
# Run it as root on Linux:
#
#   sudo bash deploy/linux/tests/host-network-test.sh
#
# The defect it pins: every start of the oneshot unit counts against systemd's
# default start limit (5 starts in 10 s), successful or not. A helper that failed
# a few times in a row therefore stopped the path unit with
# unit-start-limit-hit, and every later publish - including a clear request -
# was ignored until the node was reinstalled or rebooted. The test generates
# copies of the packaged units below /run, points them at a fake helper in a
# temporary directory and publishes the way the Agent does: eight failing
# requests, then a clear request that has to be applied. The same sequence runs
# through the OpenRC watcher, where failures were already survivable.
#
# Nothing of the real installation is touched: no proxy-agent unit, no
# /var/lib/proxy-agent and no nftables table. The fake helper only writes files,
# and everything it needs lives below one temporary directory below /run, which
# the packaged units may write to without extra privileges.
set -euo pipefail

if [[ $(uname -s) != Linux ]]; then
  printf 'SKIP host-network-test: needs Linux (uname -s %s).\n' "$(uname -s)"
  exit 0
fi
if [[ ${EUID} -ne 0 ]]; then
  printf 'host-network-test: needs root (EUID %s).\n' "${EUID}" >&2
  exit 1
fi

script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
service_unit=$script_dir/../../systemd/proxy-agent-host-network.service
path_unit=$script_dir/../../systemd/proxy-agent-host-network.path
watch_script=$script_dir/../../openrc/host-network-watch

# systemd if it is the running init, OpenRC only when the watcher can do its job:
# it hands the directory to busybox inotifyd.
init_system=''
if [[ -d /run/systemd/system ]] && command -v systemctl >/dev/null 2>&1; then
  init_system=systemd
elif [[ -x /sbin/openrc-run ]] \
  && { [[ -x /sbin/inotifyd ]] || command -v inotifyd >/dev/null 2>&1; }; then
  init_system=openrc
fi
if [[ -z $init_system ]]; then
  printf 'SKIP host-network-test: neither systemd nor OpenRC with busybox inotifyd.\n'
  exit 0
fi

if [[ $init_system == systemd ]]; then
  under_test=("$service_unit" "$path_unit")
else
  under_test=("$watch_script")
fi
for file in "${under_test[@]}"; do
  if [[ ! -f $file ]]; then
    printf 'host-network-test: missing %s\n' "$file" >&2
    exit 1
  fi
done

T=$(mktemp -d /run/qui-hn-test.XXXXXX) || exit 1
mkdir -p -- "$T/hn"
# The Agent's directory is 0700 and the packaged units inherit its umask; the
# fake helper is the only writer here.
chmod 0700 -- "$T/hn"
test_id=$$
service_name=qui-hn-test-$test_id.service
path_name=qui-hn-test-$test_id.path
watcher_pid=''
passes=0
failed=0

check() {
  if [[ $2 == ok ]]; then
    printf 'ok - %s\n' "$1"
    passes=$((passes + 1))
  else
    printf 'not ok - %s\n' "$1"
    failed=$((failed + 1))
  fi
}

cleanup() {
  set +e
  if [[ $init_system == systemd ]]; then
    systemctl stop "$path_name" "$service_name" >/dev/null 2>&1
    rm -f -- "/run/systemd/system/$service_name" "/run/systemd/system/$path_name"
    systemctl daemon-reload >/dev/null 2>&1
    systemctl reset-failed "$service_name" "$path_name" 2>/dev/null || true
  else
    if [[ -n $watcher_pid ]]; then kill "$watcher_pid" 2>/dev/null; fi
    # The watcher execs inotifyd, so its children are what still hold the watch.
    if command -v pkill >/dev/null 2>&1; then pkill -f "$T/hn" 2>/dev/null || true; fi
  fi
  rm -rf -- "$T"
}
trap cleanup EXIT

# The fake stand-in for `proxy-agent --apply-host-network DIR`. It keeps the file
# protocol of the real helper - read desired.json, answer status.json atomically -
# but touches no nftables table: an address of 192.0.2.250 makes it fail, every
# other request succeeds, an absent request is the successful no-op the real
# helper performs at boot, and every answered request is counted in ../runs.
cat > "$T/helper" <<'HELPER'
#!/bin/sh
set -eu

directory=''
while [ $# -gt 0 ]; do
  case $1 in
    --apply-host-network) directory=$2; shift 2 ;;
    *) printf 'helper: unexpected argument %s\n' "$1" >&2; exit 2 ;;
  esac
done
[ -n "$directory" ] || exit 2
[ -f "$directory/desired.json" ] || exit 0

id=$(sed -n 's/.*"id":"\([^"]*\)".*/\1/p' "$directory/desired.json")
printf '%s\n' "$id" >> "$directory/../runs"

if grep -q '"ipv4":"192.0.2.250"' "$directory/desired.json"; then
  printf '{"id":"%s","ok":false,"error":"test failure"}\n' "$id" > "$directory/.status.tmp"
  mv -f "$directory/.status.tmp" "$directory/status.json"
  exit 1
fi
printf '{"id":"%s","ok":true}\n' "$id" > "$directory/.status.tmp"
mv -f "$directory/.status.tmp" "$directory/status.json"
exit 0
HELPER
chmod 0755 -- "$T/helper"

# Publishing exactly like the Agent: one hidden temporary file renamed into place.
publish_request() {
  printf '{"id":"%s","ipv4":"%s","ipv6":"","exempt_ipv4":"","exempt_ipv6":""}\n' \
    "$1" "$2" > "$T/hn/.desired.tmp"
  mv -f -- "$T/hn/.desired.tmp" "$T/hn/desired.json"
}
publish_failure() { publish_request "$1" 192.0.2.250; }
publish_clear() { publish_request "$1" ''; }

status_id() {
  sed -n 's/.*"id":"\([^"]*\)".*/\1/p' "$T/hn/status.json" 2>/dev/null || true
}

status_ok() {
  grep -q '"ok":true' "$T/hn/status.json" 2>/dev/null
}

# Poll for the status of one id, in tenths of a second.
wait_for_status() {
  local id=$1 attempts=$(( $2 * 10 ))
  while (( attempts > 0 )); do
    if [[ -f $T/hn/status.json ]] && [[ $(status_id) == "$id" ]]; then
      return 0
    fi
    sleep 0.1
    attempts=$(( attempts - 1 ))
  done
  return 1
}

runs_count() {
  if [[ -f $T/runs ]]; then
    wc -l < "$T/runs" | tr -d ' '
  else
    printf '0'
  fi
}

if [[ $init_system == systemd ]]; then
  # The packaged units with only the paths, the program and the privileges
  # substituted. The whole [Unit] section is kept, so the start-limit setting of
  # the release under test is what runs here.
  sed \
    -e "s|/var/lib/proxy-agent/host-network|$T/hn|g" \
    -e "s|/var/lib/proxy-agent|$T|g" \
    -e "s|^ReadWritePaths=.*|ReadWritePaths=$T/hn $T|" \
    -e "s|^ExecStart=.*|ExecStart=/bin/sh $T/helper --apply-host-network $T/hn|" \
    -e '/^User=/d' \
    -e '/^Group=/d' \
    -e '/^CapabilityBoundingSet=/d' \
    -e '/^AmbientCapabilities=/d' \
    -e '/^\[Install\]/,$d' \
    "$service_unit" > "/run/systemd/system/$service_name"
  sed \
    -e "s|/var/lib/proxy-agent/host-network|$T/hn|g" \
    -e "s|^Unit=.*|Unit=$service_name|" \
    -e '/^\[Install\]/,$d' \
    "$path_unit" > "/run/systemd/system/$path_name"
  systemctl daemon-reload
  systemctl start "$path_name"
else
  sed \
    -e "s|^DIR=.*|DIR=$T/hn|" \
    -e "s|^HELPER=.*|HELPER=$T/helper|" \
    "$watch_script" > "$T/watch"
  chmod 0755 -- "$T/watch"
  "$T/watch" &
  watcher_pid=$!
  # The watcher applies the retained file and then execs inotifyd; give the watch
  # a moment to arm so the first publish cannot fall into the gap.
  sleep 1
fi

printf 'host-network-test: %s, temporary directory %s\n' "$init_system" "$T"

# Eight failing publishes. In the broken release the path unit stopped here and
# the clear below never arrived, so an unanswered failing request is recorded but
# does not fail that check by itself.
for index in 1 2 3 4 5 6 7 8; do
  publish_failure "fail-$index"
  if ! wait_for_status "fail-$index" 5; then
    printf 'note: fail-%s was not answered within 5 s\n' "$index"
  fi
done

publish_clear final
if wait_for_status final 15 && status_ok; then
  check 'a clear request after repeated helper failures is applied' ok
else
  check 'a clear request after repeated helper failures is applied' fail
fi

if [[ $init_system == systemd ]]; then
  if systemctl is-failed --quiet "$path_name"; then
    check 'the path unit is still watching' fail
  else
    check 'the path unit is still watching' ok
  fi
else
  if kill -0 "$watcher_pid" 2>/dev/null; then
    check 'the watcher is still running' ok
  else
    check 'the watcher is still running' fail
  fi
fi

runs=$(runs_count)
if [[ $runs == 9 ]]; then
  check 'the helper ran once per publish' ok
else
  printf 'note: the helper ran %s times, expected 9\n' "$runs"
  check 'the helper ran once per publish' fail
fi

# A failure immediately followed by a clear, without waiting for the failing
# answer: whatever the helper saw in between, the latest request must win.
publish_failure fail-last
publish_clear final-2
if wait_for_status final-2 15 && status_ok; then
  check 'the latest request wins after a failure' ok
else
  check 'the latest request wins after a failure' fail
fi

printf '\nhost-network-test: %s/%s passed\n' "$passes" "$((passes + failed))"
if ((failed > 0)); then exit 1; fi
exit 0
