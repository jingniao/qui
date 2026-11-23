#!/usr/bin/env bash
# Root-owned once-only legacy Agent reset. It is deliberately separate from the
# updater journal: an interrupted reset resumes from the phase in its own fixed
# directory and never asks the old Agent to drain or archive telemetry.
set -euo pipefail
umask 077
directory=/var/lib/proxycontrol-install/agent-state-reset
state=$directory/state.json
request=$directory/request
target=${1:-}
if [[ -z $target && -f $request ]]; then target=$(cat -- "$request"); fi
[[ $target =~ ^[0-9a-f]{64}$ ]] || { echo 'reset requires target digest' >&2; exit 2; }
install -d -o root -g root -m 0700 "$directory"
exec 9>"$directory/job.lock"
flock -n 9 || exit 0
status=$(/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state status)
case $status in
  complete) exit 0;;
esac
# The updater lock is held for the whole replacement. Waiting here lets the
# health check complete and prevents an old rollback from reviving discarded data.
for lock in /var/lib/proxycontrol-install/operation.lock /var/lib/proxycontrol-install/update.lock; do
  exec 8>"$lock"
  flock -x 8
done
phase=$(/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state status)
if [[ $phase == pending || -z $phase ]]; then
  /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state prepare --reset-target-sha256 "$target"
fi
/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state seal --reset-target-sha256 "$target"
systemctl stop proxy-agent.service 2>/dev/null || rc-service proxy-agent stop
/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state rebuild --reset-target-sha256 "$target"
systemctl start proxy-agent.service 2>/dev/null || rc-service proxy-agent start
/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state complete --reset-target-sha256 "$target"
