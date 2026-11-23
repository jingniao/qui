#!/usr/bin/env bash
# Root-owned once-only legacy Agent reset. It is deliberately separate from the
# updater journal: an interrupted reset resumes from the phase in its own fixed
# directory and never asks the old Agent to drain or archive telemetry.
set -euo pipefail
umask 077
directory=/var/lib/proxycontrol-install/agent-state-reset
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
exec 7>/var/lib/proxycontrol-install/update.lock
flock -x 7
exec 8>/var/lib/proxycontrol-install/operation.lock
flock -x 8
# Lock release also happens after an unsuccessful rollback. Do not discard the
# state while a durable recovery can still restore the old executable/config.
for journal in /var/lib/proxycontrol-install/rollback.*/journal /var/lib/proxycontrol-install/web-handover.*/journal; do
  if [[ -e $journal || -L $journal ]]; then
    printf 'legacy state reset requires update recovery to finish: %s\n' "$journal" >&2
    exit 1
  fi
done
phase=$(/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state status)
if [[ $phase == pending || -z $phase ]]; then
  # An older release may have armed the request without preparing a journal.
  # No identity binding exists yet; prepare against the installed executable.
  # Existing prepared/sealed journals keep their original target and reset ID.
  target=$(sha256sum /usr/local/bin/proxy-agent)
  target=${target%% *}
  /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state prepare --reset-target-sha256 "$target"
fi
if [[ $phase != rebuilt && $phase != complete ]]; then
  # seal takes the supervisor/worker lifecycle locks and retires the old identity;
  # stop its users first. A failed stop or seal must never reach deletion.
  if [[ -d /run/systemd/system ]]; then
    systemctl stop proxy-agent.service
    systemctl stop proxy-agent-host-network.path proxy-agent-tuning.path
    systemctl stop proxy-agent-host-network.service proxy-agent-tuning.service
  else
    rc-service proxy-agent stop
    rc-service proxy-agent-host-network stop
    rc-service proxy-agent-tuning stop
  fi
  /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state seal --reset-target-sha256 "$target"
  /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state rebuild --reset-target-sha256 "$target"
fi
# The root CLI creates fresh private files; return ownership before the service
# account opens them. Re-arm watchers against the newly created directories.
install -d -o proxy-agent -g proxy-agent -m 0700 /var/lib/proxy-agent/host-network
install -d -o proxy-agent -g proxy-agent -m 0700 /var/lib/proxy-agent/tuning
chown -R proxy-agent:proxy-agent /var/lib/proxy-agent
chown proxy-agent:proxy-agent /etc/proxy-agent/config.yaml
if [[ -d /run/systemd/system ]]; then
  systemctl restart proxy-agent-host-network.path proxy-agent-tuning.path
  systemctl start proxy-agent.service
else
  rc-service proxy-agent-host-network start 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-
  rc-service proxy-agent-tuning start 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-
  rc-service proxy-agent start 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-
fi
/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state complete --reset-target-sha256 "$target"
