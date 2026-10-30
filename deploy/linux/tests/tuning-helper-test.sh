#!/usr/bin/env bash
# Isolated activation test: real packaged units/watcher, fake file-only helper.
# Kernel writes/rollback are tested in internal/agent/tuning with a fake proc root.
# This never writes host sysctls or touches the installed proxy-agent services.
set -euo pipefail
if [[ $(uname -s) != Linux || $EUID != 0 ]]; then
  printf 'SKIP tuning-helper-test: needs Linux and root.\n'
  exit 0
fi
script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
init_system=''
if [[ -d /run/systemd/system ]] && command -v systemctl >/dev/null; then
  init_system=systemd
elif [[ -x /sbin/openrc-run && -x /sbin/inotifyd ]]; then
  init_system=openrc
else
  printf 'SKIP tuning-helper-test: needs systemd or OpenRC/inotifyd.\n'
  exit 0
fi
T=$(mktemp -d /run/qui-tuning-test.XXXXXXXX)
service_name=qui-tuning-test-$$.service
path_name=qui-tuning-test-$$.path
watcher_pid=''
mkdir "$T/tuning"
request_uid=$(id -u nobody) || { printf 'SKIP tuning-helper-test: needs nobody fixture account.\n'; rmdir "$T/tuning" "$T"; exit 0; }
chown "$request_uid" "$T/tuning"
chmod 0700 "$T/tuning"
cleanup() {
  set +e
  if [[ $init_system == systemd ]]; then
    systemctl stop "$path_name" "$service_name" >/dev/null 2>&1
    rm -f -- "/run/systemd/system/$service_name" "/run/systemd/system/$path_name"
    systemctl daemon-reload
    systemctl reset-failed "$service_name" "$path_name" >/dev/null 2>&1
  elif [[ -n $watcher_pid ]]; then
    kill "$watcher_pid" 2>/dev/null
  fi
  [[ $T == /run/qui-tuning-test.* ]] && rm -rf -- "$T"
}
trap cleanup EXIT
cat > "$T/helper" <<'HELPER'
#!/bin/sh
set -eu
[ "$1" = --apply-tuning ] || exit 2
d=$2
[ -f "$d/desired.json" ] || exit 0
id=$(sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p' "$d/desired.json")
status=applied
if grep -q '"clear":true' "$d/desired.json"; then status=cleared; fi
if grep -q '"bad"' "$d/desired.json"; then status=failed; fi
printf '{"request_id":"%s","status":"%s","keys":[]}\n' "$id" "$status" > "$d/.status.tmp"
mv -f "$d/.status.tmp" "$d/status.json"
[ "$status" != failed ]
HELPER
chmod 0755 "$T/helper"
publish() {
  printf '{"request_id":"%s","settings":%s,"clear":%s}\n' "$1" "$2" "$3" > "$T/tuning/.desired.tmp"
  chown "$request_uid" "$T/tuning/.desired.tmp"
  chmod 0600 "$T/tuning/.desired.tmp"
  mv -f "$T/tuning/.desired.tmp" "$T/tuning/desired.json"
}
wait_status() {
  local tries=100
  while (( tries-- > 0 )); do
    if [[ -f $T/tuning/status.json ]] && grep -q "\"request_id\":\"$1\"" "$T/tuning/status.json" \
      && grep -q "\"status\":\"$2\"" "$T/tuning/status.json"; then return 0; fi
    sleep 0.1
  done
  printf 'FAIL tuning-helper-test: missing result %s/%s\n' "$1" "$2" >&2
  return 1
}
# Retained request must replay at boot/start, before any new publish.
publish boot '{"net.ipv4.tcp_ecn":"1"}' false
if [[ $init_system == systemd ]]; then
  sed -e "s|^ExecStart=.*|ExecStart=/bin/sh $T/helper --apply-tuning $T/tuning|" \
    -e '/^ReadWritePaths=/d' -e 's/^CapabilityBoundingSet=.*/CapabilityBoundingSet=CAP_DAC_OVERRIDE/' \
    -e "/^\[Service\]/a ReadWritePaths=$T" -e '/^\[Install\]/,$d' \
    "$script_dir/../../systemd/proxy-agent-tuning.service" > "/run/systemd/system/$service_name"
  sed -e "s|/var/lib/proxy-agent/tuning|$T/tuning|g" \
    -e "s|^Unit=.*|Unit=$service_name|" -e '/^\[Install\]/,$d' \
    "$script_dir/../../systemd/proxy-agent-tuning.path" > "/run/systemd/system/$path_name"
  systemctl daemon-reload
  systemctl start "$path_name" "$service_name"
else
  sed -e "s|^DIR=.*|DIR=$T/tuning|" -e "s|^HELPER=.*|HELPER=$T/helper|" \
    "$script_dir/../../openrc/tuning-watch" > "$T/watch"
  chmod 0755 "$T/watch"
  "$T/watch" & watcher_pid=$!
fi
wait_status boot applied
sleep 1
for index in 1 2 3 4 5 6 7 8; do
  publish "fail-$index" '{"bad":"1"}' false
  wait_status "fail-$index" failed
done
publish clear '{}' true
wait_status clear cleared
publish after-clear '{"net.ipv4.tcp_ecn":"2"}' false
wait_status after-clear applied
if [[ $init_system == systemd ]]; then
  ! systemctl is-failed --quiet "$path_name"
else
  kill -0 "$watcher_pid"
fi
printf 'tuning-helper-test: boot replay, 8 failures, clear and later apply passed (%s).\n' "$init_system"
