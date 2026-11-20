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
fixture_parent=/run
if [[ $init_system == systemd ]]; then fixture_parent=/opt; fi
T=$(mktemp -d "$fixture_parent/qui-tuning-test.XXXXXXXX")
service_name=qui-tuning-test-$$.service
path_name=qui-tuning-test-$$.path
probe_name=qui-tuning-probe-$$.service
recovery_name=qui-tuning-recovery-$$.service
reset_name=qui-tuning-reset-$$.service
watcher_pid=''
chmod 0711 "$T"
mkdir "$T/tuning"
request_uid=$(id -u nobody) || { printf 'SKIP tuning-helper-test: needs nobody fixture account.\n'; rmdir "$T/tuning" "$T"; exit 0; }
chown "$request_uid" "$T/tuning"
chmod 0700 "$T/tuning"
cleanup() {
  set +e
  if [[ $init_system == systemd ]]; then
    systemctl stop "$path_name" "$service_name" "$probe_name" "$recovery_name" "$reset_name" >/dev/null 2>&1
    rm -f -- "/run/systemd/system/$service_name" "/run/systemd/system/$path_name" "/run/systemd/system/$probe_name" "/run/systemd/system/$recovery_name" "/run/systemd/system/$reset_name"
    systemctl daemon-reload
    systemctl reset-failed "$service_name" "$path_name" >/dev/null 2>&1
  elif [[ -n $watcher_pid ]]; then
    kill "$watcher_pid" 2>/dev/null
  fi
  [[ $T == "$fixture_parent"/qui-tuning-test.* ]] && rm -rf -- "$T"
}
trap cleanup EXIT
cat > "$T/helper" <<'HELPER'
#!/bin/sh
set -eu
d=$2
if [ "$1" = --probe-tuning ]; then printf 'probed\n' > "$d/probe.json"; exit 0; fi
[ "$1" = --apply-tuning ] || exit 2
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
  mkdir "$T/probe-required"
  sed -e "s|^User=.*|User=nobody|" -e "s|^Group=.*|Group=$(id -gn nobody)|" \
    -e "s|/var/lib/proxy-agent/tuning|$T/tuning|g" "$script_dir/../../systemd/proxy-agent-tuning-probe-reset.service" > "/run/systemd/system/$reset_name"
  for mode in apply probe; do
    source_unit=proxy-agent-tuning.service; dest_unit=$service_name; writable=$T/tuning
    if [[ $mode == probe ]]; then source_unit=proxy-agent-tuning-probe.service; dest_unit=$probe_name; writable="$T/tuning $T/probe-required"; fi
    sed -e "s|^ExecStart=.*|ExecStart=/bin/sh $T/helper --$mode-tuning $T/tuning|" \
    -e '/^ReadWritePaths=/d' -e 's/^CapabilityBoundingSet=.*/CapabilityBoundingSet=CAP_DAC_OVERRIDE/' \
    -e "s|proxy-agent-tuning-probe-reset.service|$reset_name|g" \
    -e "/^\[Service\]/a ReadWritePaths=$writable" -e '/^\[Install\]/,$d' \
    "$script_dir/../../systemd/$source_unit" > "/run/systemd/system/$dest_unit"
  done
  sed -e "s|/var/lib/proxy-agent/tuning|$T/tuning|g" \
    -e "s|^Unit=.*|Unit=$service_name|" -e '/^\[Install\]/,$d' \
    "$script_dir/../../systemd/proxy-agent-tuning.path" > "/run/systemd/system/$path_name"
  # Start the production boot-recovery branch from a real systemd oneshot.
  # These jobs cannot run until this unit exits. A synchronous start would
  # deadlock and hit TimeoutStartSec; unrelated system units are never named.
  mkdir "$T/recovery"
  printf 'previous receipt\n' > "$T/recovery/old-probe.json"
  cat > "$T/recover" <<EOF
set -eu
export TUNING_TEST_PROBE_UNIT=$probe_name TUNING_TEST_PATH_UNIT=$path_name TUNING_TEST_APPLY_UNIT=$service_name
/bin/bash "$script_dir/tuning-recovery-fixture.sh" boot "$T/recovery"
test ! -e "$T/tuning/probe.json"
EOF
  cat > "/run/systemd/system/$recovery_name" <<EOF
[Unit]
Before=$probe_name $path_name $service_name $reset_name
[Service]
Type=oneshot
TimeoutStartSec=15
ExecStart=/bin/bash $T/recover
EOF
  systemctl daemon-reload
  systemctl start "$recovery_name"
  for ((i=0;i<100;i++)); do
    if [[ -s $T/tuning/probe.json ]] && systemctl is-active --quiet "$path_name"; then break; fi
    sleep 0.1
  done
  systemctl is-active --quiet "$path_name"
  [[ -s $T/tuning/probe.json && ! -e $T/tuning/status.json ]]
  # Same executable, same boot: force the mount namespace to fail before Go.
  rmdir "$T/probe-required"
  if systemctl restart "$probe_name"; then printf 'FAIL: probe started without required mount\n' >&2; exit 1; fi
  [[ $(systemctl show -p ExecMainStatus --value "$probe_name") == 226 ]]
  [[ ! -e $T/tuning/probe.json ]]
  mkdir "$T/probe-required"
  systemctl restart "$probe_name"
  [[ -s $T/tuning/probe.json && ! -e $T/tuning/status.json ]]
  systemctl start "$path_name" "$service_name"
else
  sed -e "s|^DIR=.*|DIR=$T/tuning|" -e "s|^HELPER=.*|HELPER=$T/helper|" -e "s|^RUN=.*|RUN=$T|" \
    "$script_dir/../../openrc/tuning-watch" > "$T/watch"
  chmod 0755 "$T/watch"
  "$T/watch" & watcher_pid=$!
fi
wait_status boot applied
[[ -s $T/tuning/probe.json ]]
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
