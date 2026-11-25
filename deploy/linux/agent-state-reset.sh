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
# The root bridge outlives qui-update and performs its own final health check.
# Acquire its lock first: the bridge invokes an updater that takes update.lock.
install -d -o root -g root -m 0700 /var/lib/proxycontrol-install/agent-system-updates
exec 6>/var/lib/proxycontrol-install/agent-system-updates/bridge.lock
flock -x 6
# Keep deletion behind both the updater and the bridge's durable conclusion.
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
# The panel must accept each locally retained task before its tracking files
# can be discarded. Root's result remains outside the rebuilt data directory.
confirm_update_receipts() {
  local wait_for_ack=$1 tracked attempt result result_status result_digest expected_ack ack acknowledgement waiting
  for tracked in /var/lib/proxy-agent/system-update/*.request.json; do
    [[ -e $tracked ]] || continue
    attempt=${tracked##*/}; attempt=${attempt%.request.json}
    result=/var/lib/proxycontrol-install/agent-system-updates/$attempt/result.json
    [[ -f $result ]] || { printf 'legacy state reset requires a durable update result: %s\n' "$attempt" >&2; return 1; }
    result_status=$(sed -n 's/.*"status":"\([a-z_]*\)","phase".*/\1/p' "$result")
    case $result_status in
      success|failed) ;;
      *) printf 'legacy state reset requires a confirmed update outcome: %s\n' "$attempt" >&2; return 1;;
    esac
    result_digest=$(sha256sum "$result"); result_digest=${result_digest%% *}
    expected_ack="\"resultSha256\":\"$result_digest\""
    ack=/var/lib/proxy-agent/system-update/$attempt.ack.json
    waiting=false
    while :; do
      if [[ -f $ack ]]; then
        acknowledgement=$(cat "$ack")
        if [[ $acknowledgement == *"$expected_ack"* ]]; then break; fi
      fi
      if ! $wait_for_ack; then
        printf 'legacy state reset found an unacknowledged update after stopping the Agent: %s\n' "$attempt" >&2
        return 1
      fi
      if ! $waiting; then
        printf 'Waiting for panel acknowledgement of complete Agent update: %s\n' "$attempt"
        waiting=true
      fi
      sleep 1
    done
  done
}
# A newly started Agent probes the root bridge: release it before that probe,
# and re-arm the watchers against the current directories before the Agent.
start_agent_services() {
  exec 6>&-
  if [[ -d /run/systemd/system ]]; then
    systemctl restart proxy-agent-update.path
    systemctl restart proxy-agent-host-network.path proxy-agent-tuning.path
    systemctl start proxy-agent.service
  else
    rc-service proxy-agent-update restart 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-
    rc-service proxy-agent-host-network start 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-
    rc-service proxy-agent-tuning start 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-
    rc-service proxy-agent start 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-
  fi
}
phase=$(/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state status)
case $phase in
  pending|prepared) confirm_update_receipts true;;
esac
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
  # Recheck after quiescing the writer: a newly submitted task cannot disappear
  # between the acknowledgement scan and the identity seal.
  case $phase in
    pending|prepared) confirm_update_receipts false;;
  esac
  if ! /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state seal --reset-target-sha256 "$target"; then
    # Nothing has been deleted. Unless the panel committed and only its reply
    # was lost, the old identity is still valid; a later run (boot, update or
    # the qui menu) replays the same reset ID. Do not leave the node offline.
    printf 'legacy state reset could not seal the old identity; restarting the previous Agent\n' >&2
    start_agent_services
    exit 1
  fi
  /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state rebuild --reset-target-sha256 "$target"
fi
# The root CLI creates fresh private files; return ownership before the service
# account opens them. Re-arm watchers against the newly created directories.
install -d -o proxy-agent -g proxy-agent -m 0700 /var/lib/proxy-agent/host-network
install -d -o proxy-agent -g proxy-agent -m 0700 /var/lib/proxy-agent/tuning
install -d -o proxy-agent -g proxy-agent -m 0700 /var/lib/proxy-agent/system-update
chown -R proxy-agent:proxy-agent /var/lib/proxy-agent
chown proxy-agent:proxy-agent /etc/proxy-agent/config.yaml
start_agent_services
/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --reset-legacy-state complete --reset-target-sha256 "$target"
