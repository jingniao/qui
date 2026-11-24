#!/usr/bin/env bash
set -euo pipefail
repo=$(cd -- "$(dirname -- "$0")/../../.." && pwd -P)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
(cd -- "$repo" && go build -o "$temporary/qui-update-state" ./cmd/qui-update-state)
source "$repo/deploy/linux/update.sh"
trap 'rm -rf -- "$temporary"' EXIT
registry=$temporary
state_helper=$temporary/qui-update-state
QUI_UPDATE_REPORT_FD=3
exec 3> "$temporary/events"
for phase in managed_request snapshot_prepared agent_install managed_recovery bootstrap_health agent_health recovery signal_int signal_term signal_hup terminal; do
  update_stage=$phase
  report_update failed 143 $'原始错误\n"\\<>&'
done
exec 3>&-
# Use the actual web bridge's extraction, rather than a second JSON parser.
while IFS= read -r event; do
  phase=$(printf '%s' "$event" | sed -n 's/^{"stage":"\([^"]*\)","state":.*/\1/p')
  state=$(printf '%s' "$event" | sed -n 's/^{"stage":"[^"]*","state":"\([^"]*\)",.*/\1/p')
  error_json=${event#*,\"error\":}; error_json=${error_json%\}}
  [[ -n $phase && $state == failed && $error_json == '"原始错误\n\"\\<>&"' ]]
done < "$temporary/events"
cp "$temporary/events" "$temporary/events.jsonl"
private=$temporary
sed -n '/^phase=request_check; command_code=/,/^done < "\$private\/events.jsonl"/p' "$repo/deploy/linux/agent-update-request.sh" > "$temporary/parse.sh"
[[ -s $temporary/parse.sh ]]
source "$temporary/parse.sh"
[[ $result_signal == SIGHUP && $recovery_attempted == true && $recovery_success == false ]]
exec 3> "$temporary/bound"
report_event managed_request bound null 'request:digest:manifest'
report_event snapshot_prepared running null '/root/rollback.example'
exec 3>&-
[[ $(head -n 1 "$temporary/bound") == '{"stage":"managed_request","state":"bound","exitCode":null,"error":"request:digest:manifest"}' ]]
report_failed=false
update_stage=recovery
report_update failed null 'original operation failure' 2> "$temporary/diagnostic"
[[ $report_failed == true ]]
grep -q 'original operation failure' "$temporary/diagnostic"
grep -q '更新事件未写出' "$temporary/diagnostic"
if report_terminal 0 2>> "$temporary/diagnostic"; then printf 'false terminal success\n' >&2; exit 1; fi
QUI_UPDATE_REPORT_FD=''
exec 3> "$temporary/raw-event"
QUI_UPDATE_REPORT_FD=3
report_failed=false
report_structured agent_install failed 1 '"\ud800"' 'original structured failure' 2> "$temporary/raw-diagnostic"
exec 3>&-
grep -q '结构化错误无效，已按原文上报' "$temporary/raw-diagnostic"
grep -q '"error":"original structured failure"}' "$temporary/raw-event"
QUI_UPDATE_REPORT_FD=''
# Help/contract have no state-helper dependency or event side effects.
QUI_UPDATE_REPORT_FD=3 bash "$repo/deploy/linux/update.sh" --help 3> "$temporary/help-events" > /dev/null
QUI_UPDATE_REPORT_FD=3 bash "$repo/deploy/linux/update.sh" --system-update-contract 3> "$temporary/contract-events" > "$temporary/contract"
[[ ! -s $temporary/help-events && ! -s $temporary/contract-events && $(cat "$temporary/contract") == proxy-agent-system-update-v1 ]]
# Before pinning, select only the installed helper when outside a release tree.
early=$temporary/early
mkdir -p "$early/usr/local/lib/proxycontrol" "$early/var/lib/proxycontrol-install"
chmod 0700 "$early/var/lib/proxycontrol-install"
cp "$repo/deploy/linux/update.sh" "$early/updater"
install -m 0755 "$state_helper" "$early/usr/local/lib/proxycontrol/qui-update-state"
if QUI_UPDATE_TEST_ROOT=$early QUI_UPDATE_REPORT_FD=3 bash "$early/updater" --unknown 3> "$temporary/early-events" 2> "$temporary/early-diagnostic"; then exit 1; fi
grep -q '"stage":"terminal","state":"failed","exitCode":1,' "$temporary/early-events"
[[ -z $(find "$early/var/lib/proxycontrol-install" -name '.update-event.*' -print) ]]
rm "$early/usr/local/lib/proxycontrol/qui-update-state"
if QUI_UPDATE_TEST_ROOT=$early QUI_UPDATE_REPORT_FD=3 bash "$early/updater" --unknown 3> "$temporary/missing-events" 2> "$temporary/missing-diagnostic"; then exit 1; fi
[[ ! -s $temporary/missing-events ]]
grep -q '更新事件未写出' "$temporary/missing-diagnostic"
mkdir -p "$temporary/backups/nested"
backups=$temporary/backups
# Released readers accept a nested regular backup and keep database_source verbatim.
printf 'db' > "$backups/nested/pre-update 1.7.5.db"
for released in 1.10.3 1.10.4; do
  gzip -dc "$repo/internal/platform/hostupdatestate/testdata/updater-$released.sh.gz" > "$temporary/released.sh"
  sed -n '/^journal_value() {$/,/^}$/p; /^journal_read() {$/,/^}$/p' "$temporary/released.sh" > "$temporary/reader.sh"
  source "$temporary/reader.sh"
  for component in panel agent; do for phase in prepared started committed restored; do for fields in empty filled; do
    backup='' database_source=''
    if [[ $fields == filled ]]; then backup="$backups/nested/pre-update 1.7.5.db" database_source='/var/lib/proxy-panel/数据 = 1.db'; fi
    mkdir -m 0700 "$temporary/$released-$component-$phase-$fields"
    args=(journal-write --snapshot "$temporary/$released-$component-$phase-$fields" --component "$component" --previous '中文 = 1.7.5' --target 1.10.4 --was-active true --backup "$backup" --database-source "$database_source" --phase "$phase")
    if [[ $component == agent ]]; then args+=(--core-request request --core-helper v1); fi
    "$state_helper" "${args[@]}"
    file=$temporary/$released-$component-$phase-$fields/journal
    journal_read "$file"
    [[ $journal_component == "$component" && $journal_phase == "$phase" && $journal_previous == '中文 = 1.7.5' && $journal_target == 1.10.4 && $journal_was_active == true && $journal_backup == "$backup" && $journal_database_source == "$database_source" ]]
    if [[ $component == agent ]]; then [[ $(journal_value "$file" core_request) == request && $(journal_value "$file" core_helper) == v1 ]]; fi
  done; done; done
done
# take_snapshot removes a half-built snapshot when any private tool cannot be
# copied, so the next run never reports it as a legacy leftover.
root=$temporary/snapshot-root
program_directory=$root/usr/local/bin
registry=$temporary/snapshot-registry
directory=$temporary/snapshot-work
init_system=systemd
mkdir -p "$program_directory" "$directory"
mkdir -m 0700 "$registry"
printf 'updater\n' > "$directory/updater"
printf 'library\n' > "$directory/service.sh"
printf 'program\n' > "$program_directory/proxy-agent"
printf 'program\n' > "$program_directory/proxy-panel"
for broken in state database core; do
  state_helper=$temporary/qui-update-state database_helper=$temporary/qui-update-state core_helper='' snapshot_component=panel
  case $broken in
    state) state_helper=$temporary/missing-tool snapshot_component=agent;;
    database) database_helper=$temporary/missing-tool;;
    core) core_helper=$temporary/missing-tool snapshot_component=agent;;
  esac
  snapshot=''
  if take_snapshot "$snapshot_component" 2> "$temporary/snapshot-diagnostic"; then
    printf 'snapshot with missing %s tool succeeded\n' "$broken" >&2
    exit 1
  fi
  [[ -z $snapshot && -z $(find "$registry" -mindepth 1 -maxdepth 1 -name 'rollback.*' -print) ]]
done
state_helper=$temporary/qui-update-state database_helper=$temporary/qui-update-state core_helper=''
take_snapshot panel
[[ -n $snapshot && -f $snapshot/state-helper && -f $snapshot/database-helper && -f $snapshot/updater ]]
printf 'PASS state event layout, bridge extraction and closed descriptor\n'
