#!/usr/bin/env bash
# Fixed root bridge. The Agent can select an enrolled identity, revision and
# formal version, never a program, URL, path, shell expression or environment.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
registry=/var/lib/proxycontrol-install
requests=/var/lib/proxy-agent/system-update
published=/var/lib/proxy-agent-update
account=proxy-agent
[[ $EUID == 0 ]] || { printf 'Agent updater bridge requires root.\n' >&2; exit 1; }
[[ -d $registry && ! -L $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || exit 1
# Pin the entire interpreter input before install.sh can replace this helper.
if [[ ${QUI_AGENT_UPDATE_PINNED:-} != 1 ]]; then
  pinned=$(mktemp "$registry/.agent-update-runner.XXXXXXXX")
  install -o root -g root -m 0700 -- "${BASH_SOURCE[0]}" "$pinned"
  exec env QUI_AGENT_UPDATE_PINNED=1 /bin/bash "$pinned" "$@"
fi
pinned=${BASH_SOURCE[0]}
trap 'rm -f -- "$pinned"' EXIT
private_root=$registry/agent-system-updates
for path in "$private_root" "$published"; do [[ ! -L $path ]] || exit 1; done
install -d -o root -g root -m 0700 "$private_root"
install -d -o root -g "$account" -m 0750 "$published"
[[ ! -L $private_root/bridge.lock ]] || exit 1
exec 8>"$private_root/bridge.lock"
flock -n 8 || exit 0
quote() {
  local text=$1 number character escaped
  text=${text//\\/\\\\}; text=${text//\"/\\\"}
  for ((number=1; number<32; number++)); do
    printf -v character '%b' "\\$(printf '%03o' "$number")"
    printf -v escaped '\\u%04x' "$number"
    text=${text//"$character"/"$escaped"}
  done
  printf '"%s"' "$text"
}
timestamp() { date -u +%Y-%m-%dT%H:%M:%S.%NZ; }
publish() {
  local input=$1 destination=$2 temporary
  temporary=$(mktemp "$published/.publish.XXXXXXXX")
  install -o root -g "$account" -m 0640 -- "$input" "$temporary"
  sync -f "$temporary"
  mv -T -- "$temporary" "$destination"
  sync -f "${destination%/*}"
}
identity() { runuser -u "$account" -- /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --system-update-identity 8>&-; }
version_info() {
  local output='' state='' magic='' readiness='' extra='' status_code=0
  bootstrap_version=''; bootstrap_error=''; installed_version=''; installed_error=''; running_version=''; running_error=''
  if output=$(/usr/local/bin/proxy-agent --version 2>&1); then
    case $output in 'proxy-agent '*) bootstrap_version=${output#proxy-agent };; *) bootstrap_error="Unrecognized bootstrap version output: $output";; esac
  else status_code=$?; bootstrap_error="Bootstrap version command exited $status_code: $output"; fi
  if [[ -z $bootstrap_version && -z $bootstrap_error ]]; then bootstrap_error='Bootstrap version command returned no version.'; fi
  local options=(--local-agent-status)
  service_state=unknown; service_error=''
  if [[ -d /run/systemd/system ]]; then
    if output=$(systemctl show proxy-agent.service -p ActiveState --value 2>&1); then
      if [[ $output == active ]]; then service_state=running
      elif [[ -n $output ]]; then service_state=$output; options+=(--local-agent-stopped)
      else service_error='systemctl returned an empty Agent ActiveState.'; fi
    else status_code=$?; service_error="systemctl Agent state exited $status_code: $output"; fi
  else
    if output=$(rc-service proxy-agent status 2>&1); then service_state=running; else status_code=$?; service_state=stopped; service_error="rc-service Agent status exited $status_code: $output"; options+=(--local-agent-stopped); fi
  fi
  if output=$(runuser -u "$account" -- /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml "${options[@]}" 2>&1 8>&-); then
    read -r magic state installed_version readiness extra <<< "$output"
    if [[ $magic != proxy-agent-local-v1 || -n $extra || -z $installed_version || $installed_version == - ]]; then
      installed_version=''; installed_error="Unrecognized actual Agent status: $output"; running_error=$installed_error
    elif [[ $state == running ]]; then running_version=$installed_version
    else running_error="Actual Agent is not running ($state)."; fi
  else status_code=$?; installed_error="Actual Agent status command exited $status_code: $output"; running_error=$installed_error; fi
}
write_progress() {
  local phase=$1 sequence=$2
  printf '{"attemptId":"%s","sequence":%s,"phase":"%s","observedAt":"%s","error":%s}\n' "$attempt_id" "$sequence" "$phase" "$(timestamp)" "$(quote "${3:-}")" > "$private/progress.json"
  publish "$private/progress.json" "$public/progress.json"
}
write_result() {
  local status=$1 phase=$2 original_error=$3 exit_code=$4 command_code=$5 recovery_attempted=$6 recovery_success=$7 recovery_error=$8
  local bootstrap_stage=$9 agent_stage=${10} bootstrap_stage_error=${11} agent_stage_error=${12} log_error=${13}
  version_info
  local log_bytes error_json recovery_json persistence_error='' bootstrap_json agent_json error_bytes
  error_json=${original_error_json:-$(quote "$original_error")}
  recovery_json=${recovery_error_json:-$(quote "$recovery_error")}
  bootstrap_json=${bootstrap_error_json:-$(quote "$bootstrap_stage_error")}
  agent_json=${agent_error_json:-$(quote "$agent_stage_error")}
  error_bytes=$(printf '%s' "$error_json$recovery_json$bootstrap_json$agent_json" | wc -c)
  if ((error_bytes > 800000)); then
    status=unconfirmed
    persistence_error='Structured errors exceed the 1 MiB result transport boundary; complete originals remain in the root event journal and task output.'
    error_json=$(quote "$persistence_error")
    recovery_json=$(quote 'Recovery detail remains in the preserved root event journal and task output.')
    bootstrap_json=$(quote 'Original stage error remains in the preserved root event journal and task output.')
    agent_json=$bootstrap_json
  fi
  log_bytes=$(stat -c '%s' "$private/output.log")
  printf '{"schemaVersion":1,"taskId":"%s","attemptId":"%s","nodeId":"%s","revision":%s,"targetVersion":"%s","status":"%s","phase":"%s","error":%s,"exitCode":%s,"commandExitCode":%s,"signal":%s,"bootstrapStage":{"status":"%s","error":%s,"exitCode":%s},"agentStage":{"status":"%s","error":%s,"exitCode":%s},"bootstrapVersion":{"version":%s,"error":%s},"installedVersion":{"version":%s,"error":%s},"runningVersion":{"version":%s,"error":%s},"serviceState":%s,"serviceError":%s,"recovery":{"attempted":%s,"success":%s,"error":%s},"startedAt":"%s","finishedAt":"%s","logBytes":%s,"logError":%s,"persistenceError":%s}\n' \
    "$task_id" "$attempt_id" "$node_id" "$revision" "$target" "$status" "$phase" "$error_json" "$exit_code" "$command_code" "$(quote "${result_signal:-}")" \
    "$bootstrap_stage" "$bootstrap_json" "${bootstrap_exit_code:-null}" "$agent_stage" "$agent_json" "${agent_exit_code:-null}" \
    "$(quote "$bootstrap_version")" "$(quote "$bootstrap_error")" "$(quote "$installed_version")" "$(quote "$installed_error")" "$(quote "$running_version")" "$(quote "$running_error")" \
    "$(quote "$service_state")" "$(quote "$service_error")" "$recovery_attempted" "$recovery_success" "$recovery_json" "$started_at" "$(timestamp)" "$log_bytes" "$(quote "$log_error")" "$(quote "$persistence_error")" \
    > "$private/result.json.partial"
  sync -f "$private/result.json.partial"
  mv -T "$private/result.json.partial" "$private/result.json"
  publish "$private/result.json" "$public/result.json"
}
[[ -f $requests/request.json && ! -L $requests/request.json && $(stat -c '%u' "$requests/request.json") == "$(id -u "$account")" ]] || exit 0
# Reading as the Agent prevents a rename/symlink race from granting root read
# access. Validation below accepts exactly the canonical Go request document.
document=$(runuser -u "$account" -- head -c 16385 -- "$requests/request.json" 8>&-)
(( ${#document} <= 16384 )) || { printf 'Agent update request exceeds 16384 bytes.\n' >&2; exit 1; }
pattern='^\{"schemaVersion":1,"operation":"(probe|upgrade)","taskId":"([A-Za-z0-9_-]{1,128})","attemptId":"([A-Za-z0-9_-]{1,128})","nodeId":"([A-Za-z0-9_-]{1,128})","revision":(0|[1-9][0-9]*),"targetVersion":"([0-9.]*)"\}$'
[[ $document =~ $pattern ]] || { printf 'Invalid Agent update request schema.\n' >&2; exit 1; }
operation=${BASH_REMATCH[1]}; task_id=${BASH_REMATCH[2]}; attempt_id=${BASH_REMATCH[3]}; node_id=${BASH_REMATCH[4]}; revision=${BASH_REMATCH[5]}; target=${BASH_REMATCH[6]}
expected_identity=$(printf '{"nodeId":"%s","revision":%s}' "$node_id" "$revision")
identity_error=''
if actual_identity=$(identity 2>&1); then
  [[ $actual_identity == "$expected_identity" ]] || identity_error="Enrolled identity/revision changed: expected $expected_identity; observed $actual_identity"
else status_code=$?; identity_error="Agent identity/revision command exited $status_code: $actual_identity"; fi
if [[ $operation == probe ]]; then
  [[ $task_id == probe && $attempt_id == probe && -z $target ]] || exit 1
  available=false; [[ -x /usr/local/bin/qui-update && ! -L /usr/local/bin/qui-update && $(stat -c '%u' /usr/local/bin/qui-update) == 0 ]] && available=true
  supported=false; reason=$identity_error
  if ! $available; then reason="Installed root updater /usr/local/bin/qui-update is unavailable."; fi
  if $available; then
    if updater_contract=$(/usr/local/bin/qui-update --system-update-contract 2>&1 8>&-); then
      [[ $updater_contract == proxy-agent-system-update-v1 ]] || { available=false; reason="Installed updater contract differs: $updater_contract"; }
    else status_code=$?; available=false; reason="Installed updater contract probe exited $status_code: $updater_contract"; fi
  fi
  watcher_ready=false
  if [[ -d /run/systemd/system ]]; then
    if systemctl is-active --quiet proxy-agent-update.path; then watcher_ready=true; fi
  elif rc-service proxy-agent-update status >/dev/null 2>&1; then watcher_ready=true; fi
  if ! $watcher_ready; then reason='Independent root update watcher is not active.'; fi
  [[ -n $reason ]] || supported=true
  bootstrap_version=''
  if bootstrap_output=$(/usr/local/bin/proxy-agent --version 2>&1); then
    case $bootstrap_output in 'proxy-agent '*) bootstrap_version=${bootstrap_output#proxy-agent };; *) supported=false; reason="Unrecognized bootstrap version: $bootstrap_output";; esac
  else status_code=$?; supported=false; reason="Bootstrap version probe exited $status_code: $bootstrap_output"; fi
  printf '{"schemaVersion":1,"nodeId":"%s","attemptId":"probe","supported":%s,"reason":%s,"bootstrapVersion":%s,"updaterAvailable":%s,"watcherReady":%s,"observedAt":"%s"}\n' \
    "$node_id" "$supported" "$(quote "$reason")" "$(quote "$bootstrap_version")" "$available" "$watcher_ready" "$(timestamp)" > "$private_root/probe.json"
  publish "$private_root/probe.json" "$published/probe.json"
  # The Agent owns request cleanup after reading this receipt. Root must not
  # race a newly renamed upgrade request with a compare-then-unlink sequence.
  exit 0
fi
[[ $task_id != probe && $attempt_id != probe && $revision != 0 && $target =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || exit 1
private=$private_root/$attempt_id; public=$published/$attempt_id
[[ ! -L $private && ! -L $public ]] || exit 1
install -d -o root -g "$account" -m 0750 "$public"
resume=false
boot_recovery=false
managed_recovery=false
if [[ -d $private ]]; then
  [[ -f $private/request.json && $(cat "$private/request.json") == "$document" ]] || { printf 'Attempt ID already names a different request.\n' >&2; exit 1; }
  if [[ -f $private/result.json ]]; then publish "$private/result.json" "$public/result.json"; exit 0; fi
  started_at=$(cat "$private/started-at")
  original_process_terminated=false
  if [[ -f $private/updater.pid && -f $private/updater.start && -f $private/boot-id ]]; then
    previous_pid=$(cat "$private/updater.pid")
    previous_start=$(cat "$private/updater.start")
    previous_boot=$(cat "$private/boot-id")
    if [[ $previous_pid =~ ^[0-9]+$ && $previous_start =~ ^[0-9]+$ && $previous_boot =~ ^[a-f0-9-]{36}$ ]]; then
    while [[ -f /proc/$previous_pid/stat && $(cat /proc/sys/kernel/random/boot_id) == "$previous_boot" && $(awk '{print $22}' "/proc/$previous_pid/stat") == "$previous_start" ]]; do
      # The original updater may have outlived its reporting shell. Wait for
      # that exact process; never run another installer over its transaction.
      sleep 1
    done
    original_process_terminated=true
    fi
  fi
  terminal_event=''
  if [[ -f $private/events.jsonl ]]; then terminal_event=$(sed -n '/^{"stage":"terminal",/p' "$private/events.jsonl" | tail -n 1); fi
  if [[ -n $terminal_event ]]; then
    updater_status=$(sed -n 's/.*"exitCode":\([0-9]*\),.*/\1/p' <<< "$terminal_event")
    [[ $updater_status =~ ^[0-9]+$ ]] || exit 1
    log_status=unknown
    if [[ -f $private/logger.exit ]]; then log_status=$(cat "$private/logger.exit"); fi
    monitor_status=0
    resume=true
  else
    managed_event=''; managed_query_error=''
    if [[ -f $private/events.jsonl ]]; then managed_event=$(sed -n '/^{"stage":"managed_request",/p' "$private/events.jsonl" | tail -n 1); fi
    managed_binding=${managed_event#*,\"error\":\"}; managed_binding=${managed_binding%\"\}}
    if $original_process_terminated && [[ $managed_binding =~ ^(local-system-[A-Za-z0-9_-]+):([a-f0-9]{64}):([A-Za-z0-9+/=]+)$ ]]; then
      managed_id=${BASH_REMATCH[1]}; managed_digest=${BASH_REMATCH[2]}; managed_manifest=${BASH_REMATCH[3]}
      printf '%s' "$managed_manifest" | base64 -d > "$private/managed-manifest.json"
      sync -f "$private/managed-manifest.json"
      install -o root -g "$account" -m 0640 "$private/managed-manifest.json" "$public/managed-manifest.json"
      if managed_output=$(runuser -u "$account" -- /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --agent-install-receipt "$managed_id" --agent-manifest "$public/managed-manifest.json" 2>&1 8>&-); then
        managed_line=$(sed -n '/^proxy-agent-install-receipt-v1 /p' <<< "$managed_output")
        read -r magic receipt_id receipt_node receipt_revision receipt_version receipt_digest receipt_status receipt_phase receipt_extra <<< "$managed_line"
        if [[ $magic == proxy-agent-install-receipt-v1 && $receipt_id == "$managed_id" && $receipt_node == "$node_id" && $receipt_revision == "$revision" && $receipt_version == "$target" && $receipt_digest == "$managed_digest" && -z $receipt_extra && $receipt_status =~ ^(success|rolled_back|failed_closed|rejected)$ ]]; then
          managed_recovery=true; resume=true; updater_status=null; log_status=unknown; monitor_status=0
          if [[ -f $private/logger.exit ]]; then log_status=$(cat "$private/logger.exit"); fi
          if [[ ! -f $private/managed-receipt-appended ]]; then
            printf '\n[original managed installation receipt]\n%s\n' "$managed_output" >> "$private/output.log"
            sync -f "$private/output.log"
            publish "$private/output.log" "$public/output.log"
            : > "$private/managed-receipt-appended"; sync -f "$private"
          fi
        else managed_query_error="Original managed receipt is pending or differs from its bound request: $managed_output"; fi
      else managed_code=$?; managed_query_error="Original managed receipt query exited $managed_code: $managed_output"; fi
    elif [[ -n $managed_event ]]; then managed_query_error='Original updater process termination or managed request binding cannot be confirmed.'; fi
    if ! $managed_recovery; then
    snapshot_event=''
    if [[ -f $private/events.jsonl ]]; then snapshot_event=$(sed -n '/^{"stage":"snapshot_prepared",/p' "$private/events.jsonl" | tail -n 1); fi
    snapshot_path=${snapshot_event#*,\"error\":\"}; snapshot_path=${snapshot_path%\"\}}
    if [[ $snapshot_path =~ ^/var/lib/proxycontrol-install/rollback\.[A-Za-z0-9]{8}$ && -f $registry/recovery-receipts/${snapshot_path##*/}.status ]]; then
      recovery_code=$(cat "$registry/recovery-receipts/${snapshot_path##*/}.status")
      [[ $recovery_code =~ ^[0-9]+$ ]] || exit 1
      boot_recovery=true; resume=true; updater_status=null; log_status=unknown; monitor_status=0
      if [[ -f $private/logger.exit ]]; then log_status=$(cat "$private/logger.exit"); fi
      if [[ ! -f $private/boot-recovery-appended ]]; then
        cat "$registry/recovery-receipts/${snapshot_path##*/}.log" >> "$private/output.log"
        publish "$private/output.log" "$public/output.log"
        : > "$private/boot-recovery-appended"
        sync -f "$private"
      fi
    else
      # No terminal receipt has been created. Keep reconciliation open, with
      # an immutable last observation rather than an invented final result.
      last_phase=''; last_sequence=0
      if [[ -f $private/progress.json ]]; then
        last_phase=$(sed -n 's/.*"phase":"\([a-z_]*\)".*/\1/p' "$private/progress.json")
        last_sequence=$(sed -n 's/.*"sequence":\([0-9]*\),.*/\1/p' "$private/progress.json")
      fi
      previous_error=''; if [[ -f $private/receipt-query.error ]]; then previous_error=$(cat "$private/receipt-query.error"); fi
      if [[ $last_phase != interrupted || $previous_error != "$managed_query_error" ]]; then
        printf '%s' "$managed_query_error" > "$private/receipt-query.error"; sync -f "$private/receipt-query.error"
        write_progress interrupted "$((last_sequence+1))" "$managed_query_error"
      fi
      exit 0
    fi
    fi
  fi
else
for prior in "$private_root"/*; do
  [[ -d $prior && ! -L $prior && -f $prior/request.json ]] || continue
  if [[ ! -f $prior/result.json ]] || grep -q '"status":"unconfirmed"' "$prior/result.json"; then
    identity_error="A previous complete Agent update has no confirmed final outcome: ${prior##*/}; inspect its original executor and recovery materials before starting another attempt."
    break
  fi
done
install -d -o root -g root -m 0700 "$private"
printf '%s\n' "$document" > "$private/request.json"
started_at=$(timestamp); printf '%s\n' "$started_at" > "$private/started-at"
: > "$private/output.log"
sync -f "$private/request.json"; sync -f "$private"
publish "$private/request.json" "$public/request.json"
publish "$private/output.log" "$public/output.log"
write_progress request_check 1
if [[ -n $identity_error ]]; then
  printf '%s\n' "$identity_error" > "$private/output.log"; publish "$private/output.log" "$public/output.log"
  write_result failed request_check "$identity_error" null null false null '' not_started not_started '' '' ''
  exit 0
fi
if [[ ! -x /usr/local/bin/qui-update || -L /usr/local/bin/qui-update || $(stat -c '%u' /usr/local/bin/qui-update) != 0 ]]; then
  write_result failed request_check 'Installed root updater /usr/local/bin/qui-update is unavailable.' null null false null '' not_started not_started '' '' ''
  exit 0
fi
: > "$private/events.jsonl"
(
  previous=0
  while [[ ! -f $private/executor-finished ]] && kill -0 $$ 2>/dev/null; do
    count=$(wc -l < "$private/events.jsonl")
    if ((count > previous)); then
      latest=$(sed -n "${count}p" "$private/events.jsonl")
      latest_phase=$(sed -n 's/^{"stage":"\([a-z_]*\)".*/\1/p' <<< "$latest")
      if [[ -n $latest_phase ]]; then
        if ! write_progress "$latest_phase" "$((count+1))"; then
          printf 'Unable to persist or publish the updater stage.\n' > "$private/progress-error"
          exit 1
        fi
      fi
      previous=$count
    fi
    sleep 0.2
  done
) 8>&- &
monitor=$!
# The updater inherits neither the bridge ownership lock nor an Agent service
# cgroup. tee preserves both streams completely in private and Agent-readable
# files; a failed log write is a distinct result, never an apparent success.
mkfifo -m 0600 "$private/output.pipe"
(
  set +e
  tee -a "$private/output.log" "$public/output.log"
  logger_status=$?
  printf '%s\n' "$logger_status" > "$private/logger.exit"
  sync -f "$private/logger.exit"
  exit "$logger_status"
) < "$private/output.pipe" 8>&- &
logger_pid=$!
QUI_UPDATE_REPORT_FD=3 /usr/local/bin/qui-update --component agent --version "$target" --yes 3>>"$private/events.jsonl" 8>&- > "$private/output.pipe" 2>&1 &
updater_pid=$!
printf '%s\n' "$updater_pid" > "$private/updater.pid"
cat /proc/sys/kernel/random/boot_id > "$private/boot-id"
if [[ -f /proc/$updater_pid/stat ]]; then awk '{print $22}' "/proc/$updater_pid/stat" > "$private/updater.start"; fi
sync -f "$private"
set +e
wait "$updater_pid"
updater_status=$?
wait "$logger_pid"
log_status=$?
: > "$private/executor-finished"
wait "$monitor"
monitor_status=$?
set -e
rm -f -- "$private/output.pipe"
fi
phase=request_check; command_code=null; recovery_attempted=false; recovery_success=null; recovery_error=''; bootstrap_stage=not_started; agent_stage=not_started
sequence=1; failed_phase=''; original_error_json=''; recovery_error_json=''
while IFS= read -r event; do
  event_phase=$(sed -n 's/^{"stage":"\([a-z_]*\)".*/\1/p' <<< "$event")
  [[ -n $event_phase ]] || { printf 'Invalid updater event: %s\n' "$event" >&2; exit 1; }
  sequence=$((sequence+1))
  [[ $event_phase == terminal ]] && continue
  phase=$event_phase
  state=$(sed -n 's/.*"state":"\([a-z_]*\)".*/\1/p' <<< "$event")
  code=$(sed -n 's/.*"exitCode":\(null\|[0-9]*\),.*/\1/p' <<< "$event")
  if [[ $state == failed && $phase != recovery ]]; then
    if [[ -z $failed_phase ]]; then failed_phase=$phase; original_error_json=${event#*,\"error\":}; original_error_json=${original_error_json%\}}; fi
    [[ $code == null ]] || command_code=$code
  fi
  if [[ $state == failed && $phase == bootstrap_* && -z ${bootstrap_error_json:-} ]]; then
    bootstrap_error_json=${event#*,\"error\":}; bootstrap_error_json=${bootstrap_error_json%\}}
    bootstrap_exit_code=$code
  elif [[ $state == failed && $phase == agent_* && -z ${agent_error_json:-} ]]; then
    agent_error_json=${event#*,\"error\":}; agent_error_json=${agent_error_json%\}}
    agent_exit_code=$code
  fi
  if [[ $phase == managed_recovery ]]; then
    if [[ $state == success ]]; then recovery_attempted=true; recovery_success=true
    elif [[ $state == failed ]]; then recovery_attempted=true; recovery_success=false
    elif [[ $state == unknown ]]; then recovery_attempted=true; recovery_success=null; fi
    recovery_error_json=${event#*,\"error\":}; recovery_error_json=${recovery_error_json%\}}
  fi
  case $phase in bootstrap_*) bootstrap_stage=$state;; agent_*) agent_stage=$state;; signal_int) result_signal=SIGINT;; signal_term) result_signal=SIGTERM;; signal_hup) result_signal=SIGHUP;; recovery) recovery_attempted=true; if [[ $state == success ]]; then recovery_success=true; elif [[ $state == failed ]]; then recovery_success=false; recovery_error_json=${event#*,\"error\":}; recovery_error_json=${recovery_error_json%\}}; fi;; esac
done < "$private/events.jsonl"
if $managed_recovery; then
  phase=interrupted
  if [[ $receipt_status == success ]]; then agent_stage=success
  elif [[ $receipt_status == rolled_back ]]; then agent_stage=failed; recovery_attempted=true; recovery_success=true
  elif [[ $receipt_status == failed_closed ]]; then agent_stage=failed; recovery_attempted=true; recovery_success=false
  else agent_stage=failed; fi
  receipt_original=$(sed -n 's/^proxy-agent-install-original-error-v1 //p' <<< "$managed_output")
  receipt_recovery=$(sed -n 's/^proxy-agent-install-recovery-v1 //p' <<< "$managed_output")
  if [[ -n $receipt_original && $receipt_original != '""' ]]; then original_error_json=$receipt_original; agent_error_json=$receipt_original; fi
  if [[ -n $receipt_recovery ]]; then
    recovery_error_json=${receipt_recovery#*,\"error\":}; recovery_error_json=${recovery_error_json%\}}
    if [[ $receipt_recovery == *'"attempted":true'* ]]; then
      recovery_attempted=true
      if [[ $receipt_recovery == *'"success":true'* ]]; then recovery_success=true
      elif [[ $receipt_recovery == *'"success":false'* ]]; then recovery_success=false
      else recovery_success=null; fi
    fi
  fi
fi
if $boot_recovery; then
  phase=interrupted
  recovery_attempted=true
  if ((recovery_code == 0)); then recovery_success=true; else
    recovery_success=false
    recovery_error=$(cat "$registry/recovery-receipts/${snapshot_path##*/}.log")
  fi
fi
status=failed; original_error=''; log_error=''
if [[ $log_status == unknown ]]; then log_error='Final logger exit receipt is absent; task output completeness is unknown.'
elif ((log_status != 0)); then log_error="tee failed to persist complete updater output (exit $log_status)."; fi
if ((monitor_status != 0)); then log_error="$log_error Stage publication failed (exit $monitor_status)."; fi
version_info
if [[ $updater_status == 0 && $bootstrap_version == "$target" && $installed_version == "$target" && $running_version == "$target" && $service_state == running && -z $log_error ]]; then
  status=success; phase=complete
else
  if $managed_recovery; then
    original_error="Original updater was interrupted without a terminal exit receipt; its bound managed request $managed_id has a durable $receipt_status result (installation phase $receipt_phase)."
  elif $boot_recovery; then
    original_error='Original updater was interrupted before its terminal receipt; the original snapshot recovery command has now produced a durable outcome.'
  else original_error="Updater exited $updater_status; bootstrap: $bootstrap_error; installed: $installed_error; running: $running_error; service: $service_error"; fi
  [[ -z $log_error ]] || original_error="$original_error"$'\n'"$log_error"
  [[ -z $failed_phase ]] || phase=$failed_phase
  if [[ $updater_status == 0 && -n $log_error ]]; then phase=result_save; fi
fi
if [[ -f $private/progress.json ]]; then
  previous_sequence=$(sed -n 's/.*"sequence":\([0-9]*\),.*/\1/p' "$private/progress.json")
  if ((previous_sequence > sequence)); then sequence=$previous_sequence; fi
fi
write_progress "$phase" "$((sequence+1))"
write_result "$status" "$phase" "$original_error" "$updater_status" "$command_code" "$recovery_attempted" "$recovery_success" "$recovery_error" "$bootstrap_stage" "$agent_stage" '' '' "$log_error"
