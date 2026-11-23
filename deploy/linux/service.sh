#!/usr/bin/env bash
# Init-system helpers shared by the installation, enrollment, certificate,
# uninstall and update scripts. This file is sourced, never executed.
#
# Why it exists: the panel is systemd-only, but a node agent may run on Debian o
# Ubuntu (systemd) or on Alpine (OpenRC), including an LXC container whose PID 1
# is busybox init and where rc_sys is "lxc". The scripts keep one code path and
# ask this library which command to run, instead of growing a second copy of
# every script with a second set of failure modes.
#
# Locating it: deploy/linux/service.sh ships inside the release archive next to
# the scripts that are run from there, so those source their own sibling.
# install.sh also publishes a root-owned 0644 copy to
# /usr/local/lib/proxycontrol/service.sh, because update.sh is installed as
# /usr/local/bin/qui-update and has no sibling, and uninstall.sh may be run
# either from an archive or from a copy extracted on the host. Callers therefore
# try their own directory first, so an archive always uses the copy it brought,
# and fall back to the installed one. A missing, symlinked or non-root-owned file
# is refused by the caller; this file never guesses which init system is there.
#
# API: detect_init, service_is_active, service_start, service_stop,
# service_restart, service_enable, service_disable, service_reload_definitions,
# service_state, service_healthy and service_log_hint. Service names are given
# without the ".service" suffix and the systemd branch adds it, so the same call
# works on both init systems. Callers set the global init_system from detect_init
# before calling anything else.
# Release helpers: qui_valid_release_version and qui_release_is_older share the
# restricted release grammar and semver precedence with the release scripts.

# Print the init system this host runs: "systemd" or "openrc". Prints nothing and
# returns 1 when neither is running, so the caller decides how to refuse.
detect_init() {
  if [[ -d /run/systemd/system ]]; then
    printf 'systemd\n'
    return 0
  fi
  # OpenRC keeps its state in /run/openrc and its runscript interpreter in
  # /sbin/openrc-run; a stock Alpine host and its LXC containers have both.
  if [[ -d /run/openrc ]]; then
    if [[ -x /sbin/openrc-run ]] || command -v openrc-run >/dev/null 2>&1; then
      printf 'openrc\n'
      return 0
    fi
  fi
  return 1
}

# Every helper below dispatches on $init_system. A script that forgot to fill it
# must fail loudly instead of reporting "inactive" and then deleting files unde
# a running service, so an unset value is an error and not a guess.
init_system_known() {
  case ${init_system:-} in
    systemd|openrc) return 0;;
  esac
  printf 'service.sh: init_system must be systemd or openrc, got "%s"\n' "${init_system:-}" >&2
  return 1
}

# Succeeds when the service runs. systemd answers through is-active; OpenRC has
# no is-active equivalent, and the runscript's status is the equivalent verdict.
service_is_active() {
  local name=$1
  init_system_known || return 1
  case $init_system in
    systemd) systemctl is-active --quiet "$name.service";;
    openrc) rc-service "$name" status >/dev/null 2>&1;;
  esac
}

# systemd starts a service from PID 1, but rc-service forks the supervised daemon
# from this shell, so the daemon inherits every open descriptor. The scripts hold
# the installation lock on descriptor 9, and a daemon started with it open would
# keep the lock for as long as it runs; every later install, enrollment or
# uninstall would then be refused. Starting and restarting therefore close it.
service_start() {
  local name=$1
  init_system_known || return 1
  case $init_system in
    systemd) systemctl start "$name.service";;
    openrc) rc-service "$name" start 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-;;
  esac
}

service_stop() {
  local name=$1
  init_system_known || return 1
  case $init_system in
    systemd) systemctl stop "$name.service";;
    openrc) rc-service "$name" stop;;
  esac
}

service_restart() {
  local name=$1
  init_system_known || return 1
  case $init_system in
    systemd) systemctl restart "$name.service";;
    openrc) rc-service "$name" restart 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-;;
  esac
}

# Start the service at boot. The OpenRC equivalent of `systemctl enable` is
# membership of a runlevel, and "default" is the one a normal boot enters.
service_enable() {
  local name=$1
  init_system_known || return 1
  case $init_system in
    systemd) systemctl enable "$name.service";;
    openrc) rc-update add "$name" default;;
  esac
}

# Removing a service from the default runlevel is idempotent: rc-update reports
# an error for a service that is not in the runlevel, and that is not a failed
# uninstall, so it is tolerated exactly like disabling an already disabled unit.
service_disable() {
  local name=$1
  init_system_known || return 1
  case $init_system in
    systemd) systemctl disable "$name.service";;
    openrc) rc-update del "$name" default 2>/dev/null || true;;
  esac
}

# systemd caches unit files and has to be told when one is replaced; OpenRC reads
# /etc/init.d on every call, so there is nothing to reload.
service_reload_definitions() {
  init_system_known || return 1
  case $init_system in
    systemd) systemctl daemon-reload;;
    openrc) :;;
  esac
}

# Print a one-word state for messages. systemd prints what is-active prints
# (active, inactive, failed, activating, ...). OpenRC has no such vocabulary: the
# runscript's status line is reduced to started, crashed or stopped, and a status
# that cannot be read is reported through the exit code, which is non-zero fo
# every state that is not "started".
service_state() {
  local name=$1 output='' code=0
  init_system_known || return 1
  case $init_system in
    systemd) systemctl is-active "$name.service" 2>/dev/null || true;;
    openrc)
      output=$(rc-service "$name" status 2>&1) || code=$?
      case $output in
        *'status: started'*) printf 'started\n';;
        *'status: crashed'*) printf 'crashed\n';;
        *'status: stopped'*) printf 'stopped\n';;
        *) if ((code == 0)); then printf 'started\n'; else printf 'stopped\n'; fi;;
      esac;;
  esac
}

# Print the main PID of a running service, or fail when there is none to trust.
# systemd is asked for MainPID directly. OpenRC has no such query: supervise-daemon
# writes the child it watches to /run/openrc/options/<service>/child_pid (checked on
# the target host), so the file is read as a file and a missing or non-numeric value
# means "no pid" instead of a number to believe. The pid is also tested with kill -0,
# because the file of a service that just died can still name the dead child.
service_main_pid() {
  local name=$1 pid=''
  init_system_known || return 1
  case $init_system in
    systemd) pid=$(systemctl show -p MainPID --value "$name.service" 2>/dev/null || true);;
    openrc) pid=$(cat "/run/openrc/options/$name/child_pid" 2>/dev/null || true);;
  esac
  [[ $pid =~ ^[1-9][0-9]*$ ]] || return 1
  if [[ $init_system == openrc ]]; then kill -0 "$pid" 2>/dev/null || return 1; fi
  printf '%s\n' "$pid"
}

# Wait for a service to be active with a main process, then watch it for $2 seconds
# (10 by default, one poll per second) and report whether it stayed that way. A
# program that starts, crashes and is started again looks active at every poll, so
# the test is the main PID: both init systems restart a crashed program after five
# seconds (Restart=on-failure RestartSec=5, supervise-daemon respawn_delay=5), which
# changes the PID, and a changed PID is a failed start. A service that is not active,
# or has no PID, at any poll fails as well. Returns 1 in all of those cases and 0 only
# when the service was active with one unchanged PID for the whole window.
service_healthy() {
  local name=$1 seconds=${2:-10} waited=0 first='' pid=''
  init_system_known || return 1
  while ((waited < 15)); do
    if service_is_active "$name" && pid=$(service_main_pid "$name"); then
      first=$pid
      break
    fi
    sleep 1
    waited=$((waited + 1))
  done
  [[ -n $first ]] || return 1
  for ((waited = 0; waited < seconds; waited += 1)); do
    sleep 1
    service_is_active "$name" || return 1
    pid=$(service_main_pid "$name") || return 1
    [[ $pid == "$first" ]] || return 1
  done
  return 0
}

# Root-only CLI calls must not inherit the updater's locks/report descriptors.
qui_core_cli() {
  local binary=$1 config=$2
  shift 2
  "$binary" --config "$config" "$@" 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-
}

# 2 means the legacy Go flag parser rejected this exact capability flag.
# All other failures, including malformed successful output, are errors.
qui_core_handover_protocol() {
  local output='' status=0
  output=$("$1" --core-handover-protocol 2>&1 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-) || status=$?
  if ((status == 0)) && [[ $output == 1 ]]; then return 0; fi
  if ((status != 0)) && [[ $output == *'flag provided but not defined: -core-handover-protocol'* ]]; then
    return 2
  fi
  printf '核心交接能力探测失败（%s）：%s\n' "$1" "$output" >&2
  return 1
}

# 2 is the explicitly approved stop-and-update path when pidfd is unavailable.
# arm probes pidfd before writing authorization; all other errors remain fatal.
qui_core_handover_arm() {
  local output='' status=0
  output=$("$@" 2>&1) || status=$?
  if ((status == 0)); then printf '%s\n' "$output"; return 0; fi
  if [[ $output == *core_handover_unsupported* ]]; then
    printf '核心交接不可用：%s；本次更新会中断连接，按停核更新。\n' "$output" >&2
    return 2
  fi
  printf '%s\n' "$output" >&2
  return 1
}

# The target CLI returns a digest, not a worker path. Probe the matching running
# image through procfs; never guess the configured data directory or image path.
# These PIDs are used only to read/probe executables, never to send signals.
qui_core_handover_source() {
  local binary=$1 digest=$2 actual='' main='' child='' children='' children_file='' thread_children=''
  actual=$(sha256sum -- "$binary") || return 1
  if [[ ${actual%% *} == "$digest" ]]; then
    qui_core_handover_protocol "$binary"
    return $?
  fi
  main=$(service_main_pid proxy-agent) || return 1
  # Go may create the worker from any OS thread, so inspect every task's children.
  for children_file in "/proc/$main"/task/*/children; do
    thread_children=$(cat -- "$children_file") || return 1
    children+=" $thread_children"
  done
  for child in $main $children; do
    actual=$(sha256sum -- "/proc/$child/exe") || return 1
    if [[ ${actual%% *} == "$digest" ]]; then
      qui_core_handover_protocol "/proc/$child/exe"
      return $?
    fi
  done
  printf '无法确认实际 Agent 镜像的交接能力：%s\n' "$digest" >&2
  return 1
}

qui_core_handover_wait() {
  local binary=$1 config=$2 request=$3 state='' waited=0 first='' pid=''
  while ((waited < 300)); do
    if service_is_active proxy-agent && first=$(service_main_pid proxy-agent); then break; fi
    sleep 1
    waited=$((waited + 1))
  done
  [[ -n $first ]] || { printf 'Agent 未提供可观察的服务进程。\n' >&2; return 1; }
  # The Go request enforces the 300-second CLOCK_BOOTTIME deadline. This polling
  # limit also bounds a helper that keeps returning an intermediate state.
  while ((waited < 300)); do
    service_is_active proxy-agent || return 1
    pid=$(service_main_pid proxy-agent) || return 1
    [[ $pid == "$first" ]] || { printf '认领期间 Agent 重启。\n' >&2; return 1; }
    state=$(qui_core_cli "$binary" "$config" --core-handover-status --request "$request") || return 1
    case $state in
      adopted) return 0;;
      armed|prepared|detached|adopting) ;;
      failed|stopped|expired) printf '核心交接失败：%s\n' "$state" >&2; return 1;;
      *) printf '核心交接状态无效：%s\n' "$state" >&2; return 1;;
    esac
    sleep 1
    waited=$((waited + 1))
  done
  printf '等待核心认领超时。\n' >&2
  return 1
}

# A SIGKILL cannot run the initialization trap. The next update/enable attempt
# or boot finds its retained helper and revokes preservation before recovery.
qui_core_handover_recover_web() {
  local registry=$1 mode=$2 transaction='' helper='' expected='' actual=''
  for transaction in "$registry"/web-handover.*; do
    [[ -e $transaction || -L $transaction ]] || continue
    [[ -d $transaction && ! -L $transaction && $(stat -c '%u:%a' -- "$transaction") == 0:700 ]] || return 1
    helper=$transaction/core-stop-helper
    [[ -f $helper && ! -L $helper && $(stat -c '%u:%a' -- "$helper") == 0:500 ]] || return 1
    [[ -f $transaction/sha256 && ! -L $transaction/sha256 && $(stat -c '%u:%a' -- "$transaction/sha256") == 0:600 ]] || return 1
    expected=$(cat -- "$transaction/sha256") || return 1
    actual=$(sha256sum -- "$helper") || return 1
    [[ $expected =~ ^[0-9a-f]{64}$ && ${actual%% *} == "$expected" ]] || return 1
    qui_core_cli "$helper" /etc/proxy-agent/config.yaml --core-handover-revoke-all --reason recovery || return 1
    if [[ $mode != boot ]]; then service_stop proxy-agent || return 1; fi
    qui_core_cli "$helper" /etc/proxy-agent/config.yaml --finish-core-stop || return 1
    rm -rf -- "$transaction" || return 1
    printf '已撤销中断的网页初始化授权并确认停核；初始化状态保留，需重新启用网页升级。\n'
  done
}

# Restore standalone mode only after the caller has confirmed the stop barrier.
# Every failed move/start remains visible and preserves the retained helper.
agent_web_upgrade_failed() {
  local data=$1 stamp backup path
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  backup="$data/web-upgrade-failed-$stamp"
  mkdir -p -- "$backup" && chmod 0700 -- "$backup" || return 1
  for path in "$data/identity/agent-supervisor.enc" "$data/identity/agent-release-checkpoint.enc" \
    "$data/agent-release-history" "$data/releases/agent-installed" "$data/releases/agent-candidates" "$data/releases/agent-local-candidates" \
    "$data/agent-jobs" "$data/identity/agent-jobs-checkpoint.enc" "$data/identity/agent-supervision-init.pending"; do
    [[ -e $path || -L $path ]] || continue
    mv -- "$path" "$backup/" || return 1
  done
  chown -R proxy-agent:proxy-agent -- "$backup" || return 1
  service_start proxy-agent && service_healthy proxy-agent 10 || return 1
  printf '网页升级未启用，节点仍按普通模式运行；失败现场在 %s\n' "$backup" >&2
  return 0
}

agent_enable_web_upgrade() (
  local data marker pending baseline waited=0 binary=/usr/local/bin/proxy-agent
  local config=/etc/proxy-agent/config.yaml supported=false protocol_status=0
  local target='' request='' helper='' transaction='' state='' web_pending=false
  local recovery_reason=rollback exit_status=0 helper_digest='' binary_digest=''
  local updater_pid=$BASHPID
  qui_core_handover_recover_web /var/lib/proxycontrol-install interactive || return 1
  data=${QUI_AGENT_DATA:-/var/lib/proxy-agent}
  marker="$data/identity/agent-supervisor.enc"
  pending="$data/identity/agent-supervision-init.pending"
  if [[ -f $marker && ! -e $pending && ! -L $pending ]]; then
    printf '网页升级已启用\n'
    return 0
  fi
  if ! service_is_active proxy-agent; then
    printf '服务未运行，跳过网页升级启用；请启动服务后重跑 qui-update --component agent。\n'
    return 0
  fi
  # Initialization needs the storage baseline the Agent writes on its first normal
  # start; stopping a freshly started Agent before that would make it fail.
  baseline="$data/identity/agent-storage-contract.enc"
  while [[ ! -f $baseline ]]; do
    if ((waited >= ${QUI_WEB_UPGRADE_WAIT:-60})); then
      printf 'Agent 尚未完成首次启动（未生成存储基线），跳过网页升级启用；稍后在本机运行 qui-update --component agent。\n'
      return 1
    fi
    sleep 1
    waited=$((waited + 1))
    if ! service_is_active proxy-agent; then
      printf '服务未运行，跳过网页升级启用；请启动服务后重跑 qui-update --component agent。\n'
      return 0
    fi
  done
  qui_core_handover_protocol "$binary" || protocol_status=$?
  case $protocol_status in
    0)
      target=$(qui_core_cli "$binary" "$config" --core-handover-target) || return 1
      [[ $target =~ ^[0-9a-f]{64}$ ]] || return 1
      protocol_status=0
      qui_core_handover_source "$binary" "$target" || protocol_status=$?
      case $protocol_status in 0) supported=true;; 2) ;; *) return 1;; esac
      # A retained copy survives failed initialization and is removed only after
      # adoption or a confirmed stop barrier. It never lives in PrivateTmp.
      transaction=$(mktemp -d /var/lib/proxycontrol-install/web-handover.XXXXXXXX) || return 1
      chmod 0700 -- "$transaction" || return 1
      helper=$transaction/core-stop-helper
      install -o root -g root -m 0500 -- "$binary" "$helper" || return 1
      helper_digest=$(sha256sum -- "$helper") || return 1
      binary_digest=$(sha256sum -- "$binary") || return 1
      [[ ${helper_digest%% *} == "${binary_digest%% *}" ]] || return 1
      printf '%s\n' "${helper_digest%% *}" > "$transaction/sha256" || return 1
      chmod 0600 -- "$transaction/sha256" || return 1
      sync -f -- "$helper" && sync -f -- "$transaction/sha256" && sync -f -- "$transaction" \
        && sync -f -- /var/lib/proxycontrol-install || return 1
      ;;
    2) ;;
    *) return 1;;
  esac
  if ! $supported; then printf '首次启用网页升级按停核流程执行，本次更新会中断连接。\n'; fi
  # Isolate traps from update.sh/node-install.sh. A signal before start must
  # revoke preservation before any fallback moves the supervised state.
  trap 'recovery_reason=recovery; exit 130' INT
  trap 'recovery_reason=recovery; exit 143' TERM
  trap 'recovery_reason=recovery; exit 129' HUP
  trap '
    exit_status=$?
    trap "" INT TERM HUP
    if $web_pending; then
      if [[ -n $helper ]]; then
        if [[ $recovery_reason == recovery || -z $request ]]; then
          if [[ $recovery_reason == recovery ]]; then
            qui_core_cli "$helper" "$config" --core-handover-revoke-all --reason recovery || exit 1
          else
            qui_core_cli "$helper" "$config" --core-handover-revoke-all --reason abort || exit 1
          fi
        else
          qui_core_cli "$helper" "$config" --core-handover-revoke --request "$request" --reason rollback || exit 1
        fi
      fi
      service_stop proxy-agent || exit 1
      if [[ -n $helper ]]; then qui_core_cli "$helper" "$config" --finish-core-stop || exit 1; fi
      agent_web_upgrade_failed "$data" || exit 1
      exit_status=1
    fi
    if [[ -n $transaction ]]; then rm -rf -- "$transaction" || exit 1; fi
    exit "$exit_status"
  ' EXIT
  web_pending=true
  if $supported; then
    local handover_arm_status=0
    request=$(qui_core_handover_arm qui_core_cli "$helper" "$config" --core-handover-arm --scope bootstrap \
      --transaction "${transaction##*/}" --target-sha256 "$target" --updater-pid "$updater_pid") || handover_arm_status=$?
    case $handover_arm_status in
      0) [[ -n $request && $request != *$'\n'* ]] || return 1;;
      2) supported=false; request='';;
      *) return 1;;
    esac
  fi
  service_stop proxy-agent || return 1
  if [[ -n $request ]]; then
    state=$(qui_core_cli "$helper" "$config" --core-handover-status --request "$request") || return 1
    [[ $state == detached ]] || { printf '网页升级初始化未确认 detached：%s\n' "$state" >&2; return 1; }
  elif [[ -n $helper ]]; then
    qui_core_cli "$helper" "$config" --core-handover-revoke-all --reason abort \
      && qui_core_cli "$helper" "$config" --finish-core-stop || return 1
  fi
  if ! (umask 077; runuser -u proxy-agent -- /usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --initialize-agent-history); then
    return 1
  fi
  service_start proxy-agent || return 1
  if [[ -n $request ]]; then
    qui_core_handover_wait "$helper" "$config" "$request" || return 1
  fi
  service_healthy proxy-agent 10 || return 1
  if [[ -n $request ]]; then
    qui_core_cli "$helper" "$config" --core-handover-revoke --request "$request" --reason done || return 1
  fi
  web_pending=false
  printf '网页升级已启用\n'
)

# Print a command that shows the last 50 log lines of a service. Both programs
# write to stdout and are collected by the host logger: journald on systemd, and
# busybox syslogd on a stock Alpine node, where /var/log/messages holds every
# line and the init script tags it with the service name.
service_log_hint() {
  local name=$1
  init_system_known || return 1
  case $init_system in
    systemd) printf 'journalctl -u %s -n 50\n' "$name";;
    openrc) printf "grep -F '%s' /var/log/messages | tail -n 50\n" "$name";;
  esac
}

# The request directory belongs to the Agent and may be renamed by that user.
# Drop root before resolving or removing its receipt. This also works when the
# installed helper is an old version or its executable cannot start.
qui_invalidate_tuning_probe() {
  local receipt=$1/probe.json
  if [[ -e $receipt || -L $receipt ]]; then
    runuser -u proxy-agent -- rm -f -- "$receipt"
  fi
}

qui_skip_tuning_replay() {
  install -d -m 0711 -o root -g root "$1" && touch "$1/skip-replay"
}

# Release versions shared by the installers, release builder and updater.
# Keep the standalone node-install.sh validator identical (it runs before download).
qui_valid_release_version() {
  [[ $1 =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$ ]]
}

# Compare canonical decimal strings without shell-integer overflow.
qui_decimal_is_older() {
  [[ ${#1} -lt ${#2} ]] || { [[ ${#1} -eq ${#2} && $1 < $2 ]]; }
}

# Semver precedence for our restricted release grammar; unknown versions are
# unordered. Stable sorts AFTER every RC with the same major/minor/patch.
qui_release_is_older() {
  qui_valid_release_version "$1" && qui_valid_release_version "$2" || return 1
  local left=${1%%-rc.*} right=${2%%-rc.*} i
  local a=() b=()
  IFS=. read -r -a a <<< "$left"
  IFS=. read -r -a b <<< "$right"
  for i in 0 1 2; do
    if [[ ${a[i]} != "${b[i]}" ]]; then
      qui_decimal_is_older "${a[i]}" "${b[i]}"
      return $?
    fi
  done
  [[ $1 == *-rc.* ]] || return 1
  [[ $2 == *-rc.* ]] || return 0
  qui_decimal_is_older "${1##*-rc.}" "${2##*-rc.}"
}

# Record v2 retains metadata lines 2-12; lines 5/6 are reserved empty.
# Each following line is: core NAME ABSOLUTE_PATH SHA256. Never source records.
# Paths are deliberately restricted to installer-owned destinations.
qui_record_cores() {
  local record=$1 component=$2 format name path digest extra line remainder
  record_cores=(); record_digests=()
  [[ -f $record && ! -L $record ]] || return 1
  format=$(sed -n '1p' "$record")
  [[ $format == proxycontrol-install-v1 || $format == proxycontrol-install-v2 ]] || return 1
  [[ $(sed -n '2p' "$record") == "$component" && $(sed -n '7p' "$record") == installed ]] || return 1
  if [[ $format == proxycontrol-install-v1 ]]; then
    name=$(sed -n '5p' "$record"); digest=$(sed -n '6p' "$record")
    if [[ -n $name ]]; then
      [[ $component == agent && ( $name == xray || $name == sing-box || $name == gost ) && $digest =~ ^[0-9a-f]{64}$ ]] || return 1
      record_cores+=("$name"); record_digests+=("$digest")
    else [[ -z $digest ]] || return 1; fi
    return 0
  fi
  [[ -z $(sed -n '5p' "$record") && -z $(sed -n '6p' "$record") && $(sed -n '10p' "$record") =~ ^(systemd|openrc)$ ]] || return 1
  [[ $(sed -n '$=' "$record") -ge 12 ]] || return 1
  while IFS= read -r line || [[ -n $line ]]; do
    read -r extra name path digest remainder <<< "$line"
    [[ $extra == core && $component == agent && ( $name == xray || $name == sing-box || $name == gost ) && $path == /usr/local/lib/proxy-agent/"$name" && $digest =~ ^[0-9a-f]{64}$ && -z $remainder ]] || return 1
    [[ " ${record_cores[*]} " != *" $name "* ]] || return 1
    record_cores+=("$name"); record_digests+=("$digest")
  done < <(sed -n '13,$p' "$record")
}

qui_write_record() {
  local destination=$1 i
  printf 'proxycontrol-install-v2\n%s\n%s\n%s\n\n\ninstalled\n%s\n%s\n%s\n%s\n%s\n' \
    "$component" "$digest" "$unit_digest" "$host_network_service_digest" "$host_network_path_digest" \
    "$init_system" "$tuning_service_digest" "$tuning_path_digest" > "$destination"
  for i in "${!record_cores[@]}"; do
    printf 'core %s /usr/local/lib/proxy-agent/%s %s\n' "${record_cores[i]}" "${record_cores[i]}" "${record_digests[i]}" >> "$destination"
  done
}

# Only replace the three top-level core settings, preserving other YAML blocks.
qui_agent_core_config() {
  local source=$1 destination=$2 core
  awk '/^["\047]?(cores|core_name|core_path)["\047]?[[:space:]]*:/ {skip=1; next} /^[^[:space:]#]/ {skip=0} !skip {print}' "$source" > "$destination" || return 1
  if [[ ${#record_cores[@]} == 0 ]]; then
    printf 'cores: []\n' >> "$destination" || return 1
  elif [[ ${record_cores[0]} == gost ]]; then
    # No user core: the list alone is authoritative (v1.9.0+ Agents only).
    printf 'cores:\n' >> "$destination" || return 1
  else
    # The scalar pair keeps old Agents usable during a rollback; cores is
    # authoritative for the multi-core Agent. User cores always precede GOST.
    printf 'core_name: %s\ncore_path: /usr/local/lib/proxy-agent/%s\ncores:\n' "${record_cores[0]}" "${record_cores[0]}" >> "$destination" || return 1
  fi
  for core in "${record_cores[@]}"; do
    printf '  - name: %s\n    path: /usr/local/lib/proxy-agent/%s\n' "$core" "$core" >> "$destination" || return 1
  done
}
