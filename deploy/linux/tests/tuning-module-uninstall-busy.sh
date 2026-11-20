#!/usr/bin/env bash
# Exercise the real removal ordering in an isolated tree with a busy oneshot.
set -euo pipefail
root=$(cd "$(dirname "$0")/../../.." && pwd -P)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
touch "$work/proxy-agent-tuning-modules.path"
sed -n '/^for i in .*record_cores/,/^# The panel.s update bridge has/{ /^# The panel.s update bridge has/!p; }' "$root/deploy/linux/uninstall.sh" |
  sed "s|/etc/systemd/system/|$work/|g" > "$work/uninstall"
rc=0
(
  set -euo pipefail
  record_cores=(); init_system=systemd; tuning_units=true; host_network_units=true
  host_network_path=proxy-agent-host-network.path; host_network_service=proxy-agent-host-network.service
  name=proxy-agent
  fail() { echo "$*" >&2; exit 17; }
  systemctl() {
    case "$1" in
      stop|disable|start) echo "$*" >> "$work/jobs";;
      show)
        case "$*" in
          *proxy-agent-tuning-modules.path*ActiveState*) echo active;;
          *proxy-agent-tuning-modules.service*ActiveState*) echo activating;;
          *Job*) echo 0;;
          *ActiveState*) echo inactive;;
          *Result*) echo success;;
          *) exit 19;;
        esac;;
      *) exit 20;;
    esac
  }
  . "$work/uninstall"
) || rc=$?
[[ $rc == 17 ]] || { echo "unexpected result $rc"; exit 1; }
actual=$(cat "$work/jobs")
[[ $actual == $'stop proxy-agent-tuning-modules.path\nstart proxy-agent-tuning-modules.path' ]] || { echo "$actual"; exit 1; }
