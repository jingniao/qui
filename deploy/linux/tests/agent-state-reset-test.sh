#!/usr/bin/env bash
# Isolated reset ordering and real flock checks; no host services or Agent data.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd -P)
root=$(mktemp -d)
trap 'rm -rf -- "$root"' EXIT
failures=0
check() { if "$@"; then echo "ok - $*"; else echo "FAIL - $*" >&2; failures=$((failures+1)); fi; }
check python3 - "$here/../../systemd/proxy-agent-state-reset.service" "$here/../../openrc/proxy-agent-state-reset" <<'PY'
import sys
unit,rc=(open(p).read() for p in sys.argv[1:])
for line in unit.splitlines():
 if line.startswith('Before='):
  assert not any(s in line.split('=',1)[1].split() for s in ['proxy-agent.service','proxy-agent-host-network.service','proxy-agent-tuning.service']), 'reset blocks its own Agent/helper start'
assert 'before proxy-agent' not in rc, 'OpenRC reset blocks its own Agent start'
assert 'command_background=yes' in rc, 'OpenRC start waits for updater lock release'
PY
sed -e "s|/var/lib/proxycontrol-install|$root/registry|g" \
    -e "s|/var/lib/proxy-agent|$root/data|g" \
    -e "s|/etc/proxy-agent|$root/etc|g" \
    -e "s|/run/systemd/system|$root/systemd|g" \
    -e 's|/usr/local/bin/proxy-agent|agent_cli|g' \
    "$here/../agent-state-reset.sh" > "$root/reset.sh"
mkdir -p "$root/registry/agent-state-reset" "$root/etc" "$root/data" "$root/systemd"
export root
agent_cli() {
  local op='' target=''
  while (($#)); do case $1 in --reset-legacy-state) op=$2; shift 2;; --reset-target-sha256) target=$2; shift 2;; *) shift;; esac; done
  if [[ $op == status ]]; then cat "$root/phase"; return; fi
  echo "$op" >> "$root/ops"
  for lock in operation update; do
    if flock -n "$root/registry/$lock.lock" true; then echo "$lock lock was released" >&2; return 1; fi
  done
  if [[ $op == complete ]]; then
    flock -n "$root/registry/agent-system-updates/bridge.lock" true || return 1
  elif flock -n "$root/registry/agent-system-updates/bridge.lock" true; then
    echo 'bridge lock was released before rebuild' >&2; return 1
  fi
  case $op in
    prepare) [[ $target == $(printf '%064d' 2) ]] || { echo 'reset target does not match executing binary' >&2; return 1; }; echo prepared > "$root/phase";;
    seal) [[ -e $root/stopped && $scenario != seal-fails ]] || return 1; if [[ $(cat "$root/phase") == prepared ]]; then echo sealed > "$root/phase"; fi;;
    rebuild) [[ -e $root/stopped ]] || return 1; echo rebuilt > "$root/phase";;
    complete) [[ -e $root/started && -e $root/ownership ]] || return 1; echo complete > "$root/phase";;
    *) return 1;;
  esac
}
systemctl() {
  echo "$*" >> "$root/ops"
  case $1 in
    stop) [[ $scenario != stop-fails ]] || return 1; stop_agent;;
    start) if [[ $2 == proxy-agent.service ]]; then start_agent; fi;;
    restart) if [[ $2 == proxy-agent-update.path ]]; then touch "$root/watcher"; fi;;
    *) return 1;;
  esac
}
rc-service() {
  echo "$*" >> "$root/ops"
  case $2 in
    stop) [[ $scenario != stop-fails ]] || return 1; stop_agent;;
    start|restart)
      if [[ $1 == proxy-agent-update ]]; then touch "$root/watcher"; fi
      start_agent
      # Real child processes reproduce OpenRC's descriptor inheritance.
      sleep 30 >/dev/null 2>&1 &
      echo "$!" >> "$root/children"
      ;;
    *) return 1;;
  esac
}
stop_agent() {
  if [[ $scenario == delayed-ack ]]; then [[ -f $root/data/system-update/task.ack.json ]] || return 1; fi
  if [[ $scenario == new-unconfirmed ]]; then
    mkdir -p "$root/data/system-update" "$root/registry/agent-system-updates/task"
    echo '{}' > "$root/data/system-update/task.request.json"
    echo '{"status":"success","phase":"complete"}' > "$root/registry/agent-system-updates/task/result.json"
  fi
  touch "$root/stopped"
}
start_agent() {
  [[ -e $root/watcher ]] || { echo 'Agent started before update watcher' >&2; return 1; }
  flock -n "$root/registry/agent-system-updates/bridge.lock" true || { echo 'bridge probe is blocked' >&2; return 1; }
  touch "$root/started"
}
chown() { echo ownership >> "$root/ops"; touch "$root/ownership"; }
install() { if [[ $1 == -d ]]; then mkdir -p "${@: -1}"; else return 1; fi; }
sha256sum() { if [[ $1 == agent_cli ]]; then printf '%064d  agent_cli\n' 2; else command sha256sum "$@"; fi; }
export -f agent_cli systemctl rc-service stop_agent start_agent chown install sha256sum
for init in systemd openrc; do
if [[ $init == systemd ]]; then mkdir -p "$root/systemd"; else rmdir "$root/systemd"; fi
for scenario in pending prepared sealed deleting deleted rebuilt seal-fails stop-fails rollback-pending delayed-ack missing-result new-unconfirmed complete; do
  export scenario
  : > "$root/ops"
  rm -f "$root/stopped" "$root/started" "$root/ownership" "$root/watcher"
  rm -rf "$root/data/system-update" "$root/registry/agent-system-updates/task"
  phase=$scenario
  case $scenario in seal-fails|stop-fails|rollback-pending|delayed-ack|missing-result|new-unconfirmed) phase=prepared;; esac
  if [[ $scenario == rollback-pending ]]; then
    mkdir -p "$root/registry/rollback.failed"
    printf 'component=agent\nphase=started\n' > "$root/registry/rollback.failed/journal"
  fi
  echo "$phase" > "$root/phase"
  if [[ $scenario == delayed-ack || $scenario == missing-result || $scenario == deleting ]]; then
    mkdir -p "$root/data/system-update"
    echo '{}' > "$root/data/system-update/task.request.json"
  fi
  if [[ $scenario == delayed-ack ]]; then
    mkdir -p "$root/registry/agent-system-updates/task"
    echo '{"status":"success","phase":"complete"}' > "$root/registry/agent-system-updates/task/result.json"
    : > "$root/output"
    (
      for ((i=0;i<200;i++)); do
        if grep -q 'Waiting for panel acknowledgement' "$root/output"; then
          digest=$(command sha256sum "$root/registry/agent-system-updates/task/result.json"); digest=${digest%% *}
          printf '{"resultSha256":"%s"}\n' "$digest" > "$root/data/system-update/task.ack.json"
          exit 0
        fi
        sleep 0.05
      done
      exit 1
    ) &
    ack_writer=$!
  fi
  set +e
  timeout 15 bash "$root/reset.sh" "$(printf '%064d' 1)" > "$root/output" 2>&1
  code=$?
  set -e
  if [[ $scenario == delayed-ack ]]; then check wait "$ack_writer"; fi
  if [[ $scenario == seal-fails || $scenario == stop-fails || $scenario == rollback-pending || $scenario == missing-result || $scenario == new-unconfirmed ]]; then
    check test "$code" -eq 1
    check bash -c '! grep -Eq "^(rebuild|complete)$" "$root/ops"'
  else
    check test "$code" -eq 0
    check test "$(cat "$root/phase")" = complete
    if [[ $scenario == rebuilt ]]; then
      check bash -c '! grep -Eq "^(seal|rebuild)$" "$root/ops"'
    fi
    if [[ $scenario == complete ]]; then check test ! -s "$root/ops"; fi
  fi
  if ((code!=0)); then cat "$root/output"; fi
  if [[ $scenario == rollback-pending ]]; then
    check test ! -s "$root/ops"
    rm "$root/registry/rollback.failed/journal"
    rmdir "$root/registry/rollback.failed"
  fi
  for lock in operation update agent-state-reset/job agent-system-updates/bridge; do
    check flock -n "$root/registry/$lock.lock" true
  done
  if [[ -f $root/children ]]; then
    while read -r pid; do kill "$pid"; done < "$root/children"
    rm "$root/children"
  fi
done
done
# Exercise the installer tail used even when the running updater predates reset.
sed -n '/^# Queue the reset after publication\./,/^# End reset scheduling\./p' "$here/../install.sh" > "$root/schedule.sh"
check test -s "$root/schedule.sh"
export QUI_UPDATE=1
component=agent
legacy_reset_request=$root/request
touch "$legacy_reset_request"
service_start() { echo "$*" >> "$root/ops"; }
for init_system in systemd openrc; do
  : > "$root/ops"
  . "$root/schedule.sh"
  check grep -q proxy-agent-state-reset "$root/ops"
done
((failures==0))
