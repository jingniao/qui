#!/usr/bin/env bash
# Update the panel and/or the node agent in place, from a GitHub release or from a
# local release archive, installed as /usr/local/bin/qui-update by
# deploy/linux/install.sh.
#
# Trust in the archive rests on the GitHub repository: SHA256SUMS is published in the
# same release as the archives, so its digest only proves that the download matches
# what the repository published. --bundle and --sums do the same check with two files
# from the local disk, for a host that has no route to the release. The script
# replaces only the program, the service definitions (systemd units or OpenRC init
# scripts), the packaged scripts and the install record, and it does so by running the
# deploy/linux/install.sh of the archive. Agent core settings are normalized
# and snapshotted for rollback; node identity and other configuration are retained.
#
# An update is reversible and survives being interrupted. While the service is stopped
# and before install.sh runs, the files install.sh may replace are copied into a fresh
# root-only 0700 directory under /var/lib/proxycontrol-install/ (cp -a, so owner, group
# and mode are kept), a panel update also copies the database into
# /var/lib/proxy-panel/backups/, because database migrations are not reversible, and the
# snapshot then receives a journal that records how far the update got. The journal is
# what makes Ctrl-C, a hung-up SSH session, kill -9 and a power failure recoverable: a
# signal runs the same recovery inline, and deploy/linux/update-recovery.sh does it at
# boot, ordered before the services start. After the update the new version has to stay
# up: both init systems restart a crashed program after five seconds
# (Restart=on-failure RestartSec=5, supervise-daemon respawn_delay=5), so a crash loop
# looks active at every poll but changes its main PID, and that change is what the
# ten-second health check catches. When install.sh fails, when the installed version
# is not the requested one, or when the new version does not stay up, the service is
# stopped, the snapshot is put back and the previous version is started again; a panel
# rollback also restores the pre-update database copy when the new panel had already
# run. The snapshot directory is removed after a successful update and after a
# successful rollback, and kept (with its path printed) when the rollback itself does
# not come up healthy.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

repository=jingniao/qui

# Root's web bridge alone supplies this inherited descriptor. Reports describe
# execution facts; the ordinary terminal uses the very same branches below.
update_stage=request_check
update_error=''
state_helper='' state_helper_temporary='' event_temporary='' report_failed=false
state_helper_source() {
  local prefix=${root:-${QUI_UPDATE_TEST_ROOT:-}} script_directory=''
  script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P) || return 1
  if [[ -f $script_directory/install.sh ]]; then
    printf '%s\n' "$script_directory/../../bin/qui-update-state"
  else
    printf '%s\n' "$prefix/usr/local/lib/proxycontrol/qui-update-state"
  fi
}
validate_state_helper() {
  local helper=$1 protocol=''
  [[ -f $helper && ! -L $helper && $(stat -c '%u:%g' -- "$helper") == 0:0 ]] || return 1
  case $(stat -c '%a' -- "$helper") in 500|755) ;; *) return 1;; esac
  protocol=$("$helper" --protocol 9>&-) || return 1
  [[ $protocol == qui-update-state-v1 ]]
}
state_cli() { "$state_helper" "$@" 9>&-; }
prepare_state_helper() {
  local source=${1:-} previous=$state_helper_temporary candidate=''
  [[ -n $source ]] || source=$(state_helper_source) || return 1
  validate_state_helper "$source" || { printf '缺少可用的状态工具：%s\n' "$source" >&2; return 1; }
  candidate=$(mktemp "$registry/.state-helper.XXXXXXXX") || return 1
  if ! install -o root -g root -m 0500 -- "$source" "$candidate" || ! validate_state_helper "$candidate"; then
    rm -f -- "$candidate" || return 1
    return 1
  fi
  state_helper_temporary=$candidate
  state_helper=$candidate
  [[ -z $previous ]] || rm -f -- "$previous" || return 1
}
prepare_recovery_state_helper() {
  local source=$1/state-helper expected='' actual=''
  [[ -f $source && ! -L $source && $(stat -c '%u:%g:%a' -- "$source") == 0:0:500 ]] || { printf '快照缺少可用状态工具：%s\n' "$1" >&2; return 1; }
  [[ -f $1/state-helper.sha256 && ! -L $1/state-helper.sha256 ]] || return 1
  expected=$(cat -- "$1/state-helper.sha256") || return 1
  actual=$(sha256sum -- "$source") || return 1
  [[ $expected =~ ^[0-9a-f]{64}$ && $expected == "${actual%% *}" ]] || { printf '快照状态工具摘要不匹配：%s\n' "$1" >&2; return 1; }
  prepare_state_helper "$source" || return 1
  actual=$(sha256sum -- "$state_helper") || return 1
  [[ $expected == "${actual%% *}" ]] || { printf '状态工具活动副本摘要不匹配：%s\n' "$1" >&2; return 1; }
}
report_event() {
  [[ -n ${QUI_UPDATE_REPORT_FD:-} ]] || return 0
  local helper=$state_helper event_registry=${registry:-${QUI_UPDATE_TEST_ROOT:-}/var/lib/proxycontrol-install} status=0
  [[ $QUI_UPDATE_REPORT_FD =~ ^[3-8]$ ]] || { printf '更新事件未写出：Invalid update report descriptor.\n' >&2; return 1; }
  if [[ -z $helper ]]; then
    helper=$(state_helper_source) || return 1
    validate_state_helper "$helper" || { printf '更新事件未写出：状态工具缺失或协议无效：%s\n' "$helper" >&2; return 1; }
  fi
  if [[ -z $event_temporary ]]; then
    event_temporary=$(mktemp "$event_registry/.update-event.XXXXXXXX") || { printf '更新事件未写出：无法创建私有事件文件。\n' >&2; return 1; }
  fi
  printf '%s' "$4" | "$helper" event --stage "$1" --state "$2" --exit-code "$3" "${@:5}" 9>&- > "$event_temporary" || status=$?
  if ((status != 0)); then
    printf '更新事件未写出：状态工具编码失败（%s）。\n' "$status" >&2
    if [[ ${5:-} == --error-json && $status == 1 ]]; then return 2; fi
    return 1
  fi
  cat -- "$event_temporary" >&"$QUI_UPDATE_REPORT_FD" || { printf '更新事件未写出：事件转发失败。\n' >&2; return 1; }
}
report_update() {
  if ! report_event "$update_stage" "$1" "${2:-null}" "${3:-}"; then
    printf '原始操作错误：%s\n' "${3:-$update_error}" >&2
    report_failed=true
  fi
  # Reporting must not interrupt required core-stop or database/file recovery.
  # EXIT converts this accumulated failure into a nonzero result.
  return 0
}
report_structured() {
  local status=0
  report_event "$1" "$2" "$3" "$4" --error-json || status=$?
  if ((status == 2)); then
    printf '结构化错误无效，已按原文上报。\n' >&2
    report_event "$1" "$2" "$3" "$5" || report_failed=true
  elif ((status != 0)); then
    printf '原始操作错误：%s\n' "$5" >&2
    report_failed=true
  fi
}
report_stage() { update_stage=$1; report_update running; }
sync_report_fd() {
  if [[ -n ${QUI_UPDATE_REPORT_FD:-} ]]; then
    sync -f "/proc/self/fd/$QUI_UPDATE_REPORT_FD" || { printf '更新事件未写出：报告 fd 同步失败。\n' >&2; report_failed=true; }
  fi
  return 0
}
report_terminal() {
  local status=$1
  if $report_failed && ((status == 0)); then status=1; fi
  update_stage=terminal
  if ((status == 0)); then report_update success "$status"; else report_update failed "$status"; fi
  sync_report_fd
  ! $report_failed
}
fail() { update_error=$*; report_update failed null "$*"; printf '更新被拒绝：%s\n' "$*" >&2; exit 1; }
if [[ $# == 1 && $1 == --system-update-contract ]]; then
  printf 'proxy-agent-system-update-v1\n'
  exit 0
fi
cleanup_state_files() {
  local file status=0
  for file in "$event_temporary" "${database_helper_temporary:-}" "$state_helper_temporary"; do
    [[ -z $file ]] || rm -f -- "$file" || status=1
  done
  return "$status"
}
early_cleanup() {
  local status=$?
  report_terminal "$status" || { if ((status == 0)); then status=1; fi; }
  cleanup_state_files || { if ((status == 0)); then status=1; fi; }
  exit "$status"
}
trap early_cleanup EXIT
usage() {
  cat <<USAGE
用法：qui-update [--version X.Y.Z[-rc.N]] [--component panel|agent] [--yes] [--database FILE]
                 [--bundle FILE --sums FILE [--agent-manifest FILE]]

  --version X.Y.Z[-rc.N] 安装指定发布版本（默认：GitHub 上的最新发布）；
                    Agent 只能升级，低于已安装 Agent 的版本会被拒绝
  --component panel|agent  只更新指定的已安装组件（默认保持先 Agent 后面板）
  --yes             不询问确认，直接更新
  --database FILE   降级到更旧的面板版本时，同时把数据库换成该备份
                    （只用于已安装面板且 --version 低于当前版本；FILE 是
                     /var/lib/proxy-panel/backups/ 里的 pre-update-*.db，
                     它之后写入的数据会丢失）
  --bundle FILE     离线更新：本地发布归档（proxy-control-X.Y.Z-linux-ARCH.tar.gz）
  --sums FILE       离线更新：与该归档配套的 SHA256SUMS
                    （两个参数必须同时给出，并同时用 --version 指定版本；此时不
                    访问 GitHub，摘要校验与下载时完全相同）
  --agent-manifest FILE  受管 Agent 离线更新所需的配套官方签名清单
                    （仅与 --bundle/--sums 一起使用；普通节点不需要）
  --recover DIR     内部使用：恢复一个被中断的更新（[--boot] 由开机单元调用）

脚本从 https://github.com/$repository/releases 下载归档与 SHA256SUMS，校验摘要
后调用归档里的 deploy/linux/install.sh 原地替换程序、服务定义（systemd 单元或
OpenRC 初始化脚本）、随包脚本与安装记录。Agent 核心配置会规范化并纳入回滚；身份与其余配置保留。面板更新前
会在服务停止时把数据库备份到 /var/lib/proxy-panel/backups/。更新前会把可能被替换
的文件快照到 /var/lib/proxycontrol-install/rollback.*，并写入进度日志；新版本启动
后十秒内不重启才算成功。失败、Ctrl-C、SSH 断线、kill -9 或断电后都会恢复到更新前
的版本（面板还恢复到更新前的数据库备份），开机时由
proxycontrol-update-recovery.service 完成同样的恢复。同一台主机同时装有面板与节点
时，默认两个组件都从同一个归档更新（先节点，后面板）；--component 只更新指定组件。
USAGE
}

# Test-only prefix for every absolute path this script touches, so
# deploy/linux/tests/update-test.sh can exercise the recovery paths without touching a
# real installation. It is empty in production and only root can set the environment of
# a root shell, so it is trusted the same way $PATH is.
root=${QUI_UPDATE_TEST_ROOT:-}
# install -o/-g owner of the database and the backup directory. Only the test root
# overrides it; a real host always uses the panel service account.
panel_account=proxy-panel
if [[ -n $root ]]; then panel_account=${QUI_UPDATE_TEST_ACCOUNT:-proxy-panel}; fi

# Paths that do not come from the options, so they are set once here and read by every
# function below.
registry=$root/var/lib/proxycontrol-install
backups=$root/var/lib/proxy-panel/backups
panel_config=$root/etc/proxy-panel/config.yaml
program_directory=$root/usr/local/bin
# Filled by main() before the update loop runs: the version being installed, the
# extracted archive and the temporary download directory the EXIT trap removes.
version=''
directory=''
source_dir=''
# Filled by take_snapshot and read by restore_snapshot and the success paths; empty
# means no snapshot was taken, which is what keeps the EXIT trap out of it.
snapshot=''
# Private backup hardlinks used by preflight_backup; retained for signal cleanup.
check_directory=''
database_helper=''
database_helper_temporary=''
core_handover=false
core_request=''
core_target=''
core_helper=''
core_source_supported=false
core_target_supported=false
core_preflight_done=false
# The component the run is working on. The signal handlers and the journal read these,
# because a signal can arrive between any two statements of update_component.
active_component=''
active_previous=''
active_target=''
active_was_active=false
active_backup=''
active_database_source=''
# True between a successful stop and the next successful start, so a signal before the
# snapshot exists still puts the service back.
service_stopped=false
# --database FILE: a database copy to install together with a downgrade.
database_file=''
staged_database=''
# True when --version is older than the installed panel, which changes the messages
# after a failure.
downgrade=false
# Backup files written through a .partial name that the signal handler has to remove.
partial_files=()
# The bootstrap transaction and managed installation have separate commit points.
# Local candidates survive in the Agent's data directory; these public release
# input copies are only for the unprivileged verifier, and can always be removed.
managed_agent=false
agent_runtime_version=''
agent_runtime_state=''
agent_runtime_ready=''
agent_inputs=''
managed_installing=false
managed_core_journal=''
managed_core_request=''
managed_target_digest=''
agent_guard_pid=''
agent_guard_read=''
agent_guard_write=''

# Read both the legacy fixed-core v1 and the multi-core v2 install records.
record_complete() {
  local component=$1 record
  record=$registry/$component.manifest
  [[ -f $record && ! -L $record ]] || return 1
  qui_record_cores "$record" "$component"
}

# Print the raw value of the first top-level "key: value" line of a file, with a
# trailing carriage return removed; the same simple parsing qui.sh uses. Returns
# 1 when the key is absent.
read_value() {
  local file=$1 key=$2 line
  while IFS= read -r line || [[ -n $line ]]; do
    if [[ $line =~ ^[[:space:]]*$key[[:space:]]*:[[:space:]]*(.*)$ ]]; then
      printf '%s\n' "${BASH_REMATCH[1]%$'\r'}"
      return 0
    fi
  done < "$file"
  return 1
}

# Print the version the installed program reports; "未知" when it cannot be read.
installed_version() {
  local component=$1 output
  output=$("$program_directory/proxy-$component" --version 2>/dev/null || true)
  case $output in
    "proxy-$component "*) printf '%s' "${output#proxy-$component }";;
    *) printf '未知';;
  esac
}

agent_cli() {
  runuser -u proxy-agent -- "$program_directory/proxy-agent" \
    --config "$root/etc/proxy-agent/config.yaml" "$@" 3>&- 4>&- 5>&- 6>&- 7>&- 8>&-
}

prepare_core_handover() {
  local candidate=$1 status=0 selected=''
  core_preflight_done=true
  core_handover=false core_request='' core_target='' core_helper=''
  core_source_supported=false core_target_supported=false
  qui_core_handover_protocol "$program_directory/proxy-agent" || status=$?
  case $status in 0) core_source_supported=true;; 2) ;; *) return 1;; esac
  status=0
  qui_core_handover_protocol "$candidate" || status=$?
  case $status in 0) core_target_supported=true; core_helper=$candidate;; 2) ;; *) return 1;; esac
  if $core_source_supported; then
    [[ -n $core_helper ]] || core_helper=$program_directory/proxy-agent
    selected=$(qui_core_cli "$program_directory/proxy-agent" "$root/etc/proxy-agent/config.yaml" --core-handover-target) || return 1
    [[ $selected =~ ^[0-9a-f]{64}$ ]] || { printf '实际 Agent 摘要无效。\n' >&2; return 1; }
    status=0
    if $active_was_active; then
      qui_core_handover_source "$program_directory/proxy-agent" "$selected" || status=$?
      case $status in 0) ;; 2) core_source_supported=false;; *) return 1;; esac
    fi
  fi
  if $managed_agent; then
    core_target=$selected
  else
    core_target=$(sha256sum -- "$candidate") || return 1
    core_target=${core_target%% *}
  fi
  if $core_source_supported && $core_target_supported; then core_handover=true; fi
  if ! $core_handover; then
    printf '[proxy-agent] 源或目标不支持交接协议；本次更新会中断连接。\n'
  fi
}

# Only the verified, root-owned snapshot copy is used after stop or replacement.
snapshot_core_cli() {
  local helper=$snapshot/core-stop-helper digest='' expected=''
  [[ -f $helper && ! -L $helper && $(stat -c '%u:%a' -- "$helper") == 0:500 ]] || return 1
  [[ -f $snapshot/core-stop-helper.sha256 && ! -L $snapshot/core-stop-helper.sha256 \
    && $(stat -c '%u:%a' -- "$snapshot/core-stop-helper.sha256") == 0:600 ]] || return 1
  expected=$(cat -- "$snapshot/core-stop-helper.sha256") || return 1
  digest=$(sha256sum -- "$helper") || return 1
  [[ $expected =~ ^[0-9a-f]{64}$ && ${digest%% *} == "$expected" ]] || return 1
  qui_core_cli "$helper" "$root/etc/proxy-agent/config.yaml" "$@"
}

component_healthy() {
  if [[ $active_component == agent && -n $core_request ]]; then
    local selected='' waited=0
    qui_core_handover_wait "$snapshot/core-stop-helper" "$root/etc/proxy-agent/config.yaml" "$core_request" || return 1
    service_healthy proxy-agent 10 || return 1
    # A managed worker records adoption before its Ready frame; the supervisor
    # opens its local socket only after that, so the target query may need to
    # wait for it, as install_managed_release waits for --local-agent-status.
    until selected=$(qui_core_cli "$program_directory/proxy-agent" "$root/etc/proxy-agent/config.yaml" --core-handover-target 2>&1); do
      if ((waited >= 30)) || ! service_is_active proxy-agent; then
        printf '%s\n' "$selected" >&2
        return 1
      fi
      sleep 1
      waited=$((waited + 1))
    done
    [[ $selected == "$core_target" ]] || { printf '认领后的实际 Agent 镜像与授权目标不同：%s\n' "$selected" >&2; return 1; }
  else
    service_healthy "proxy-$active_component" 10
  fi
}

agent_read_status() {
  local output='' magic='' extra='' options=(--local-agent-status)
  if ! service_is_active proxy-agent; then options+=(--local-agent-stopped); fi
  if ! output=$(agent_cli "${options[@]}"); then
    printf '无法确认实际 Agent 版本；旧安装请先用已安装的 qui-update 更新引导程序，再重跑同版本命令。\n' >&2
    return 1
  fi
  read -r magic agent_runtime_state agent_runtime_version agent_runtime_ready extra <<< "$output"
  [[ $magic == proxy-agent-local-v1 && -z $extra ]] || {
    printf 'Agent 本地状态格式不匹配：%s\n' "$output" >&2
    return 1
  }
  case $agent_runtime_state in
    unmanaged) managed_agent=false;;
    running|installed) managed_agent=true;;
    *) printf 'Agent 本地状态未知：%s\n' "$output" >&2; return 1;;
  esac
}

prepare_managed_release() {
  local manifest=$1 binary=$2
  managed_target_digest=$(sha256sum -- "$binary") || fail '无法读取已校验的 worker 摘要。'
  managed_target_digest=${managed_target_digest%% *}
  agent_inputs=$(mktemp -d "${TMPDIR:-/var/tmp}/qui-agent.XXXXXXXX") \
    || fail '无法准备 Agent 校验输入目录。'
  chown proxy-agent:proxy-agent -- "$agent_inputs" || fail '无法设置 Agent 校验目录属主。'
  install -o proxy-agent -g proxy-agent -m 0600 -- "$manifest" "$agent_inputs/manifest.json" \
    || fail '无法准备 Agent 签名清单。'
  install -o proxy-agent -g proxy-agent -m 0600 -- "$binary" "$agent_inputs/proxy-agent" \
    || fail '无法准备 Agent 候选程序。'
  # The installed, already trusted bootstrap verifies the candidate. Only its
  # supervisor may then probe the verified immutable image, before any stop.
  agent_cli --check-agent-release --agent-manifest "$agent_inputs/manifest.json" \
    --agent-binary "$agent_inputs/proxy-agent" --agent-version "$version" \
    || fail '受管 Agent 签名、兼容性或升级状态检查失败；服务未停止。'
}

reserve_agent_bootstrap() {
  local output=''
  # The supervisor holds the existing jobs locks while this coprocess keeps its
  # pipe open. It cannot poll a queued web update across the bootstrap stop.
  # A killed shell closes the non-inherited coproc pipe and releases the lease.
  coproc QUI_AGENT_GUARD {
    agent_cli --reserve-agent-bootstrap --agent-manifest "$agent_inputs/manifest.json" \
      --agent-binary "$agent_inputs/proxy-agent" --agent-version "$version"
  }
  agent_guard_pid=${QUI_AGENT_GUARD_PID:-}
  agent_guard_read=${QUI_AGENT_GUARD[0]:-}
  agent_guard_write=${QUI_AGENT_GUARD[1]:-}
  [[ -n $agent_guard_pid && -n $agent_guard_read && -n $agent_guard_write ]] \
    || fail '无法启动引导程序更新的排他保留。'
  if ! read -r output <&"$agent_guard_read"; then
    fail '未取得引导程序更新保留，服务未停止。'
  fi
  [[ $output == 'proxy-agent-bootstrap-v1 reserved' ]] \
    || fail "引导程序更新保留格式不匹配：$output"
}

release_agent_bootstrap() {
  local status=0
  if [[ -n $agent_guard_write ]]; then
    exec {agent_guard_write}>&-
    agent_guard_write=''
  fi
  if [[ -n $agent_guard_read ]]; then
    exec {agent_guard_read}<&-
    agent_guard_read=''
  fi
  if [[ -n $agent_guard_pid ]]; then
    wait "$agent_guard_pid" || status=$?
    agent_guard_pid=''
  fi
  if ((status != 0)); then
    printf '引导程序更新保留异常退出（%s），请检查服务与任务状态。\n' "$status" >&2
    return "$status"
  fi
}

managed_core_cli() {
  # Reuse the snapshot verifier after the bootstrap snapshot has been disposed.
  local snapshot=$managed_core_journal
  snapshot_core_cli "$@"
}

managed_core_rollback() {
  [[ -n $managed_core_journal ]] || return 0
  if [[ -n $managed_core_request ]]; then
    managed_core_cli --core-handover-revoke --request "$managed_core_request" --reason rollback || return 1
  else
    managed_core_cli --core-handover-revoke-all --reason abort || return 1
  fi
  # The non-root installer may have lost its candidate. Root must confirm both
  # worker and core exit before any retained image is allowed to restart.
  service_stop proxy-agent || return 1
  managed_core_cli --finish-core-stop || return 1
  rm -rf -- "$managed_core_journal" || return 1
  managed_core_journal='' managed_core_request=''
}

install_managed_release() {
  local output='' waited=0
  report_stage agent_install
  # The bootstrap commit must precede this call. Signal handling must not restore
  # its old shell snapshot over the managed installer's newer durable journal.
  [[ -z $active_component && -z $snapshot ]] || fail '引导程序事务尚未提交，不能开始实际 Agent 安装。'
  managed_installing=true
  printf '[proxy-agent] 更新实际 Agent 到 %s ...\n' "$version"
  # A stable supervisor PID does not by itself prove its worker/socket is ready.
  # Wait for that explicit readiness after the bootstrap service restart.
  until output=$(agent_cli --local-agent-status 2>&1); do
    if ((waited >= 30)) || ! service_is_active proxy-agent; then
      printf '%s\n' "$output" >&2
      fail "引导程序已是 $(installed_version agent)，实际 Agent 入口未就绪；请检查服务后重跑命令。"
    fi
    sleep 1
    waited=$((waited + 1))
  done
  local install_status=0
  local local_request="local-system-$(cat /proc/sys/kernel/random/uuid)" local_digest local_manifest
  local install_options=(--agent-install-request-id "$local_request")
  local_digest=$(sha256sum -- "$agent_inputs/proxy-agent") || fail '无法读取 worker 候选摘要。'
  local_digest=${local_digest%% *}
  [[ $local_digest == "$managed_target_digest" ]] || fail 'worker 候选已偏离已校验镜像，拒绝安装。'
  if ! $core_preflight_done; then
    local active_was_active=true
    prepare_core_handover "$source_dir/bin/proxy-agent" || fail 'worker 核心交接预检失败。'
  fi
  if [[ -n $core_helper ]]; then
    # A persistent helper also covers SIGKILL/boot through the existing
    # web-handover recovery scan. Never retain the disposable candidate path.
    local helper_digest=''
    helper_digest=$(sha256sum -- "$program_directory/proxy-agent") || fail '无法校验已安装停核 helper。'
    [[ ${helper_digest%% *} == "$managed_target_digest" ]] || fail '已安装 helper 与已校验镜像不同。'
    managed_core_journal=$(mktemp -d "$registry/web-handover.worker.XXXXXXXX") || fail '无法创建 worker 交接日志。'
    chmod 0700 -- "$managed_core_journal" || fail '无法保护 worker 交接日志。'
    install -o root -g root -m 0500 -- "$program_directory/proxy-agent" "$managed_core_journal/core-stop-helper" || fail '无法保留 worker 停核 helper。'
    printf '%s\n' "$managed_target_digest" > "$managed_core_journal/core-stop-helper.sha256" || fail '无法记录 helper 摘要。'
    chmod 0600 -- "$managed_core_journal/core-stop-helper.sha256" || fail '无法保护 helper 摘要。'
    install -o root -g root -m 0600 -- "$managed_core_journal/core-stop-helper.sha256" "$managed_core_journal/sha256" || fail '无法记录恢复摘要。'
    printf 'transaction=%s\ntarget=%s\n' "$local_request" "$local_digest" > "$managed_core_journal/journal" || fail '无法绑定 worker 安装事务。'
    chmod 0600 -- "$managed_core_journal/journal" || fail '无法保护 worker 安装事务。'
    sync -f -- "$managed_core_journal" || fail '无法同步 worker 交接日志。'
  fi
  local worker_switch=true
  if $core_handover; then
    local current_digest=''
    current_digest=$(managed_core_cli --core-handover-target) || fail '无法确认切换前的实际 worker 摘要。'
    if [[ $current_digest == "$local_digest" ]]; then worker_switch=false; fi
  fi
  if $core_handover && $worker_switch; then
    local handover_arm_status=0
    managed_core_request=$(qui_core_handover_arm managed_core_cli --core-handover-arm --scope worker \
      --transaction "$local_request" --target-sha256 "$local_digest" --updater-pid "$$") || handover_arm_status=$?
    case $handover_arm_status in
      0)
        [[ $managed_core_request =~ ^[0-9a-f]{64}$ ]] || fail 'worker 交接 request id 无效。'
        printf 'request=%s\n' "$managed_core_request" >> "$managed_core_journal/journal" || fail '无法记录 worker 交接 request。'
        sync -f -- "$managed_core_journal/journal" || fail '无法同步 worker 交接 request。'
        ;;
      2) core_handover=false; managed_core_request='';;
      *) fail '无法授权 worker 交接。';;
    esac
  elif $worker_switch; then
    printf '[proxy-agent] 本次 worker 切换会中断连接。\n'
  fi
  if [[ -n ${QUI_UPDATE_REPORT_FD:-} ]]; then
    # Bind the original managed request before Queue or Apply. The root event
    # journal outlives the bootstrap snapshot and the disposable input directory.
    local_manifest=$(base64 -w 0 "$agent_inputs/manifest.json")
    report_event managed_request bound null "$local_request:$local_digest:$local_manifest" || report_failed=true
    sync_report_fd
  fi
  if output=$(agent_cli --install-agent-release --agent-manifest "$agent_inputs/manifest.json" \
    --agent-binary "$agent_inputs/proxy-agent" --agent-version "$version" "${install_options[@]}" 2>&1); then
    printf '%s\n' "$output"
  else
    install_status=$?
    printf '%s\n' "$output" >&2
    local original_json='' recovery_json='' recovery_error_json='' recovery_state=not_attempted
    original_json=$(sed -n 's/^proxy-agent-install-original-error-v1 //p' <<< "$output")
    recovery_json=$(sed -n 's/^proxy-agent-install-recovery-v1 //p' <<< "$output")
    if [[ -n $original_json && $original_json != '""' && -n ${QUI_UPDATE_REPORT_FD:-} ]]; then
      report_structured agent_install failed "$install_status" "$original_json" "$output"
    else report_update failed "$install_status" "$output"; fi
    if [[ -n $recovery_json && -n ${QUI_UPDATE_REPORT_FD:-} ]]; then
      recovery_error_json=${recovery_json#*,\"error\":}; recovery_error_json=${recovery_error_json%\}}
      if [[ $recovery_json == *'"attempted":true'* ]]; then
        recovery_state=unknown
        if [[ $recovery_json == *'"success":true'* ]]; then recovery_state=success
        elif [[ $recovery_json == *'"success":false'* ]]; then recovery_state=failed; fi
      fi
      report_structured managed_recovery "$recovery_state" null "$recovery_error_json" "$output"
    fi
    printf '[proxy-agent] 引导程序版本：%s；实际 Agent 更新失败，以上错误和受管安装记录已保留。\n' "$(installed_version agent)" >&2
    managed_core_rollback || fail "worker 回滚停核屏障失败，保留现场：$managed_core_journal"
    if agent_read_status; then
      printf '[proxy-agent] 当前实际 Agent：%s（%s）。\n' "$agent_runtime_version" "$agent_runtime_state" >&2
    fi
    return 1
  fi
  if [[ -n $managed_core_request ]]; then
    local handover_state=''
    handover_state=$(managed_core_cli --core-handover-status --request "$managed_core_request") || fail '无法读取 worker 认领回执。'
    [[ $handover_state == adopted ]] || fail "worker 未完成核心认领：$handover_state"
  fi
  report_stage agent_health
  service_healthy proxy-agent 10 || fail '实际 Agent 切换后服务未保持稳定，请检查受管安装记录和服务日志。'
  agent_read_status || fail '无法核实升级后的实际 Agent 版本。'
  [[ $agent_runtime_state == running && $agent_runtime_ready == ready && $agent_runtime_version == "$version" ]] \
    || fail "实际 Agent 版本核验失败：$agent_runtime_version（$agent_runtime_state/$agent_runtime_ready）。"
  if [[ -n $managed_core_request ]]; then
    managed_core_cli --core-handover-revoke --request "$managed_core_request" --reason done || fail '无法撤销已完成的 worker 交接授权。'
    managed_core_request=''
  fi
  if [[ -n $managed_core_journal ]]; then
    rm -rf -- "$managed_core_journal" || fail '无法清理已提交的 worker 交接日志。'
    managed_core_journal=''
  fi
  managed_installing=false
  report_update success
  printf '[proxy-agent] 引导程序 %s；实际 Agent %s，服务运行正常。\n' "$(installed_version agent)" "$agent_runtime_version"
}

# Print the digest SHA256SUMS lists for exactly this file name; returns 1 when
# there is no such line.
sums_digest() {
  local name=$1 line digest rest
  while IFS= read -r line; do
    digest=${line%%[[:space:]]*}
    [[ $digest =~ ^[0-9a-fA-F]{64}$ ]] || continue
    rest=${line#"$digest"}
    rest=${rest#"${rest%%[![:space:]]*}"}
    [[ $rest == "$name" ]] || continue
    printf '%s' "${digest,,}"
    return 0
  done < "$directory/SHA256SUMS"
  return 1
}


# One executable is pinned before installation replaces shared tools. Recovery
# uses its own durable copy, independently of the program files being restored.
database_cli() {
  local helper=${database_helper:-$root/usr/local/lib/proxycontrol/qui-update-db}
  "$helper" "$@"
}
validate_database_helper() {
  local helper=$1 protocol=''
  [[ -f $helper && ! -L $helper && $(stat -c '%u' -- "$helper") == 0 ]] || return 1
  [[ $(stat -c '%a' -- "$helper") == 500 || $(stat -c '%a' -- "$helper") == 755 ]] || return 1
  protocol=$("$helper" --protocol) || return 1
  [[ $protocol == qui-update-database-v1 ]]
}
prepare_database_helper() {
  local source=$root/usr/local/lib/proxycontrol/qui-update-db packaged=''
  packaged=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)/bin/qui-update-db
  if [[ -f $(dirname -- "${BASH_SOURCE[0]}")/install.sh ]]; then source=$packaged; fi
  validate_database_helper "$source" || fail "缺少可用的数据库工具：$source"
  database_helper_temporary=$(mktemp "$registry/.database-helper.XXXXXXXX") || return 1
  install -o root -g root -m 0500 -- "$source" "$database_helper_temporary" || return 1
  database_helper=$database_helper_temporary
  validate_database_helper "$database_helper" || return 1
}

# --no-insecure overrides a -k that a curl wrapper or alias may add in front of
# the arguments, so nothing is fetched without a verified certificate.
download() {
  local status=0 output=''
  if output=$(curl --no-insecure -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 5 --output "$2" -- "$1" 2>&1); then return 0; else status=$?; fi
  printf '%s\n' "$output" >&2
  report_update failed "$status" "$output"
  fail "下载失败：$1（curl 退出码 $status）"
}

# Resolve the configured value only at the update boundary. Backup/recovery use the
# root-owned record, never the panel-writable configuration.
configured_database_path() {
  local database=''
  [[ -f $panel_config && ! -L $panel_config ]] || fail "缺少配置文件：$panel_config"
  database=$(read_value "$panel_config" database) || fail "配置里找不到 database：$panel_config"
  if [[ ${#database} -ge 2 ]]; then
    case $database in \"*\"|\'*\') database=${database:1:-1};; esac
  fi
  validate_database_path "$database" "${1:-source}"
}

validate_database_path() {
  local database=$1 mode=${2:-source} resolved='' data_directory=''
  if [[ $mode == target && ! -e $database && ! -L $database ]]; then
    [[ $database == /* && ${database##*/} != . && ${database##*/} != .. ]] || fail "数据库必须是绝对文件路径：$database"
    resolved=$(realpath -e -- "${database%/*}") || fail "无法解析数据库父目录：$database"
    data_directory=$(realpath -e -- "$root/var/lib/proxy-panel") || fail '缺少面板数据目录。'
    [[ $resolved == "$data_directory" || $resolved == "$data_directory/"* ]] || fail "数据库必须位于 /var/lib/proxy-panel/ 下：$database"
    [[ $(stat -c '%u' -- "$resolved") == "$(id -u "$panel_account")" ]] || fail "数据库父目录不属于面板账号：$database"
    printf '%s/%s\n' "$resolved" "${database##*/}"
    return 0
  fi
  [[ $database == /* && -f $database && ! -L $database ]] || fail "数据库必须是绝对路径的普通文件：$database"
  resolved=$(realpath -e -- "$database") || fail "无法解析数据库路径：$database"
  data_directory=$(realpath -e -- "$root/var/lib/proxy-panel") || fail '缺少面板数据目录。'
  [[ $resolved == "$data_directory/"* ]] || fail "数据库必须位于 /var/lib/proxy-panel/ 下：$database"
  [[ $(stat -c '%u' -- "$resolved") == "$(id -u "$panel_account")" ]] || fail "数据库不属于面板账号：$database"
  printf '%s\n' "$resolved"
}

database_path() {
  local record=$registry/panel.database database=''
  [[ -f $record && ! -L $record && $(stat -c '%u:%a:%h' "$record") == 0:600:1 ]] || fail "数据库路径记录必须为 root 0600 普通文件：$record"
  database=$(cat -- "$record") || return 1
  validate_database_path "$database" "${1:-source}"
}

ensure_database_record() {
  local configured='' recorded='' temporary=''
  if [[ -e $registry/panel.database || -L $registry/panel.database ]]; then
    configured=$(configured_database_path target) || return 1
    recorded=$(database_path target) || return 1
    [[ $configured == "$recorded" ]] || fail '配置里的 database 与安装记录不一致；请 Owner 在 shell 中确认数据库路径并修正配置或安装记录。'
  else
    configured=$(configured_database_path) || return 1
    temporary=$(mktemp "$registry/.panel-database.XXXXXXXX") || return 1
    if ! printf '%s\n' "$configured" > "$temporary" || ! chmod 0600 "$temporary" \
      || ! sync -f -- "$temporary" || ! mv -T -- "$temporary" "$registry/panel.database"; then
      rm -f -- "$temporary"
      return 1
    fi
    sync -f -- "$registry" || return 1
  fi
}

# Only the explicitly authorized CLI backup is read from root's private staging.

# Config-derived source reads run as the panel account. Stage and compare in root's private
# registry, then rename into a pinned panel-owned directory on the same filesystem.
copy_database_atomic() {
  local source=$1 destination=$2 mode=${3:-backup}
  local -a options=()
  [[ $mode != restore ]] || options+=(--restore)
  [[ -z $staged_database || $source != "$staged_database" ]] || options+=(--owner-staged)
  database_cli copy --registry "$registry" --account "$panel_account" --source "$source" --destination "$destination" "${options[@]}"
}

# Copy the panel database while the panel is stopped and print the copy's path.
# The path comes from the private installation record; the copy keeps the service
# account as owner and 0600, so only root and the panel can read it.
backup_database() {
  local previous=$1 database='' stamp='' destination=''
  database=$(database_path) || return 1
  [[ -f $database && ! -L $database ]] || fail "找不到数据库文件：$database"
  if [[ ! -d $backups ]]; then
    runuser -u "$panel_account" -- mkdir -m 0700 -- "$backups" \
      || fail "无法创建备份目录：$backups"
  fi
  stamp=$(date +%Y%m%d-%H%M%S)
  destination=$backups/pre-update-$previous-$stamp.db
  copy_database_atomic "$database" "$destination" || fail "数据库备份失败：$destination"
  printf '%s\n' "$destination"
}

# Keep the database as it is right now under a labelled name and print its path. Used
# before a rollback overwrites the database, so the pages the new panel wrote - which
# are the only copy of anything it migrated - are not simply thrown away. A host whose
# database file is gone has nothing to keep.
preserve_database() {
  local label=$1 database='' stamp='' destination=''
  database=$(database_path target) || return 1
  [[ -f $database && ! -L $database ]] || return 0
  if [[ ! -d $backups ]]; then
    runuser -u "$panel_account" -- mkdir -m 0700 -- "$backups" || return 1
  fi
  stamp=$(date +%Y%m%d-%H%M%S)
  destination=$backups/$label-$stamp.db
  copy_database_atomic "$database" "$destination" || return 1
  printf '%s\n' "$destination"
}

# Succeed when the database on disk is not the copy BACKUP names, either because the
# pages differ or because of the log files SQLite would replay onto them. This is what
# tells a recovery that a panel did run although its journal never got past "prepared".
database_differs() {
  local backup=$1 database='' result=0
  database=$(database_path target) || return 2
  [[ -f $database && ! -L $database ]] || return 0
  compare_database_source "$database" "$backup" || result=$?
  case $result in 1) return 0;; 0) ;; *) return 2;; esac
  if [[ -f $backup-wal && ! -L $backup-wal ]]; then
    if [[ ! -f $database-wal || -L $database-wal ]]; then return 0; fi
    result=0
    compare_database_source "$database-wal" "$backup-wal" || result=$?
    case $result in 1) return 0;; 0) ;; *) return 2;; esac
    return 1
  fi
  if [[ -f $database-wal && ! -L $database-wal && -s $database-wal ]]; then return 0; fi
  return 1
}

# cmp: 0 equal, 1 different, 2 error. Source SIGPIPE is allowed only when cmp reports different.
compare_database_source() {
  database_cli compare --account "$panel_account" --source "$1" --backup "$2"
}

# Put the database copy back after a failed panel update. The new panel may already
# have migrated the database, and migrations are not reversible, so the copy replaces
# it: the log files of the stopped new version are removed first, otherwise SQLite
# would replay the new version's pages onto the old file. Both files are replaced
# through a temporary file in the private root registry and one rename, so an interrupted
# restore never leaves a half-written database behind.
restore_database() {
  local backup=$1 database=''
  [[ -f $backup && ! -L $backup ]] || { printf '没有可恢复的数据库备份：%s\n' "$backup" >&2; return 1; }
  database=$(database_path target) || return 1
  if ! copy_database_atomic "$backup" "$database" restore; then
    printf '无法恢复数据库：%s\n' "$database" >&2
    return 1
  fi
  # MESSAGE (optional second argument) names what was put in place: the pre-update copy
  # after a rollback, or the --database file of a downgrade.
  printf '%s：%s\n' "${2:-已恢复更新前的数据库}" "$backup"
}

# Explain how to go back to an older panel that cannot open the database it finds:
# only a database copy taken before that version ran can be installed with it, and
# everything written after that copy is lost. Printed instead of telling the operator
# to overwrite files by hand, because qui-update can now do it in one step.
downgrade_guidance() {
  local target=$1 found=false file selection=''
  [[ ${selected_component:-} != panel ]] || selection=' --component panel'
  printf '目标版本 %s 无法打开当前数据库：数据库里有它不认识的迁移。\n' "$target"
  printf '要与该版本一起回退数据库，请用 --database 指定下面某个备份（新的在前）：\n'
  if [[ -d $backups ]]; then
    while IFS= read -r file; do
      [[ -f $file && ! -L $file ]] || continue
      printf '  %s（%s）\n' "$file" "$(stat -c '%y' -- "$file" 2>/dev/null || printf '修改时间未知')"
      found=true
    done < <(ls -1t -- "$backups"/pre-update-"$target"-*.db 2>/dev/null || true)
  fi
  if ! $found; then printf '  没有找到 pre-update-%s-*.db 备份。\n' "$target"; fi
  printf '命令：qui-update%s --version %s --database <上面的备份文件> --yes\n' "$selection" "$target"
  printf '注意：该备份之后写入的数据（新用户、之后登记的节点、配置改动）会丢失；\n'
  printf '      之后登记的节点需要重新登记。\n'
}

# Print the log hint, the downgrade command and the database copy after a failure.
report_failure() {
  local name=$1 previous=$2 backup=$3 selection=''
  [[ -z ${selected_component:-} ]] || selection=" --component $selected_component"
  printf '日志：%s\n' "$(service_log_hint "$name" || true)"
  # The Agent cannot be moved back to an older release, so only the panel gets the
  # downgrade hint.
  if [[ $name == proxy-agent ]]; then
    :
  elif qui_valid_release_version "$previous"; then
    printf '降级：用 qui-update%s --version %s 装回旧版本。\n' "$selection" "$previous"
  else
    printf '降级：用 qui-update%s --version X.Y.Z 装回旧版本。\n' "$selection"
  fi
  if [[ -n $backup ]]; then
    printf '数据库迁移不可逆：回到旧版本时用 qui-update%s --version <旧版本> --database %s 一起回退数据库。\n' "$selection" "$backup"
  fi
}

# The files the archive's install.sh replaces for a component: the program, the main
# service definition, the updater and the init-system library that every host gets,
# the terminal menu and the web-UI update bridge on a panel, the enforced-source and
# tuning pairs on an agent, and the install record. Defined once because the snapshot and
# its restore have to agree: a file added to install.sh's publish calls belongs here too,
# or a rollback leaves it at the new version.
component_files() {
  local component=$1
  printf '%s\n' "$program_directory/proxy-$component"
  if [[ $init_system == systemd ]]; then
    printf '%s\n' "$root/etc/systemd/system/proxy-$component.service"
  else
    printf '%s\n' "$root/etc/init.d/proxy-$component"
  fi
  printf '%s\n' "$root/usr/local/lib/proxycontrol/qui-update-db"
  printf '%s\n' "$root/usr/local/lib/proxycontrol/qui-update-state"
  printf '%s\n' "$program_directory/qui-update" "$root/usr/local/lib/proxycontrol/service.sh"
  printf '%s\n' "$program_directory/qui" "$root/usr/local/lib/proxycontrol/uninstall.sh"
  if [[ $component == panel ]]; then
    # The web-UI update bridge is part of a panel: install.sh publishes the helper and
    # the two units beside the terminal menu. A snapshot taken before they existed does
    # not contain them, and the restore then removes the ones the failed install created.
    printf '%s\n' "$root/usr/local/lib/proxycontrol/panel-update-request" \
      "$root/etc/systemd/system/proxy-panel-update.service" \
      "$root/etc/systemd/system/proxy-panel-update.path"
  elif [[ $init_system == systemd ]]; then
    printf '%s\n' "$root/etc/systemd/system/proxy-agent-host-network.service" \
      "$root/etc/systemd/system/proxy-agent-host-network.path" \
      "$root/etc/systemd/system/proxy-agent-tuning.service" \
      "$root/etc/systemd/system/proxy-agent-tuning.path" \
      "$root/etc/systemd/system/proxy-agent-tuning-probe.service" \
      "$root/etc/systemd/system/proxy-agent-tuning-probe-reset.service" \
      "$root/etc/systemd/system/proxy-agent-tuning-modules.service" \
      "$root/etc/systemd/system/proxy-agent-tuning-modules.path" \
      "$registry/agent.tuning-probe-digests"
  else
    printf '%s\n' "$root/etc/init.d/proxy-agent-host-network" \
      "$root/usr/local/lib/proxy-agent/host-network-watch" \
      "$root/etc/init.d/proxy-agent-tuning" \
      "$root/usr/local/lib/proxy-agent/tuning-watch"
  fi
  if [[ $component == agent ]]; then
    printf '%s\n' "$root/usr/local/lib/proxycontrol/agent-update-request"
    if [[ $init_system == systemd ]]; then
      printf '%s\n' "$root/etc/systemd/system/proxy-agent-update.service" "$root/etc/systemd/system/proxy-agent-update.path"
    else
      printf '%s\n' "$root/etc/init.d/proxy-agent-update" "$root/usr/local/lib/proxy-agent/agent-update-watch"
    fi
    printf '%s\n' "$root/usr/local/lib/proxy-agent/xray" "$root/usr/local/lib/proxy-agent/sing-box" "$root/usr/local/lib/proxy-agent/gost" "$root/etc/proxy-agent/config.yaml"
  fi
  printf '%s\n' "$registry/$component.manifest"
}

# Copy everything component_files lists into a fresh root-only 0700 directory inside
# the 0700 registry, keeping owner, group and mode with cp -a. A file that does not
# exist yet is simply absent from the snapshot, and restore_snapshot then removes the
# one a failed install created. The updater and the init-system library that are running
# this update are copied in as well - from the temporary $directory, because on a host
# with both components the agent update has already replaced the installed
# /usr/local/bin/qui-update by the time the panel snapshot is taken - so a recovery can
# run the code that wrote the journal. They go in last, the directory is synced, and only
# then does the caller write the journal: a directory that holds a journal is complete by
# construction. A snapshot that cannot be completed is removed again and reported as a
# failure: an update must not start without one.
take_snapshot() {
  local component=$1 path target
  snapshot=$(mktemp -d "$registry/rollback.XXXXXXXX") || { snapshot=''; return 1; }
  chmod 0700 -- "$snapshot" || { rm -rf -- "$snapshot"; snapshot=''; return 1; }
  while IFS= read -r path; do
    target=$snapshot/files$path
    [[ -e $path || -L $path ]] || continue
    mkdir -p -- "${target%/*}" || { rm -rf -- "$snapshot"; snapshot=''; return 1; }
    cp -a -- "$path" "$target" || { rm -rf -- "$snapshot"; snapshot=''; return 1; }
  done < <(component_files "$component")
  # A half-built snapshot without a journal would later read as a legacy leftover.
  if ! snapshot_helpers "$component"; then
    rm -rf -- "$snapshot"
    snapshot=''
    return 1
  fi
  if [[ ! -f $directory/updater || ! -f $directory/service.sh ]] \
    || ! install -o root -g root -m 0600 -- "$directory/updater" "$snapshot/updater" \
    || ! install -o root -g root -m 0600 -- "$directory/service.sh" "$snapshot/service.sh" \
    || ! chmod 0700 -- "$snapshot" \
    || ! sync -f -- "$snapshot"; then
    rm -rf -- "$snapshot"
    snapshot=''
    return 1
  fi
}
# Copy the private tools a recovery verifies into $snapshot. Every step is
# checked; take_snapshot removes the whole directory when one fails.
snapshot_helpers() {
  local component=$1 state_digest=''
  install -o root -g root -m 0500 -- "$state_helper" "$snapshot/state-helper" || return 1
  state_digest=$(sha256sum -- "$snapshot/state-helper") || return 1
  printf '%s\n' "${state_digest%% *}" > "$snapshot/state-helper.sha256" || return 1
  sync -f -- "$snapshot/state-helper" || return 1
  sync -f -- "$snapshot/state-helper.sha256" || return 1
  if [[ $component == panel ]]; then
    install -o root -g root -m 0500 -- "$database_helper" "$snapshot/database-helper" || return 1
    local database_digest=''
    database_digest=$(sha256sum -- "$snapshot/database-helper") || return 1
    printf '%s\n' "${database_digest%% *}" > "$snapshot/database-helper.sha256" || return 1
  fi
  if [[ $component == agent && -n $core_helper ]]; then
    install -o root -g root -m 0500 -- "$core_helper" "$snapshot/core-stop-helper" || return 1
    # Store the digest alone, independent of the snapshot's absolute path.
    local helper_digest='' copied_digest=''
    helper_digest=$(sha256sum -- "$core_helper") || return 1
    copied_digest=$(sha256sum -- "$snapshot/core-stop-helper") || return 1
    [[ ${helper_digest%% *} == "${copied_digest%% *}" ]] || return 1
    printf '%s\n' "${helper_digest%% *}" > "$snapshot/core-stop-helper.sha256" || return 1
    sync -f -- "$snapshot/core-stop-helper" || return 1
    sync -f -- "$snapshot/core-stop-helper.sha256" || return 1
  fi
}
# The state tool owns journal bytes and persistence. Bash only selects the phase
# and passes the active transaction's fields; journal text is never executed.
journal_write() {
  local phase=$1
  local args=(journal-write --snapshot "$snapshot" --component "$active_component"
    --previous "$active_previous" --target "$active_target" --was-active "$active_was_active"
    --backup "$active_backup" --database-source "$active_database_source" --phase "$phase")
  if [[ $active_component == agent ]]; then args+=(--core-request "$core_request" --core-helper "${core_helper:+v1}"); fi
  state_cli "${args[@]}"
}

# Read exactly nine NUL-delimited fields after checking the tool's exit status.
# The Go reader retains historical optional fields and nested backup paths.
journal_read() {
  local temporary='' values=() status=0
  temporary=$(mktemp "$registry/.update-event.XXXXXXXX") || return 1
  state_cli journal-read --file "$1" --backups "$backups" > "$temporary" || status=$?
  if ((status == 0)); then mapfile -d '' -t values < "$temporary" || status=$?; fi
  rm -f -- "$temporary" || return 1
  ((status == 0 && ${#values[@]} == 9)) || return 1
  journal_component=${values[0]} journal_previous=${values[1]} journal_target=${values[2]}
  journal_was_active=${values[3]} journal_backup=${values[4]} journal_database_source=${values[5]}
  journal_phase=${values[6]} journal_core_request=${values[7]} journal_core_helper=${values[8]}
}

# Remove a finished snapshot in one step that an interrupted rm -rf cannot confuse: the
# directory is renamed inside the registry and the registry is synced first, so the
# journal stops answering to a rollback.* name before anything is deleted, and a boot
# that happens in the middle of the deletion finds a discard.* directory that the next
# run removes instead of a half-removed rollback that looks like a pending update.
dispose_snapshot() {
  [[ -n $snapshot ]] || return 0
  state_cli discard --registry "$registry" --snapshot "$snapshot" || return 1
  snapshot=''
}

snapshot_cleanup_warning() {
  local message=$1
  printf '%s\n' "$message" >&2
  if [[ -n ${QUI_UPDATE_REPORT_FD:-} ]]; then report_update running null "$message"; fi
}

dispose_before_failure() {
  if ! dispose_snapshot; then snapshot_cleanup_warning "快照丢弃失败，恢复材料保留在 $snapshot 或 $registry/discard.${snapshot##*/rollback.}"; fi
}

dispose_after_commit() {
  if ! dispose_snapshot; then
    if ! $journal_finalized; then
      printf '快照停在未完成阶段，下次运行会回滚本次更新，请先人工处理：%s\n' "$snapshot" >&2
      return 1
    fi
    snapshot_cleanup_warning "更新成功但快照清理失败：$snapshot 或 $registry/discard.${snapshot##*/rollback.}"
  fi
}

# Delete the .partial copies a backup or a preserve left behind. backup_database runs
# inside a command substitution, so the array that call fills is not visible here; the
# glob is what a signal handler can rely on, and the update lock makes sure no other run
# is writing one at the same time.
remove_database_partials() {
  local file=''
  for file in "${partial_files[@]}"; do rm -f -- "$file"; done
  partial_files=()
  for file in "$registry"/database.*.partial; do
    [[ -e $file || -L $file ]] || continue
    rm -f -- "$file"
  done
}


# Remove the staging directories of an install.sh that was killed before its own cleanup
# could run (kill -9, power failure): they only hold copies of release files, 20-40 MB
# each. install.sh holds operation.lock for as long as it runs, so they are removed only
# while that lock can be taken here; otherwise an install is running and they are its own.
remove_install_staging() {
  local lock=$registry/operation.lock staging=''
  [[ -f $lock && ! -L $lock ]] || return 0
  exec 8<>"$lock" || return 0
  if flock -n 8; then
    for staging in "$registry"/staging.*; do
      [[ -d $staging && ! -L $staging ]] || continue
      rm -rf -- "$staging"
    done
  fi
  exec 8>&-
  return 0
}

# Put the recorded files back. Each copy goes to a temporary file in the destination's
# own directory and is renamed into place, the same publish pattern install.sh uses, so
# a file is never half-written and no temporary is left behind; a path the snapshot
# does not hold is removed, because install.sh created it and the previous version did
# not have it. The init-system definitions are read again afterwards, and on OpenRC the
# agent's watcher is restarted so the previous watcher runs instead of the one the
# failed install started.
restore_snapshot() {
  local component=$1 mode=$2 path source temporary probe_unit
  local job_options=()
  if [[ $mode == boot ]]; then job_options=(--no-block); fi
  [[ -n $snapshot && -d $snapshot ]] || { printf '没有可恢复的快照。\n' >&2; return 1; }
  # Do not leave a new root watcher running when rolling back to a release
  # without tuning. Stop before replacing its binary or unit definitions.
  if [[ $component == agent ]]; then
    if [[ $init_system == systemd && -f $root/etc/systemd/system/proxy-agent-tuning.service ]]; then
      # During boot the recovery unit precedes these jobs. Leave retained
      # units queued, and cancel only jobs for definitions that disappear.
      for probe_unit in proxy-agent-tuning-modules.path proxy-agent-tuning-probe.service proxy-agent-tuning-probe-reset.service; do
        path=$root/etc/systemd/system/$probe_unit
        if [[ -f $path && ( $mode != boot || ! -f $snapshot/files$path ) ]]; then
          systemctl stop "${job_options[@]}" "$probe_unit" || return 1
        fi
      done
      if [[ $mode != boot || ! -f $snapshot/files$root/etc/systemd/system/proxy-agent-tuning.service ]]; then
        systemctl stop "${job_options[@]}" proxy-agent-tuning.path proxy-agent-tuning.service || return 1
      fi
      if [[ ! -f $snapshot/files$root/etc/systemd/system/proxy-agent-tuning.service ]]; then
        systemctl disable proxy-agent-tuning.path proxy-agent-tuning.service || return 1
      fi
    elif [[ $init_system == openrc && -f $root/etc/init.d/proxy-agent-tuning ]]; then
      if [[ $mode != boot ]] && service_is_active proxy-agent-tuning; then service_stop proxy-agent-tuning || return 1; fi
      if [[ ! -f $snapshot/files$root/etc/init.d/proxy-agent-tuning ]]; then service_disable proxy-agent-tuning || return 1; fi
    fi
    # Even a restored legacy helper must retire the candidate's same-boot receipt.
    qui_invalidate_tuning_probe "$root/var/lib/proxy-agent/tuning" || return 1
  fi
  # An update started from the panel's web UI runs inside proxy-panel-update.service, so
  # the restore must not stop the unit it is running in: only the path unit is disabled,
  # and only when the snapshot shows the previous version had no update bridge at all,
  # before its unit files are removed below. A disable that fails (a unit file the failed
  # install left broken, or systemd refusing the call) is reported and the restore goes
  # on: leaving the old files in place because the watcher could not be disabled would
  # keep the panel on the version that just failed.
  if [[ $component == panel && $init_system == systemd && -f $root/etc/systemd/system/proxy-panel-update.path ]] \
    && [[ ! -f $snapshot/files$root/etc/systemd/system/proxy-panel-update.path ]]; then
    systemctl disable --now proxy-panel-update.path \
      || printf '[%s] 无法停用 proxy-panel-update.path，仍继续恢复更新前的文件。\n' "$component"
  fi
  if [[ $component == agent && $init_system == systemd && -f $root/etc/systemd/system/proxy-agent-update.path && ! -f $snapshot/files$root/etc/systemd/system/proxy-agent-update.path ]]; then
    systemctl disable --now proxy-agent-update.path || return 1
  elif [[ $component == agent && $init_system == openrc && -f $root/etc/init.d/proxy-agent-update && ! -f $snapshot/files$root/etc/init.d/proxy-agent-update ]]; then
    # Do not terminate a current root runner with the service that owns it.
    service_disable proxy-agent-update || return 1
    if [[ -z ${QUI_UPDATE_REPORT_FD:-} && $mode != boot ]] && service_is_active proxy-agent-update; then service_stop proxy-agent-update || return 1; fi
  fi
  while IFS= read -r path; do
    source=$snapshot/files$path
    if [[ ! -e $source && ! -L $source ]]; then
      rm -f -- "$path"
      continue
    fi
    # A file install.sh did not get to replace is left alone: rewriting it gains
    # nothing, and the file that made install.sh fail (immutable, or on a read-only
    # mount) would make the restore fail too. Owner and mode count as content here,
    # because install.sh sets both.
    if [[ -f $path && ! -L $path ]] && cmp -s -- "$source" "$path" \
      && [[ $(stat -c '%u:%g:%a' -- "$source") == "$(stat -c '%u:%g:%a' -- "$path")" ]]; then
      continue
    fi
    temporary=${path%/*}/.${path##*/}.rollback.$$
    if ! cp -a -- "$source" "$temporary" \
      || ! sync -f -- "$temporary" \
      || ! mv -T -- "$temporary" "$path"; then
      rm -f -- "$temporary"
      printf '无法恢复 %s\n' "$path" >&2
      return 1
    fi
    sync -f -- "${path%/*}"
  done < <(component_files "$component")
  service_reload_definitions || return 1
  if [[ $init_system == openrc && $component == agent && $mode != boot ]] && service_is_active proxy-agent-host-network; then
    service_restart proxy-agent-host-network || return 1
  fi
  if [[ $component == agent ]]; then
    if [[ $init_system == systemd && -f $root/etc/systemd/system/proxy-agent-update.path ]]; then
      systemctl start "${job_options[@]}" proxy-agent-update.path || return 1
    elif [[ $init_system == openrc && $mode != boot && -f $root/etc/init.d/proxy-agent-update ]] && ! service_is_active proxy-agent-update; then
      service_start proxy-agent-update || return 1
    fi
    if [[ $init_system == systemd && -f $root/etc/systemd/system/proxy-agent-tuning.path ]]; then
      if [[ -f $root/etc/systemd/system/proxy-agent-tuning-probe.service ]]; then
        systemctl start "${job_options[@]}" proxy-agent-tuning-probe.service || printf '恢复后的调优权限探测失败。\n' >&2
      fi
      systemctl start "${job_options[@]}" proxy-agent-tuning.path || return 1
    elif [[ $init_system == openrc && $mode != boot && -f $root/etc/init.d/proxy-agent-tuning ]]; then
      qui_skip_tuning_replay "$root/run/proxy-agent-tuning" || return 1
      service_start proxy-agent-tuning || return 1
    fi
  fi
  printf '[proxy-%s] 已恢复更新前的文件。\n' "$component"
}

# Bring a snapshot back to a consistent state, whatever part of the update it recorded
# and however often this runs. It is the one recovery routine behind every entry point,
# so a failed update, a signal, a boot after a power failure and the next run after a
# kill -9 all take the same path:
#   committed  - the update had already succeeded; only remove the snapshot.
#   restored   - a rollback had already put the files and the database back; only start
#                the service if the journal says it ran and it is not running, then
#                remove the snapshot.
#   prepared   - install.sh may have run but the new program never started: put the
#   started      files back, and for a panel also the database when the journal says the
#                new panel could have touched it (started) or when it did run anyway
#                (prepared and the database differs from the backup). The database the
#                rollback overwrites is kept under failed-update-<target> first.
# It is idempotent: restore_snapshot skips files that are already identical and the
# database is only re-done while the journal is not yet "restored", so running it again
# after an interruption at any point converges. MODE is interactive, signal or boot; at
# boot the service is neither stopped (a stop job would cancel the start systemd queued
# for it) nor started (the boot unit is ordered before the services, so they start after
# it), and the journal is left at "restored" for a boot that finds it there.
forward_install_output() {
  local status=$1
  if [[ -f $snapshot/install-output.log ]]; then
    if [[ ! -f $snapshot/install-output-forwarded ]]; then
      if ((status == 0)); then cat -- "$snapshot/install-output.log"
      else cat -- "$snapshot/install-output.log" >&2; fi || return 1
      : > "$snapshot/install-output-forwarded" || return 1
    fi
    sync -f -- "$snapshot/install-output-forwarded"
  fi
}
finish_recovered_snapshot() {
  if (($1 != 0)); then
    printf '恢复步骤已执行，但安装输出转发失败；快照保留在 %s。\n' "$snapshot" >&2
    return 1
  fi
  if ! dispose_snapshot; then
    if ! $journal_finalized; then
      printf '恢复进度未持久化且快照清理失败，下次运行会重新恢复（started 面板会先保留当前库再覆盖数据库）：%s\n' "$snapshot" >&2
      return 1
    fi
    snapshot_cleanup_warning "恢复已完成但快照清理失败：$snapshot 或 $registry/discard.${snapshot##*/rollback.}"
  fi
}
recover_snapshot() {
  local directory_arg=$1 mode=$2 name='' component='' previous='' target='' was_active=false
  local backup='' phase='' preserved='' restore=false output_status=${3:-0} journal_finalized=true
  local journal_core_request='' journal_core_helper=''
  local journal_component='' journal_previous='' journal_target='' journal_phase=''
  local journal_was_active='' journal_backup='' journal_database_source=''
  if [[ ! -d $directory_arg || -L $directory_arg ]]; then
    printf '不是更新快照目录：%s\n' "$directory_arg" >&2
    return 1
  fi
  prepare_recovery_state_helper "$directory_arg" || return 1
  if ! journal_read "$directory_arg/journal"; then
    printf '快照 %s 的进度日志无效，未做任何修改。\n' "$directory_arg" >&2
    return 1
  fi
  component=$journal_component previous=$journal_previous target=$journal_target
  phase=$journal_phase was_active=$journal_was_active backup=$journal_backup
  # The journal writer reads the same globals, so a recovered snapshot is written back
  # with the values it was read with.
  snapshot=$directory_arg
  active_component=$component
  active_previous=$previous
  active_target=$target
  active_was_active=$was_active
  active_backup=$backup
  active_database_source=$journal_database_source
  core_request=''
  if [[ $component == agent ]]; then
    core_helper=''
    core_request=$journal_core_request
    if [[ -f $snapshot/core-stop-helper || -L $snapshot/core-stop-helper ]]; then
      core_helper=$snapshot/core-stop-helper
      # Recovery never trusts an old detached receipt to authorize preservation.
      if [[ $mode != rollback || -z $core_request || $phase == committed ]]; then
        snapshot_core_cli --core-handover-revoke-all --reason recovery || return 1
      else
        snapshot_core_cli --core-handover-revoke --request "$core_request" --reason rollback || return 1
      fi
    elif [[ $journal_core_helper == v1 ]]; then
      printf '缺少新停核 helper，保留快照并停止恢复：%s\n' "$snapshot" >&2
      return 1
    fi
  fi
  if [[ $component == panel && ( $phase == prepared || $phase == started ) ]]; then
    local helper_expected='' helper_actual=''
    database_helper=$snapshot/database-helper
    validate_database_helper "$database_helper" || { printf '快照缺少可用数据库工具：%s\n' "$snapshot" >&2; return 1; }
    [[ -f $snapshot/database-helper.sha256 && ! -L $snapshot/database-helper.sha256 ]] || return 1
    helper_expected=$(cat -- "$snapshot/database-helper.sha256") || return 1
    helper_actual=$(sha256sum -- "$database_helper") || return 1
    [[ $helper_expected =~ ^[0-9a-f]{64}$ && $helper_expected == "${helper_actual%% *}" ]] || { printf '快照数据库工具摘要不匹配。\n' >&2; return 1; }
  fi
  name=proxy-$component
  # A killed updater never forwarded its direct install child's captured output.
  # Replay it before recovery can dispose of the only durable copy.
  if ! forward_install_output 1; then
    output_status=1
    printf '[%s] 安装输出转发或确认保存失败；继续恢复文件、数据库和服务，快照将保留在 %s。\n' "$name" "$snapshot" >&2
  fi
  case $phase in
    committed)
      finish_recovered_snapshot "$output_status" || return 1
      printf '[%s] 更新到 %s 的步骤此前已经完成，无需恢复。\n' "$name" "$target"
      return 0
      ;;
    restored)
      if [[ $mode != boot && $was_active == true ]] && ! service_is_active "$name"; then
        if ! service_start "$name" || ! service_healthy "$name" 10; then
          printf '[%s] 已恢复到 %s，但服务没有恢复健康。\n' "$name" "$previous"
          printf '[%s] 服务状态：%s\n' "$name" "$(service_state "$name" || true)"
          printf '[%s] 日志：%s\n' "$name" "$(service_log_hint "$name" || true)"
          printf '[%s] 快照保留在 %s。\n' "$name" "$directory_arg"
          return 1
        fi
        service_stopped=false
      fi
      finish_recovered_snapshot "$output_status" || return 1
      printf '[%s] 已恢复到 %s。\n' "$name" "$previous"
      return 0
      ;;
  esac
  if [[ $component == agent ]]; then
    # A boot recovery must not cancel the queued start job; the helper proves
    # the old worker has exited before it examines or stops any retained core.
    if [[ $mode != boot ]] && ! service_stop "$name"; then
      printf '[%s] 停止失败，保留快照与新 helper，禁止覆盖文件。\n' "$name" >&2
      return 1
    fi
    if [[ -n $core_helper ]] && ! snapshot_core_cli --finish-core-stop; then
      printf '[%s] 停核未确认，保留现场：%s\n' "$name" "$snapshot" >&2
      return 1
    fi
  elif [[ $mode != boot ]]; then
    # Not conditional on service_is_active for the reason the old rollback always was: a
    # unit waiting for its restart is "activating", and that is the state a failed update
    # usually leaves behind. A refusal is reported, not fatal: the files go back anyway.
    service_stop "$name" || printf '[%s] 停止 %s 失败。\n' "$name" "$name"
  fi
  if ! restore_snapshot "$component" "$mode"; then
    printf '[%s] 无法恢复更新前的文件：快照保留在 %s。\n' "$name" "$directory_arg"
    return 1
  fi
  remove_install_staging
  if [[ $component == panel && -n $backup ]]; then
    case $phase in
      started) restore=true ;;
      prepared)
        if database_differs "$backup"; then restore=true
        else
          case $? in
            1) ;;
            *) printf '数据库比较失败；保留快照，停止恢复。\n' >&2; return 1;;
          esac
        fi ;;
    esac
    if $restore; then
      printf '[%s] 恢复更新前的数据库 ...\n' "$name"
      if preserved=$(preserve_database "failed-update-$target"); then
        if [[ -n $preserved ]]; then printf '[%s] 已保留更新后的数据库：%s\n' "$name" "$preserved"; fi
      else
        printf '[%s] 无法保留更新后的数据库；仍将恢复备份。\n' "$name"
      fi
      if ! restore_database "$backup"; then
        printf '[%s] 数据库没有恢复，备份仍保留在 %s；快照保留在 %s。\n' "$name" "$backup" "$directory_arg"
        return 1
      fi
    fi
  fi
  journal_finalized=true
  if ! journal_write restored; then
    journal_finalized=false
    printf '[%s] 无法记录恢复进度；下次运行会重新恢复（重复恢复是安全的）。\n' "$name"
  fi
  if [[ $mode == boot ]]; then
    finish_recovered_snapshot "$output_status" || return 1
    printf '[%s] 已恢复到 %s（开机时完成被中断的更新回滚）。\n' "$name" "$previous"
    return 0
  fi
  if ! $was_active; then
    finish_recovered_snapshot "$output_status" || return 1
    printf '[%s] 已恢复到 %s；服务原本未运行，未启动。\n' "$name" "$previous"
    printf '[%s] 服务状态：%s\n' "$name" "$(service_state "$name" || true)"
    return 0
  fi
  if service_start "$name" && service_healthy "$name" 10; then
    service_stopped=false
    finish_recovered_snapshot "$output_status" || return 1
    printf '[%s] 已恢复到 %s，服务运行正常（10 秒内无重启）。\n' "$name" "$previous"
    printf '[%s] 服务状态：%s\n' "$name" "$(service_state "$name" || true)"
    return 0
  fi
  printf '[%s] 已恢复到 %s，但旧版本没有恢复健康。\n' "$name" "$previous"
  printf '[%s] 服务状态：%s\n' "$name" "$(service_state "$name" || true)"
  printf '[%s] 日志：%s\n' "$name" "$(service_log_hint "$name" || true)"
  printf '[%s] 快照保留在 %s。\n' "$name" "$directory_arg"
  return 1
}

# Put the component back to the version that ran before the update. The journal decides
# what a rollback has to do - files only, or files and the database - so the caller no
# longer passes a "restore the database too" flag: the phase recorded before install.sh
# ran, and whether the new panel could have touched the database, say it exactly.
# Returns 0 when the previous version is back (and running, if it was), 1 otherwise; the
# caller prints the downgrade instructions only in the second case.
rollback_component() {
  local component=$1 previous=$2 was_active=$3 reason=$4
  local name=proxy-$1
  report_update failed null "$reason"
  report_stage recovery
  printf '\n[%s] %s：回滚到 %s ...\n' "$name" "$reason" "$previous"
  local recovery_status=0 recovery_output='' recovery_report=''
  recovery_report=$(mktemp "$registry/.recovery-report.XXXXXXXX") || return 1
  if recover_snapshot "$snapshot" rollback "${install_output_status:-0}" > "$recovery_report" 2>&1; then
    cat -- "$recovery_report"
    rm -f -- "$recovery_report"
    report_update success
  else
    recovery_status=$?
    recovery_output=$(cat -- "$recovery_report")
    printf '%s\n' "$recovery_output" >&2
    rm -f -- "$recovery_report"
    report_update failed "$recovery_status" "$recovery_output"
    return "$recovery_status"
  fi
}

# Take the update lock before anything that writes to the registry, the service
# definitions or the database. install.sh, run as a child, takes its own lock
# (operation.lock) on its own descriptor 9, so the two never wait for each other, and
# service.sh closes descriptor 9 for the daemons it starts; this lock is what keeps two
# qui-update runs - a retry after a signal, the terminal menu, the boot recovery - from
# taking snapshots and replacing the database at the same time. WAIT_SECONDS is 0 for a
# run that should refuse, and 60 for the boot unit, which waits for an update that is
# still running.
take_update_lock() {
  local wait_seconds=$1 lock=$registry/update.lock waited=0
  [[ ! -L $lock ]] || fail "更新锁是符号链接，拒绝使用：$lock"
  [[ -d $registry && ! -L $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail "安装登记目录必须为 root 0700：$registry"
  # Created under noclobber and 0600 like install.sh creates operation.lock: a lock file
  # that another user could prepare would be a way to block or to follow the update.
  if [[ ! -e $lock ]]; then
    (set -o noclobber; : > "$lock") || fail "无法创建更新锁：$lock"
    chmod 0600 -- "$lock" || fail "无法设置更新锁权限：$lock"
    sync -f -- "$lock" || true
  fi
  [[ -f $lock && ! -L $lock ]] || fail "更新锁不是普通文件：$lock"
  exec 9<>"$lock"
  # A wait is a loop of non-blocking attempts because busybox flock, which is what an
  # Alpine node has, knows -n but not -w.
  until flock -n 9; do
    ((waited < wait_seconds)) || fail '另一个 qui-update 正在运行。'
    sleep 1
    waited=$((waited + 1))
  done
}

# Unknown installed versions remain unordered, as before.
version_is_older() {
  qui_release_is_older "$1" "$2"
}

# Put the service back after a failure that happened before anything was replaced. The
# steps up to the journal only stop the service, so the service is the whole undo; the
# caller then fails with the message the operator has to act on.
restore_service_after_stop() {
  $active_was_active || return 0
  if service_start "proxy-$active_component"; then
    service_stopped=false
  else
    printf '[proxy-%s] 启动 proxy-%s 失败，服务仍是停止状态。\n' \
      "$active_component" "$active_component"
  fi
}

# Ask the target panel binary whether it can open FILE. The binary comes from the
# extracted archive, so the answer is about the version that is about to be installed and
# not about the one that runs now. Its status is read with an explicit if, because a
# command substitution that fails would end the script under set -e before the answer
# could be explained. Returns 0 when the file is usable, 3 when the target cannot open it
# (a database written by a newer panel), 4 when the caller has to refuse - with the
# binary's own report printed - and 2 when the target is too old to know the flag.
preflight_database() {
  local file=$1 check_mode=${2:-direct} header='' binary=$source_dir/bin/proxy-panel output='' status=0 checker=''
  if [[ ! -f $binary || -L $binary ]]; then
    printf '归档里没有面板程序，无法检查数据库：%s\n' "$binary" >&2
    return 4
  fi
  # The archive's file is not run in place: its mode is whatever the archive says, and
  # the extraction directory may sit on a noexec /tmp. A private 0700 copy in the
  # root-only registry is run instead. A program that still cannot be run is treated like
  # one that predates the flag - the health check and the rollback still protect the
  # update - and never as a verdict on the database.
  checker=$registry/.check-panel.$$
  if ! install -o root -g root -m 0700 -- "$binary" "$checker"; then
    rm -f -- "$checker"
    printf '无法准备数据库检查程序，跳过预检。\n' >&2
    return 2
  fi
  if [[ $check_mode == backup ]]; then
    if output=$(database_cli preflight --check-directory "$check_directory" --backup "$file" --checker "$checker" 2>&1); then
      header=${output%%$'\n'*}
      output=${output#*$'\n'}
      if [[ ! $header =~ ^checker-exit:\ (0|[1-9][0-9]{0,2})$ ]] || ((BASH_REMATCH[1] > 255)); then
        printf '数据库工具返回的检查状态无效：%s\n' "$header" >&2
        rm -f -- "$checker"
        return 4
      fi
      status=${BASH_REMATCH[1]}
    else
      printf '%s\n' "$output" >&2
      rm -f -- "$checker"
      return 4
    fi
  elif output=$("$checker" --check-database "$file" 2>&1); then status=0; else status=$?; fi
  rm -f -- "$checker"
  case $status in
    126|127)
      printf '目标版本的面板程序无法在本机运行（%s），跳过预检。\n' "$status" >&2
      return 2;;
    0) return 0;;
    3) return 3;;
    4|5)
      printf '%s\n' "$output" >&2
      return 4;;
    1)
      # A panel from 1.0.2 or older has no such flag: Go's flag package answers with this
      # line and exits 1, so the caller decides instead of guessing a verdict.
      case $output in
        *'flag provided but not defined'*) return 2;;
      esac
      printf '%s\n' "$output" >&2
      return 4;;
    *)
      printf '%s\n' "$output" >&2
      return 4;;
  esac
}

# Check the backup through root-owned paths. copy_database_atomic already requires
# the registry and backup directory to share a filesystem, so hardlinks work here.
# Root never creates or removes anything inside the panel account's directory.
preflight_backup() {
  local status=0
  check_directory=$(mktemp -d "$registry/check.XXXXXXXX") || return 4
  preflight_database "$1" backup || status=$?
  if ! rm -rf -- "$check_directory"; then
    printf '无法删除数据库预检目录：%s\n' "$check_directory" >&2
    return 4
  fi
  check_directory=''
  return "$status"
}

# Stop the component, back up the panel database while it is stopped, snapshot the files
# install.sh may replace, run the bundled install.sh, then start the service again when
# it was active and check that the new version stays up for ten seconds. Every failure
# after the snapshot is taken ends in rollback_component, so an update either leaves the
# new version running or leaves the host as it was. The service name carries no .service
# suffix: the init-system library adds it on systemd.
update_component() {
  local component=$1 name=proxy-$1 mode=--panel
  local binary=$source_dir/bin/proxy-$1 digest='' previous='' backup='' installed=''
  local was_active=false status=0 install_output='' install_output_status=0
  report_stage bootstrap_install
  [[ $component == agent ]] && mode=--node
  previous=$(installed_version "$component")
  if service_is_active "$name"; then was_active=true; fi
  if [[ $component == agent && $managed_agent == true ]]; then
    # Recheck after download and the human confirmation, immediately before the
    # bootstrap restart. Actual switching performs its own atomic queue reserve.
    agent_read_status || fail '无法重新确认受管 Agent 状态。'
    [[ $agent_runtime_state == running && ( $agent_runtime_ready == ready || $agent_runtime_ready == local-recovery ) ]] \
      || fail '受管 Agent 状态已变化，取消引导程序更新。'
    reserve_agent_bootstrap
  fi
  # The globals the journal and the signal handler read are filled before the service
  # stops, so a signal from here on finds a complete picture of what is being replaced.
  active_component=$component
  active_previous=$previous
  active_target=$version
  active_was_active=$was_active
  active_backup=''
  active_database_source=''
  if [[ $component == panel && -n $database_file ]]; then active_database_source=$database_file; fi
  if [[ $component == panel ]]; then
    # These files do not change while the panel serves requests. Prepare and
    # persist them before downtime; the final database backup and prepared
    # journal are deliberately still made after stop.
    take_snapshot "$component" || fail "[$name] 无法在停服前保存回滚快照，服务未停止。"
  fi
  if [[ $component == agent ]]; then
    prepare_core_handover "$binary" || fail '核心交接预检失败，服务未停止。'
    take_snapshot "$component" || fail '无法在停服前保存 Agent 回滚快照与新 helper。'
    journal_write prepared || fail '无法在停服前保存 Agent 更新日志。'
    # No helper means both the installed and the candidate Agent definitively
    # predate core handover (flag rejected), and such an Agent never detaches a
    # core: Pdeathsig and its KillMode=mixed unit end every core with it.
    if ! $was_active && [[ -n $core_helper ]]; then
      snapshot_core_cli --core-handover-revoke-all --reason recovery \
        && snapshot_core_cli --finish-core-stop || fail '服务未运行但遗留核心退出未确认，拒绝更新。'
    elif $core_handover; then
      local handover_arm_status=0
      core_request=$(qui_core_handover_arm snapshot_core_cli --core-handover-arm --scope bootstrap \
        --transaction "${snapshot##*/}" --target-sha256 "$core_target" --updater-pid "$$") || handover_arm_status=$?
      case $handover_arm_status in
        0) [[ -n $core_request && $core_request != *$'\n'* ]] || fail '核心交接 request id 无效。';;
        2) core_handover=false; core_request='';;
        *) fail '无法授权核心交接，服务未停止。';;
      esac
      journal_write prepared || fail '无法记录核心交接请求，保留现场并中止。'
    fi
  fi
  printf '\n[%s] 停止 %s ...\n' "$name" "$name"
  # Set before the stop: bash runs a trapped signal only after the foreground command,
  # so a Ctrl-C during systemctl/rc-service stop is handled the moment the stop returns,
  # and the handler has to know the service may be down. Starting one that still runs
  # is harmless.
  service_stopped=true
  if ! service_stop "$name"; then
    if [[ $component == agent ]]; then
      rollback_component "$component" "$previous" "$was_active" '停止 Agent 失败' \
        || report_failure "$name" "$previous" "$backup"
    else
      dispose_before_failure
    fi
    fail "无法停止 $name，保留恢复材料。"
  fi
  if [[ $component == agent && -n $core_request ]]; then
    local handover_state=''
    handover_state=$(snapshot_core_cli --core-handover-status --request "$core_request") || handover_state=error
    if [[ $handover_state != detached ]]; then
      rollback_component "$component" "$previous" "$was_active" "核心未确认 detached：$handover_state" \
        || report_failure "$name" "$previous" "$backup"
      fail '核心交接未确认，未安装候选版本。'
    fi
  elif [[ $component == agent && -n $core_helper ]]; then
    snapshot_core_cli --core-handover-revoke-all --reason abort \
      && snapshot_core_cli --finish-core-stop || fail '停核未确认，禁止安装候选版本。'
  fi
  if [[ $component == agent && $managed_agent == true ]]; then
    if ! release_agent_bootstrap; then
      rollback_component "$component" "$previous" "$was_active" '引导程序排他保留未正常结束' \
        || report_failure "$name" "$previous" "$backup"
      fail '引导程序排他保留未正常结束，更新没有开始。'
    fi
  fi
  if [[ $component == panel ]]; then
    # backup_database runs in a command substitution, so a fail inside it only ends that
    # subshell; this status check is what keeps a failed backup from leaving the panel
    # stopped.
    if ! backup=$(backup_database "$previous"); then
      remove_database_partials
      restore_service_after_stop
      dispose_before_failure
      fail "[$name] 数据库备份失败，更新没有开始。"
    fi
    active_backup=$backup
    printf '[%s] 数据库已备份：%s\n' "$name" "$backup"
    # With --database the copy that matters was already checked by main before anything
    # stopped; without it, the database the panel ran on is what the target has to open. A
    # target older than the flag cannot answer, and then the update relies on the health
    # check and on the rollback, which restores the database as well.
    if [[ -z $database_file ]]; then
      status=0
      preflight_backup "$backup" || status=$?
      case $status in
        0) ;;
        2) printf '[%s] 目标版本 %s 不支持数据库检查，跳过预检。\n' "$name" "$version";;
        3)
          # Nothing was replaced and the database is the one the copy was taken from, so
          # the copy is removed again instead of piling up with every refused attempt.
          rm -f -- "$backup" "$backup-wal"
          restore_service_after_stop
          dispose_before_failure
          downgrade_guidance "$version"
          exit 1;;
        *)
          restore_service_after_stop
          dispose_before_failure
          fail "[$name] 数据库无法用于这次更新，更新没有开始（备份保留在 $backup）。";;
      esac
    fi
  fi
  printf '[%s] 回滚快照：%s\n' "$name" "$snapshot"
  if [[ $component == panel ]] && ! journal_write prepared; then
    # Every later phase would fail the same way, so the update must not start; the
    # snapshot holds nothing install.sh replaced and is removed again.
    dispose_before_failure
    restore_service_after_stop
    fail "[$name] 无法写入回滚快照的进度日志，更新没有开始。"
  fi
  if [[ -n ${QUI_UPDATE_REPORT_FD:-} ]]; then
    report_event snapshot_prepared running null "$snapshot" || report_failed=true
    sync_report_fd
  fi
  digest=$(sha256sum -- "$binary"); digest=${digest%% *}
  # QUI_UPDATE tells install.sh this is an update: it is run from an update that
  # starts the service itself and skips its first-install hint.
  # Keep install.sh a direct child: its interruption and the update transaction
  # must have the same parent. Capture bytes in the root-owned snapshot instead
  # of introducing a command-substitution process between them.
  status=0
  QUI_UPDATE=1 "$source_dir/deploy/linux/install.sh" "$mode" --binary "$binary" --sha256 "$digest" \
    > "$snapshot/install-output.log" 2>&1 || status=$?
  if ! forward_install_output "$status"; then
    install_output_status=1
    report_update failed null 'Unable to forward preserved install.sh output.'
    rollback_component "$component" "$previous" "$was_active" '无法读取已保存的 install.sh 输出' \
      || report_failure "$name" "$previous" "$backup"
    exit 1
  fi
  if ((status != 0)); then
    if install_output=$(cat -- "$snapshot/install-output.log"); then
      report_update failed "$status" "$install_output"
    else
      install_output_status=1
      report_update failed "$status" 'install.sh failed; its preserved output could not be read.'
    fi
    # The journal still says "prepared", so a panel rollback puts the files back and only
    # restores the database when the new panel got to it anyway.
    rollback_component "$component" "$previous" "$was_active" 'install.sh 失败' \
      || report_failure "$name" "$previous" "$backup"
    if [[ $component == panel ]] && $downgrade; then downgrade_guidance "$version"; fi
    exit 1
  fi
  installed=$(installed_version "$component")
  if [[ $installed != "$version" ]]; then
    rollback_component "$component" "$previous" "$was_active" \
      "install.sh 报告成功，但程序版本是 $installed 而不是 $version" \
      || report_failure "$name" "$previous" "$backup"
    if [[ $component == panel ]] && $downgrade; then downgrade_guidance "$version"; fi
    exit 1
  fi
  if ! $was_active; then
    # No service will start, so only a --database copy has anything left to do. The
    # journal moves to "started" before it, because from there on the database on disk is
    # not the one the update found and a recovery has to put the copy back.
    if [[ $component == panel && -n $database_file ]]; then
      if ! journal_write started; then
        rollback_component "$component" "$previous" "$was_active" '无法记录更新进度' \
          || report_failure "$name" "$previous" "$backup"
        exit 1
      fi
      if ! restore_database "$staged_database" '已换用 --database 指定的数据库'; then
        rollback_component "$component" "$previous" "$was_active" \
          '无法写入 --database 指定的数据库' \
          || report_failure "$name" "$previous" "$backup"
        exit 1
      fi
    fi
    # The update of this component is complete: record that, then remove the snapshot. A
    # snapshot left at an unfinished phase would make the next run roll a working update
    # back, so it is removed even when the record cannot be written.
    journal_finalized=true
    if ! journal_write committed; then
      journal_finalized=false
      if [[ $component == agent ]]; then fail '无法提交未运行 Agent 的更新日志，快照已保留。'; fi
      printf '[%s] 无法把更新记录为完成。\n' "$name"
    fi
    dispose_after_commit || return 1
    if [[ $component == agent ]]; then
      printf '[%s] 已更新到 %s；服务原本未运行，未启动，网页升级未启用（启动服务后重跑 qui-update --component agent）。\n' "$name" "$version"
    else
      printf '[%s] 已更新到 %s；服务原本未运行，未启动。\n' "$name" "$version"
    fi
    active_component=''
    return 0
  fi
  if ! journal_write started; then
    rollback_component "$component" "$previous" "$was_active" '无法记录更新进度' \
      || report_failure "$name" "$previous" "$backup"
    exit 1
  fi
  if [[ $component == panel && -n $database_file ]]; then
    if ! restore_database "$staged_database" '已换用 --database 指定的数据库'; then
      rollback_component "$component" "$previous" "$was_active" \
        '无法写入 --database 指定的数据库' \
        || report_failure "$name" "$previous" "$backup"
      exit 1
    fi
  fi
  printf '[%s] 启动 %s 并检查十秒内是否重启 ...\n' "$name" "$name"
  report_stage bootstrap_health
  if service_start "$name" && component_healthy; then
    if [[ $component == agent && -f /etc/systemd/system/proxy-agent-state-reset.service ]]; then systemctl start --no-block proxy-agent-state-reset.service || fail '???? Agent ????????'; elif [[ $component == agent && -f /etc/init.d/proxy-agent-state-reset ]]; then rc-service proxy-agent-state-reset start || fail '???? Agent ????????'; fi
    service_stopped=false
    journal_finalized=true
    if ! journal_write committed; then
      journal_finalized=false
      if [[ $component == agent ]]; then
        rollback_component "$component" "$previous" "$was_active" '无法提交 Agent 更新日志' \
          || report_failure "$name" "$previous" "$backup"
        exit 1
      fi
      printf '[%s] 无法把更新记录为完成。\n' "$name"
    fi
    if [[ $component == agent && -n $core_request ]]; then
      local adopted_request=$core_request
      snapshot_core_cli --core-handover-revoke --request "$core_request" --reason done \
        || fail '更新已提交，但撤销交接授权失败，快照与 helper 已保留。'
      core_request=''
      printf '[%s] 核心交接已认领并提交：%s。\n' "$name" "$adopted_request"
    fi
    dispose_after_commit || return 1
    if [[ $component == agent ]]; then
      agent_enable_web_upgrade || true
      if [[ -f ${QUI_AGENT_DATA:-/var/lib/proxy-agent}/identity/agent-supervisor.enc && ! -e ${QUI_AGENT_DATA:-/var/lib/proxy-agent}/identity/agent-supervision-init.pending && ! -L ${QUI_AGENT_DATA:-/var/lib/proxy-agent}/identity/agent-supervision-init.pending ]]; then
        printf '[%s] 引导程序已更新到 %s；实际 Agent 将由本机受管安装事务核验。\n' "$name" "$version"
      else
        printf '[%s] 已更新到 %s，服务运行正常（10 秒内无重启）；网页升级未启用。\n' "$name" "$version"
      fi
    else
      printf '[%s] 已更新到 %s，服务运行正常（10 秒内无重启）。\n' "$name" "$version"
    fi
    active_component=''
    report_update success
    return 0
  fi
  printf '[%s] 新版本 %s 没有持续运行。\n' "$name" "$version"
  printf '[%s] 日志：%s\n' "$name" "$(service_log_hint "$name" || true)"
  rollback_component "$component" "$previous" "$was_active" "新版本 $version 未能持续运行" \
    || report_failure "$name" "$previous" "$backup"
  if [[ $component == panel ]] && $downgrade; then downgrade_guidance "$version"; fi
  exit 1
}

# Turn a signal into the recovery of the update that is in progress, then exit with the
# status the signal deserves. Nothing may interrupt this: the three traps are switched off
# first, so a second Ctrl-C cannot cut a half-done restore short, and set +e is on because
# every step below is checked by hand. HUP is the SSH session going away, and with it the
# terminal this script writes to, so the report goes to a root-only file in the registry
# instead of nowhere.
on_signal() {
  local signal=$1 log=$registry/interrupted-update.log
  trap '' INT TERM HUP
  set +e
  report_update failed null "Updater received SIG$signal during $update_stage."
  report_stage "signal_${signal,,}"
  if [[ $signal == HUP ]]; then
    if [[ ! -e $log ]]; then
      (set -o noclobber; : > "$log") 2>/dev/null || true
      chmod 0600 -- "$log" 2>/dev/null || true
    fi
    if [[ -f $log && ! -L $log ]]; then exec >>"$log" 2>&1 || true; fi
  fi
  printf '\n更新被中断（%s）...\n' "$signal"
  release_agent_bootstrap
  if $managed_installing; then
    managed_core_rollback || printf 'worker 停核屏障失败，现场保留在 %s。\n' "$managed_core_journal" >&2
    printf '引导程序阶段已提交；实际 Agent 的任务和结果保留在节点受管日志中，请检查服务后重跑命令。\n'
  fi
  # The private copy preflight_database runs, if the signal came during the check.
  rm -f -- "$registry"/.check-panel.* 2>/dev/null
  [[ -z $check_directory ]] || rm -rf -- "$check_directory"
  if [[ -n $snapshot && -f $snapshot/journal ]]; then
    # A snapshot with a journal knows what was already replaced, so the one recovery
    # routine decides how much of the update has to be undone.
    report_stage recovery
    if recover_snapshot "$snapshot" signal; then report_update success; else report_update failed "$?" 'Interrupted updater recovery failed; original recovery output is retained in the task log.'; fi
  else
    # No journal means install.sh never started: a partial snapshot only has to be
    # removed, and the service that this run stopped goes back up.
    if [[ -n $snapshot && -d $snapshot ]]; then rm -rf -- "$snapshot"; fi
    snapshot=''
    remove_database_partials
    if $service_stopped && $active_was_active; then
      service_start "proxy-$active_component" || printf '[proxy-%s] 服务没有重新启动，请手工启动。\n' "$active_component"
      service_stopped=false
    fi
  fi
  case $signal in
    INT) exit 130;;
    TERM) exit 143;;
    *) exit 129;;
  esac
}

# Finish or undo an update that an earlier run did not complete: every rollback.*
# directory of the registry is a snapshot, and the journal in it says what is left to do.
# Each one is handed to its own updater copy - the code that wrote the journal - in a child
# process, because that copy is the version the journal was written with and it renames and
# removes the snapshot itself. This run already holds the update lock and says so in the
# environment, so the child does not take it again. A rollback.* directory without a
# journal was left by an updater from before journals existed; nothing of it replaced
# anything, so it is only reported. a discard.* directory is a snapshot whose removal was
# interrupted and holds nothing that is needed any more.
remove_discarded_snapshots() {
  local pending=''
  for pending in "$registry"/discard.*; do
    [[ -e $pending || -L $pending ]] || continue
    if [[ ! -d $pending || -L $pending ]]; then printf '跳过无法识别的残留：%s\n' "$pending" >&2; continue; fi
    state_cli discard --registry "$registry" --discarded "$pending" || fail "无法清理已丢弃快照：$pending"
  done
}

recover_interrupted_updates() {
  local pending='' updater='' temporary=''
  local journal_component='' journal_previous='' journal_target='' journal_phase=''
  local journal_was_active='' journal_backup='' journal_database_source=''
  qui_core_handover_recover_web "$registry" interactive \
    || fail '中断的网页初始化停核未确认，保留现场并中止更新。'
  for temporary in "$registry"/.state-helper.* "$registry"/.update-event.* "$registry"/.database-helper.*; do
    [[ ! -f $temporary || -L $temporary ]] || rm -f -- "$temporary" || fail "无法删除残留工具或事件：$temporary"
  done
  # A kill -9 or power loss during preflight can leave private backup hardlinks.
  for pending in "$registry"/check.*; do
    [[ -e $pending || -L $pending ]] || continue
    if [[ ! -d $pending || -L $pending ]]; then
      printf '跳过无法识别的残留：%s\n' "$pending" >&2
      continue
    fi
    rm -rf -- "$pending"
  done
  for pending in "$registry"/rollback.*; do
    [[ -e $pending || -L $pending ]] || continue
    if [[ ! -d $pending || -L $pending ]]; then
      fail "安装登记目录里有不是目录的快照：$pending（请检查后手工处理）"
    fi
    if [[ ! -e $pending/journal ]]; then
      printf '保留的旧快照：%s（旧版 qui-update 留下，可手工检查后删除）\n' "$pending"
      continue
    fi
    [[ $(stat -c '%u:%g:%a' -- "$pending") == 0:0:700 ]] || fail "快照目录权限无效：$pending"
    [[ -f $pending/journal && ! -L $pending/journal && $(stat -c '%u:%g:%a' -- "$pending/journal") == 0:0:600 ]] || fail "快照 journal 权限无效：$pending"
    printf '发现被中断的更新：%s，交给其快照 updater 恢复。\n' "$pending"
    updater=$pending/updater
    if [[ ! -f $updater || -L $updater || $(stat -c '%u:%g:%a' -- "$updater") != 0:0:600 ]]; then
      fail "快照 $pending 没有 updater，无法恢复（请检查后手工处理）。"
    fi
    PROXYCONTROL_UPDATE_LOCKED=1 QUI_UPDATE_REPORT_FD= bash "$updater" --recover "$pending" 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- \
      || fail "恢复被中断的更新失败：$pending（请查看上面的输出，快照未删除）"
  done
}

# Everything else runs here, and the call of this function is the last line of the file.
# install.sh replaces /usr/local/bin/qui-update while bash is running it, and bash reads
# a script incrementally, so a replaced file could otherwise feed it different commands
# than the ones it started with. Calling a function that was fully parsed before anything
# was replaced removes that risk; the rename install.sh uses already gives the new file a
# new inode, so this is the second line of defence rather than the only one. The call is
# guarded by a comparison with $0 because the tests source this file instead of running it.
cleanup() {
  local original_status=$? guard_status=0
  release_agent_bootstrap || guard_status=$?
  if $managed_installing && ! managed_core_rollback; then
    printf 'worker 停核屏障失败，现场保留在 %s。\n' "$managed_core_journal" >&2
    if ((original_status == 0)); then original_status=1; fi
  fi
  if [[ -n $agent_inputs ]]; then rm -rf -- "$agent_inputs" || guard_status=1; fi
  if [[ -n $directory ]]; then rm -rf -- "$directory" || guard_status=1; fi
  if ((original_status == 0 && guard_status != 0)); then original_status=$guard_status; fi
  report_terminal "$original_status" || { if ((original_status == 0)); then original_status=1; fi; }
  cleanup_state_files || { if ((original_status == 0)); then original_status=1; fi; }
  exit "$original_status"
}

main() {
  local assume_yes=false bundle_file='' sums_file='' agent_manifest_file='' arch='' base='' bundle=''
  local release='' tag='' expected='' actual='' up_to_date=true answer=''
  local components=() component tool file required service_library=''
  local selected_component=''
  local recover_directory='' boot=false status=0 installed_panel='' installed_agent='' output=''
  local snapshot_directory='' registry_directory='' log='' has_agent=false
  while (($#)); do
    case $1 in
      --component)
        (($# >= 2)) || fail '缺少 --component 的值。'
        case $2 in panel|agent) selected_component=$2;; *) fail '--component 必须是 panel 或 agent。';; esac
        shift 2;;
      --version)
        (($# >= 2)) || fail '缺少 --version 的值。'
        version=$2; shift 2;;
      --bundle|--sums)
        (($# >= 2)) || fail "缺少 $1 的值。"
        if [[ $1 == --bundle ]]; then bundle_file=$2; else sums_file=$2; fi
        shift 2;;
      --agent-manifest)
        (($# >= 2)) || fail '缺少 --agent-manifest 的值。'
        agent_manifest_file=$2; shift 2;;
      --database)
        (($# >= 2)) || fail '缺少 --database 的值。'
        database_file=$2; shift 2;;
      --recover)
        (($# >= 2)) || fail '缺少 --recover 的值。'
        recover_directory=$2; shift 2;;
      --boot) boot=true; shift;;
      --yes) assume_yes=true; shift;;
      --help) trap - EXIT; usage; exit 0;;
      *) fail "未知参数：$1（用 --help 查看用法）";;
    esac
  done
  # --recover and --boot are the internal entry points of a snapshot's updater and of the
  # boot unit. They undo what an earlier run left behind and nothing else, so mixing them
  # with update options would be ambiguous.
  if [[ -n $recover_directory || $boot == true ]]; then
    [[ -n $recover_directory ]] || fail '--boot 只能与 --recover 一起使用。'
    if [[ -n $selected_component || -n $version || -n $database_file || -n $bundle_file || -n $sums_file || -n $agent_manifest_file || $assume_yes == true ]]; then
      fail '--recover 不能与更新参数一起使用。'
    fi
  fi
  # Offline update: either both local files or neither of them, plus --version, because
  # the archive name checked in SHA256SUMS carries the version. Both are read as regular
  # files and never through a symlink, like the file curl writes on the download path.
  if [[ -n $bundle_file || -n $sums_file ]]; then
    [[ -n $bundle_file && -n $sums_file ]] || fail '--bundle 与 --sums 必须同时给出。'
    [[ -n $version ]] || fail '离线更新（--bundle/--sums）必须同时用 --version 指定版本。'
    for file in "$bundle_file" "$sums_file"; do
      [[ -f $file && ! -L $file ]] || fail "离线更新的文件必须是普通文件，不能是符号链接：$file"
    done
  fi
  if [[ -n $agent_manifest_file ]]; then
    [[ -n $bundle_file ]] || fail '--agent-manifest 仅用于 --bundle/--sums 离线更新。'
    [[ -f $agent_manifest_file && ! -L $agent_manifest_file ]] \
      || fail '--agent-manifest 必须是普通文件，不能是符号链接。'
  fi

  [[ $(uname -s) == Linux && $EUID -eq 0 ]] || fail '请以 root 身份在 Linux 上运行。'
  # The init-system library shared with install.sh, enroll.sh and uninstall.sh: this
  # script is installed as /usr/local/bin/qui-update and has no sibling in an
  # archive, so the installed copy is what it normally reads. It is not reimplemented
  # inline: a stopped service must be started again by exactly the same command that
  # install.sh and uninstall.sh use, and a second copy of that logic would be an
  # untested dialect. An installation old enough to predate the library is told to
  # run the archive's install.sh once, which writes the library and this script.
  # Run from an archive, the sibling copy wins, as service.sh documents: the installed
  # library may be older and lack a helper this script calls (service_healthy).
  service_library=$root/usr/local/lib/proxycontrol/service.sh
  if [[ -f $(dirname -- "${BASH_SOURCE[0]}")/service.sh ]]; then
    service_library=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/service.sh
  fi
  [[ -f $service_library && ! -L $service_library && $(stat -c '%u' "$service_library" 2>/dev/null) == 0 ]] \
    || fail "缺少初始化系统库 $service_library：这是旧版安装。请用与安装版本相同的发布归档执行一次 deploy/linux/install.sh，再重跑本脚本。"
  # shellcheck source=service.sh
  . "$service_library"
  # A library older than this script lacks the release helpers; without this check
  # version_is_older would return 127 and silently skip the downgrade guard.
  declare -F qui_valid_release_version qui_release_is_older >/dev/null \
    || fail "初始化系统库 $service_library 版本过旧：请用与本脚本同一发布归档里的 deploy/linux/update.sh 运行。"
  [[ -z $version ]] || qui_valid_release_version "$version" || fail '版本号必须是 X.Y.Z 或 X.Y.Z-rc.N（N 为无前导零的正整数）。'
  init_system=$(detect_init) || fail '既不是 systemd 也不是 OpenRC：不支持本机的初始化系统。'
  if [[ -n $recover_directory ]]; then
    # The recovery entry point. A snapshot runs its own updater with --recover DIR and the
    # boot unit does the same with --boot; only a snapshot this script created is accepted
    # - a direct child of the registry, root-owned 0700, with a root-owned 0600 journal -
    # because that directory decides which files and which database are put back. The
    # journal is read and never executed.
    snapshot_directory=$(realpath -e -- "$recover_directory") || fail "快照目录不存在：$recover_directory"
    # The registry is resolved the same way, so a /var reached through a symlink still
    # matches; what is compared is where the two paths really are.
    registry_directory=$(realpath -e -- "$registry") || fail "缺少安装登记目录：$registry"
    [[ ${snapshot_directory%/*} == "$registry_directory" && ${snapshot_directory##*/} == rollback.* ]] \
      || fail "不是本脚本创建的更新快照：$snapshot_directory"
    [[ -d $snapshot_directory && ! -L $snapshot_directory ]] \
      && [[ $(stat -c '%u:%a' -- "$snapshot_directory") == 0:700 ]] \
      || fail "快照目录必须是 root 拥有的 0700 目录：$snapshot_directory"
    [[ -f $snapshot_directory/journal && ! -L $snapshot_directory/journal ]] \
      && [[ $(stat -c '%u:%a' -- "$snapshot_directory/journal") == 0:600 ]] \
      || fail "快照缺少 root 拥有的 0600 进度日志：$snapshot_directory/journal"
    # The parent of a nested recovery already holds the lock and says so in the
    # environment; the boot unit waits for an update that is still running, while an
    # operator's --recover refuses instead of blocking a terminal.
    if [[ ${PROXYCONTROL_UPDATE_LOCKED:-} != 1 ]]; then
      if $boot; then take_update_lock 60; else take_update_lock 0; fi
    fi
    if $boot; then
      # A boot recovery runs before anyone can read the console, so its output goes to a
      # root-only log as well as to the boot log.
      log=$registry/update-recovery.log
      if [[ ! -e $log ]]; then
        (set -o noclobber; : > "$log") || fail "无法创建恢复日志：$log"
        chmod 0600 -- "$log" || fail "无法设置恢复日志权限：$log"
      fi
      [[ -f $log && ! -L $log ]] || fail "恢复日志不是普通文件：$log"
      command -v tee >/dev/null || fail '缺少命令：tee'
      exec > >(tee -a "$log") 2>&1
    fi
    status=0
    if $boot; then
      recover_snapshot "$snapshot_directory" boot || status=$?
    else
      recover_snapshot "$snapshot_directory" interactive || status=$?
    fi
    exit "$status"
  fi
  # date comes from the same coreutils package as install and sha256sum; cp, cmp, mkdir
  # and sync are what the snapshot and its restore use; rc-service and rc-update are the
  # OpenRC equivalents of systemctl; flock guards the update lock
  # and realpath resolves --recover.
  required_tools=(curl tar sha256sum install mktemp uname grep sed cp cmp mkdir mv sync chmod stat date rm flock sort realpath runuser id chown cat)
  if [[ $init_system == systemd ]]; then
    required_tools+=(systemctl)
  else
    required_tools+=(rc-service rc-update)
  fi
  for tool in "${required_tools[@]}"; do
    command -v "$tool" >/dev/null || fail "缺少命令：$tool"
  done

  # The agent is updated before the panel, so a panel that cannot be replaced still
  # leaves a working node.
  for component in agent panel; do
    if [[ -z $selected_component || $selected_component == "$component" ]] && record_complete "$component"; then components+=("$component"); fi
  done
  [[ -z $selected_component ]] || record_complete "$selected_component" || fail "指定组件未安装：$selected_component"
  [[ ${#components[@]} -gt 0 ]] || fail "这台主机上没有有效的安装记录：$registry"
  for component in "${components[@]}"; do
    [[ $component != agent ]] || has_agent=true
  done
  [[ -z $agent_manifest_file || $has_agent == true ]] || fail '--agent-manifest 必须用于 Agent 更新。'

  # One update at a time, taken before this run writes a snapshot, a service definition or
  # the database: the registry is what the next run reads to finish what an interrupted one
  # left behind, and two updaters must not write it at once. An update that an earlier run
  # did not finish is completed or undone here, before the up-to-date check below can exit
  # and before anything is downloaded.
  take_update_lock 0
  if [[ $selected_component != agent ]] && record_complete panel; then
    ensure_database_record || fail '无法确认面板数据库路径，更新没有开始。'
  fi
  trap cleanup EXIT
  recover_interrupted_updates
  prepare_state_helper || fail '无法固定本次状态工具，服务未停止。'
  remove_discarded_snapshots

  if $has_agent; then
    agent_read_status || fail '无法读取本机 Agent 升级状态。'
    if $managed_agent; then
      [[ $agent_runtime_state == running ]] || fail '受管 Agent 服务已停止；请先启动服务再升级。'
      [[ $agent_runtime_ready != baseline-required ]] || fail '请先在面板为该节点发布基础配置，再更新 Agent。'
      [[ $agent_runtime_ready == ready || $agent_runtime_ready == local-recovery ]] \
        || fail '受管 Agent 已有任务、恢复未完成或服务异常；请先处理再升级。'
      if [[ -n $bundle_file && -z $agent_manifest_file ]]; then
        fail '受管 Agent 离线更新必须提供 --agent-manifest FILE；不会联网补取清单。'
      fi
    fi
  fi

  # --database only makes sense together with an installed panel, a version to go down to
  # and a copy that was taken while that panel was stopped. It is checked here, before
  # anything is downloaded or stopped.
  if [[ -n $database_file ]]; then
    [[ -n $version ]] || fail '--database 必须与 --version 一起使用。'
    [[ $selected_component != agent ]] || fail '--database 不能与 --component agent 一起使用。'
    record_complete panel || fail '--database 只用于已安装面板的主机。'
    [[ -f $database_file && ! -L $database_file ]] \
      || fail "--database 必须是普通文件，不能是符号链接：$database_file"
  fi

  # An offline update always knows its version, so only the online path asks GitHub for
  # the latest release.
  if [[ -z $version ]]; then
    printf '查询 GitHub 最新发布 ...\n'
    release=$(curl --no-insecure -fsSL --proto '=https' --tlsv1.2 "https://api.github.com/repos/$repository/releases/latest") \
      || fail '无法查询 GitHub 最新发布；请检查网络，或用 --version 指定版本。'
    tag=$(printf '%s\n' "$release" | grep -m1 '"tag_name"' | sed -e 's/.*"tag_name"[[:space:]]*:[[:space:]]*"//' -e 's/".*//') \
      || fail 'GitHub 返回的内容里没有 tag_name。'
    [[ $tag == v* ]] && qui_valid_release_version "${tag#v}" || fail "最新发布的标签不是 vX.Y.Z 或 vX.Y.Z-rc.N：$tag"
    version=${tag#v}
  fi

  case $(uname -m) in
    x86_64) arch=amd64;;
    aarch64|arm64) arch=arm64;;
    *) fail "不支持的架构：$(uname -m)";;
  esac

  up_to_date=true
  for component in "${components[@]}"; do
    [[ $(installed_version "$component") == "$version" ]] || up_to_date=false
    if [[ $component == agent && $managed_agent == true && ( $agent_runtime_version != "$version" || $agent_runtime_ready != ready ) ]]; then
      up_to_date=false
    fi
  done
  if $up_to_date; then
    printf '已是最新版本 %s。\n' "$version"
    # A host upgraded to this version by an older qui-update never ran the enable
    # step, and rerunning qui-update lands here because nothing needs installing.
    for component in "${components[@]}"; do
      if [[ $component == agent ]] && declare -F agent_enable_web_upgrade >/dev/null; then
        agent_enable_web_upgrade || true
      fi
    done
    exit 0
  fi

  # The Agent only moves forward: a release older than the installed Agent is refused
  # before anything is downloaded or stopped, whether it came from --version or from
  # GitHub's latest release. A default run on a combined host updates the Agent too, so
  # going back on the panel there needs --component panel. An installed version that
  # cannot be read stays unordered and is not refused.
  for component in "${components[@]}"; do
    [[ $component == agent ]] || continue
    installed_agent=$(installed_version agent)
    if version_is_older "$version" "$installed_agent"; then
      if [[ -z $selected_component ]] && record_complete panel; then
        fail "不支持把 Agent 降级：目标版本 $version 低于已安装的引导程序 $installed_agent。只回退面板请加 --component panel。"
      fi
      fail "不支持把 Agent 降级：目标版本 $version 低于已安装的引导程序 $installed_agent。"
    fi
    if $managed_agent && version_is_older "$version" "$agent_runtime_version"; then
      fail "不支持把 Agent 降级：目标版本 $version 低于实际 Agent $agent_runtime_version。"
    fi
  done

  # A panel older than the installed one cannot migrate the database back, so every
  # message about this run says so: report_failure prints the matching --database command
  # and downgrade_guidance explains which copy fits. The target version does not have to
  # come with --database, and then the update is expected to fail on its health check.
  if [[ $selected_component != agent ]] && record_complete panel; then
    installed_panel=$(installed_version panel)
    if version_is_older "$version" "$installed_panel"; then downgrade=true; fi
  fi

  bundle=proxy-control-$version-linux-$arch
  base=https://github.com/$repository/releases/download/v$version
  # Only the temporary download directory is removed by the trap: a rollback snapshot is
  # removed by the update that took it, or kept for the operator when the rollback itself
  # did not come up.
  directory=$(mktemp -d) || fail '无法创建临时目录。'
  if [[ " ${components[*]} " == *' panel '* ]]; then prepare_database_helper || fail '无法固定本次数据库工具，服务未停止。'; fi
  # The running script and the library it sourced are staged here, because take_snapshot
  # stores them in the snapshot: a recovery has to run the code that wrote the journal, and
  # install.sh replaces /usr/local/bin/qui-update while this script is running. The library
  # is the copy this run resolved, so the recovery has the helpers this code calls.
  install -m 0600 -- "${BASH_SOURCE[0]}" "$directory/updater" || fail '无法暂存更新脚本副本。'
  install -m 0600 -- "$service_library" "$directory/service.sh" || fail '无法暂存初始化系统库副本。'
  # From here on a signal has to end in a recovery: Ctrl-C, a closing SSH session and a
  # killed terminal are exactly the reasons the journal exists. The EXIT trap above stays
  # and still removes the temporary directory, because it runs after these handlers.
  trap 'on_signal INT' INT
  trap 'on_signal TERM' TERM
  trap 'on_signal HUP' HUP
  if [[ -n $bundle_file ]]; then
    # Offline update: the two local files are copied under the names the download path
    # uses, so the digest check below is the same code for both and the archive is
    # verified against its SHA256SUMS either way.
    install -m 0600 -- "$bundle_file" "$directory/$bundle.tar.gz" || fail '无法复制离线归档。'
    install -m 0600 -- "$sums_file" "$directory/SHA256SUMS" || fail '无法复制离线 SHA256SUMS。'
    printf '使用离线归档 %s 与其 SHA256SUMS。\n' "${bundle_file##*/}"
  else
    report_stage download
    printf '下载 %s ...\n' "$bundle.tar.gz"
    download "$base/$bundle.tar.gz" "$directory/$bundle.tar.gz"
    download "$base/SHA256SUMS" "$directory/SHA256SUMS"
  fi

  report_stage verification
  expected=$(sums_digest "$bundle.tar.gz") || fail "SHA256SUMS 里没有 $bundle.tar.gz 的条目。"
  actual=$(sha256sum -- "$directory/$bundle.tar.gz"); actual=${actual%% *}
  [[ $actual == "$expected" ]] || fail "归档 SHA256 与 SHA256SUMS 不一致：$bundle.tar.gz"

  source_dir=$directory/$bundle
  tar --no-same-owner -xzf "$directory/$bundle.tar.gz" -C "$directory" || fail '归档解压失败。'
  [[ -d $source_dir && ! -L $source_dir ]] || fail "归档里缺少 $bundle/ 目录。"
  for component in "${components[@]}"; do
    for required in "bin/proxy-$component" "deploy/linux/install.sh"; do
      [[ -f $source_dir/$required && ! -L $source_dir/$required ]] || fail "归档里缺少 $required。"
    done
  done

  if $managed_agent; then
    if [[ -z $bundle_file ]]; then
      agent_manifest_file=$directory/proxy-agent-$version-linux-$arch.manifest.json
      download "$base/proxy-agent-$version-linux-$arch.manifest.json" "$agent_manifest_file"
    fi
    report_stage preflight
    prepare_managed_release "$agent_manifest_file" "$source_dir/bin/proxy-agent"
  fi

  # --database moves the panel down a version together with a copy from before the newer
  # one ran: a copy the target cannot open, or a target that is not older, would either
  # start a broken panel or migrate the copy forward again and make it pointless. Both
  # versions must use the supported release grammar to be ordered, which version_is_older also checks.
  if [[ -n $database_file ]]; then
    if ! version_is_older "$version" "$installed_panel"; then
      fail '--database 只用于把面板降级到更旧的版本。'
    fi
    # Root CLI explicitly authorizes this input, unlike the panel's configuration.
    # Freeze it before preflight/stop/install; subsequent restores read only this copy.
    staged_database=$directory/owner-database.db
    install -m 0600 -- "$database_file" "$staged_database" || fail '无法暂存 --database 备份。'
    if [[ -f $database_file-wal && ! -L $database_file-wal ]]; then
      install -m 0600 -- "$database_file-wal" "$staged_database-wal" || fail '无法暂存 --database WAL。'
    fi
    # Nothing has stopped yet, so a copy the target cannot use is refused while the panel
    # is still up.
    status=0
    preflight_database "$staged_database" || status=$?
    case $status in
      0) ;;
      2)
        # The target predates --check-database (1.0.2 and older). The installed panel is
        # newer than the target, so a copy it reads as compatible is one the older target
        # can open; every other verdict is refused with the report it printed.
        output=''
        status=0
        output=$("$program_directory/proxy-panel" --check-database "$staged_database" 2>&1) || status=$?
        if ((status != 0)); then
          printf '%s\n' "$output" >&2
          if ((status == 3)); then
            fail "$database_file 由比 $installed_panel 更新的面板写入，$version 无法打开。"
          fi
          fail "$database_file 无法通过已安装面板的数据库检查：$program_directory/proxy-panel"
        fi;;
      3) fail "$database_file 也包含目标版本 $version 不认识的迁移，不能与它一起安装。";;
      *) fail "$database_file 不是 $version 能打开的面板数据库，不能与它一起安装。";;
    esac
  fi

  printf '\n目标版本：%s\n' "$version"
  for component in "${components[@]}"; do
    printf 'proxy-%s：%s → %s\n' "$component" "$(installed_version "$component")" "$version"
    if [[ $component == agent && $managed_agent == true ]]; then
      printf '实际 Agent：%s → %s\n' "$agent_runtime_version" "$version"
    fi
  done
  printf '归档 SHA256 与 SHA256SUMS 一致：%s\n' "$expected"
  if [[ -n $database_file ]]; then
    printf '同时替换面板数据库：%s（%s）\n' "$database_file" \
      "$(stat -c '%y' -- "$database_file" 2>/dev/null || printf '修改时间未知')"
    printf '警告：该备份之后写入的数据会丢失（新用户、新建的设备、配置改动）；\n'
    printf '      之后登记的节点需要重新登记。\n'
  fi
  if ! $assume_yes; then
    printf '确认更新？[y/N] '
    read -r answer || answer=''
    case $answer in
      y|Y) ;;
      *) printf '已取消更新。\n'; exit 0;;
    esac
  fi

  for component in "${components[@]}"; do
    if [[ $component != agent || $(installed_version agent) != "$version" ]]; then
      update_component "$component"
    fi
    if [[ $component == agent && $managed_agent == true ]]; then
      install_managed_release || exit 1
    fi
  done

  printf '\n更新完成：\n'
  for component in "${components[@]}"; do
    if [[ $component == agent && $managed_agent == true ]]; then
      printf 'proxy-agent 引导程序 %s；实际 Agent %s（服务状态：%s）\n' \
        "$(installed_version agent)" "$agent_runtime_version" "$(service_state proxy-agent || true)"
    else
      printf 'proxy-%s %s（服务状态：%s）\n' "$component" "$(installed_version "$component")" \
        "$(service_state "proxy-$component" || true)"
    fi
  done
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then main "$@"; fi
