#!/usr/bin/env bash
# Removal preserves all identity, consumption, configuration and recovery data.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
fail() { printf 'Uninstall refused: %s\n' "$*" >&2; exit 1; }
if [[ ${1:-} == --help ]]; then
  printf '用法：uninstall.sh panel|agent [--purge]\n      uninstall.sh all --purge\n普通卸载保留数据；--purge 交互确认后清除全部数据。\n'; exit 0
fi
purge=false
if [[ $# == 2 && $2 == --purge ]]; then purge=true; fi
[[ ( $# == 1 && ( $1 == panel || $1 == agent ) ) || ( $# == 2 && $purge == true && ( $1 == panel || $1 == agent || $1 == all ) ) ]] || fail 'Use uninstall.sh panel|agent [--purge] or all --purge.'
[[ $(uname -s) == Linux && $EUID == 0 ]] || fail 'Run locally as root on Linux.'
# The init-system helpers (deploy/linux/service.sh) are sourced before anything is
# removed, because the installed copy is deleted at the end of this script: an
# archive's sibling copy wins, and the installed one is read only when this script
# was extracted on its own. Anything but a root-owned regular file is refused
# rather than read as root.
service_library=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/service.sh
if [[ ! -f $service_library ]]; then service_library=/usr/local/lib/proxycontrol/service.sh; fi
[[ -f $service_library && ! -L $service_library && $(stat -c '%u' "$service_library" 2>/dev/null) == 0 ]] \
  || fail "Missing the init-system library service.sh: $service_library"
# shellcheck source=service.sh
. "$service_library"
init_system=$(detect_init) || fail 'Neither systemd nor OpenRC is running on this host.'
requested=$1
uninstall_component() {
component=$1
name=proxy-$component
registry=/var/lib/proxycontrol-install
record=$registry/$component.manifest
removed=$registry/$component.removed
target=/usr/local/bin/$name
# The panel's terminal menu is removed with the program.
qui=/usr/local/bin/qui
# The updater and the init-system library serve both components, so they are
# removed only when no other component of this platform is installed.
update=/usr/local/bin/qui-update
library=/usr/local/lib/proxycontrol/service.sh
# The boot-time hook that finishes an interrupted update belongs to both
# components in the same way: it is inert without a journal, and the updater that
# writes the journals is what has to be gone before the hook is useless.
recovery=/usr/local/lib/proxycontrol/update-recovery
# The service definitions follow the init system the installation was made for:
# a unit file on systemd, an init script on OpenRC, plus the watcher the OpenRC
# init script supervises where systemd has a path unit.
if [[ $init_system == systemd ]]; then
  unit=/etc/systemd/system/$name.service
  host_network_service=/etc/systemd/system/proxy-agent-host-network.service
  host_network_path=/etc/systemd/system/proxy-agent-host-network.path
  tuning_service=/etc/systemd/system/proxy-agent-tuning.service
  tuning_path=/etc/systemd/system/proxy-agent-tuning.path
  # The panel's web-UI update bridge: a path unit arms a root oneshot unit that runs the
  # installed updater. Both are systemd definitions, like the panel itself, so the OpenRC
  # branch below has no counterpart to name.
  panel_update_service=/etc/systemd/system/proxy-panel-update.service
  panel_update_path=/etc/systemd/system/proxy-panel-update.path
  panel_update_helper=/usr/local/lib/proxycontrol/panel-update-request
  recovery_service=/etc/systemd/system/proxycontrol-update-recovery.service
  init_directories=(/etc/systemd /etc/systemd/system)
else
  unit=/etc/init.d/$name
  host_network_service=/etc/init.d/proxy-agent-host-network
  host_network_path=/usr/local/lib/proxy-agent/host-network-watch
  tuning_service=/etc/init.d/proxy-agent-tuning
  tuning_path=/usr/local/lib/proxy-agent/tuning-watch
  panel_update_service=''
  panel_update_path=''
  panel_update_helper=''
  recovery_service=/etc/init.d/proxycontrol-update-recovery
  init_directories=(/etc/init.d)
fi
required_tools=(sha256sum stat flock sed install rm)
if [[ $init_system == systemd ]]; then
  required_tools+=(systemctl)
else
  required_tools+=(rc-service rc-update)
fi
for tool in "${required_tools[@]}"; do command -v "$tool" >/dev/null || fail "Missing command: $tool"; done
for path in /usr/local /usr/local/bin /usr/local/lib /usr/local/lib/proxy-agent /usr/local/lib/proxycontrol /var/lib "$registry" /run/lock "${init_directories[@]}"; do [[ ! -L $path ]] || fail "Symlinked destination: $path"; done
[[ -d $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail 'No protected installation registry.'
[[ ! -L $registry/operation.lock ]] || fail 'Symlinked installation lock.'
[[ -f $record && ! -L $record && $(stat -c '%u:%a' "$record") == 0:600 ]] || fail 'Missing protected installation record.'
[[ ! -e $removed && ! -L $removed ]] || fail 'Uninstall was already recorded; inspect retained files.'
qui_record_cores "$record" "$component" || fail 'Installation is incomplete or unknown.'
# Line 10 names the init system the service definitions were written for; a
# record written before that line existed is a systemd record. Removing with the
# tooling of the other init system would leave a service behind that still points
# at a deleted program, so the mismatch is refused.
recorded_init=$(sed -n '10p' "$record")
[[ -n $recorded_init ]] || recorded_init=systemd
[[ $recorded_init == "$init_system" ]] \
  || fail "The installation was made for $recorded_init but this host runs $init_system; use the uninstall.sh of the other init system."
binary_digest=$(sed -n '3p' "$record")
unit_digest=$(sed -n '4p' "$record")
# The enforced-source units are recorded after the completion marker. An
# installation made before that feature has no such lines, and its units are
# then left untouched.
host_network_service_digest=$(sed -n '8p' "$record")
host_network_path_digest=$(sed -n '9p' "$record")
host_network_units=false
if [[ $component == agent && -n $host_network_service_digest && -n $host_network_path_digest ]]; then
  host_network_units=true
fi
verify_owned() {
  local file=$1 expected=$2 actual
  [[ -f $file && ! -L $file && $(stat -c '%u' "$file") == 0 && $expected =~ ^[0-9a-f]{64}$ ]] || fail "Unrecognized owned file: $file"
  actual=$(sha256sum -- "$file"); actual=${actual%% *}
  [[ $actual == "$expected" ]] || fail "Modified file retained: $file"
}
verify_owned "$target" "$binary_digest"
verify_owned "$unit" "$unit_digest"
if $host_network_units; then
  verify_owned "$host_network_service" "$host_network_service_digest"
  verify_owned "$host_network_path" "$host_network_path_digest"
fi
tuning_units=false
if [[ $component == agent && -n $(sed -n '11p' "$record") && -n $(sed -n '12p' "$record") ]]; then
  tuning_units=true
  verify_owned "$tuning_service" "$(sed -n '11p' "$record")"
  verify_owned "$tuning_path" "$(sed -n '12p' "$record")"
fi
for i in "${!record_cores[@]}"; do
  verify_owned "/usr/local/lib/proxy-agent/${record_cores[i]}" "${record_digests[i]}"
done
# A failed stop is not a clean handoff: never delete the program underneath it.
if [[ $init_system == systemd ]]; then
  systemctl stop "$name.service"
  [[ $(systemctl show "$name.service" -p ActiveState --value) == inactive ]] || fail 'Service did not stop cleanly.'
  [[ $(systemctl show "$name.service" -p Result --value) == success ]] || fail 'Service stop failed; preserve programs and inspect accounting/recovery state.'
  systemctl disable "$name.service"
else
  # OpenRC has neither ActiveState nor Result: the runscript's own status is the
  # only verdict, and it has to read "stopped" before anything is deleted. A stop
  # of an already stopped service is an rc warning, not an error, so its exit
  # status is not the verdict - the state check below is.
  rc-service "$name" stop || true
  [[ $(service_state "$name") == stopped ]] || fail 'Service did not stop cleanly; preserve programs and inspect accounting/recovery state.'
  # Leaving the default runlevel is idempotent: rc-update reports an error for a
  # service that is not in it, and that is not a failed uninstall.
  rc-update del "$name" default 2>/dev/null || true
fi
if $host_network_units; then
  if [[ $init_system == systemd ]]; then
    # The path unit may be armed and the oneshot unit is only run by it, so the
    # watcher is stopped first. A unit that is already inactive or was removed by
    # hand is not an error: the files and the table are what this script owns.
    systemctl disable --now "${host_network_path##*/}" || true
    systemctl disable "${host_network_service##*/}" || true
  else
    # The watcher is one supervised service here; it is stopped before its file
    # goes away for the same reason, and a service that is already stopped or not
    # in the runlevel is not an error.
    rc-service proxy-agent-host-network stop || true
    rc-update del proxy-agent-host-network default || true
  fi
fi
if $tuning_units; then
  if [[ $init_system == systemd ]]; then
    systemctl disable --now proxy-agent-tuning.path
    systemctl stop proxy-agent-tuning.service
    systemctl disable proxy-agent-tuning.service
  else
    service_stop proxy-agent-tuning
    service_disable proxy-agent-tuning
  fi
  # A clear request restores the root-owned baseline before removing anything.
  # A restore failure deliberately stops uninstall and preserves the binary.
  tuning_clear=$(mktemp -d "$registry/tuning-clear.XXXXXXXX")
  printf '{"request_id":"uninstall-clear","settings":{},"clear":true}\n' > "$tuning_clear/desired.json"
  if ! "$target" --apply-tuning "$tuning_clear"; then
    fail "Tuning restore failed; see $tuning_clear/status.json."
  fi
  # Atomic rename, without opening an Agent-controlled file as root.
  [[ -d /var/lib/proxy-agent/tuning && ! -L /var/lib/proxy-agent/tuning ]] || fail 'Unsafe tuning directory.'
  mv -T -- "$tuning_clear/desired.json" /var/lib/proxy-agent/tuning/desired.json
  mv -T -- "$tuning_clear/status.json" /var/lib/proxy-agent/tuning/status.json
  rmdir -- "$tuning_clear"
  rm -- "$tuning_service" "$tuning_path"
fi
# The panel's update bridge has no digest either: it is a packaged script and two units
# published like the terminal menu, and nothing is left for them to run once the panel is
# gone. The watcher is disarmed before anything is removed, so no request that the still
# running panel wrote can arm the oneshot unit mid-uninstall. A request directory that is
# not there, a unit that was never enabled and a unit removed by hand are all not a failed
# uninstall, which is why the disable calls tolerate an error.
if [[ $component == panel && -n $panel_update_helper ]]; then
  systemctl disable --now "${panel_update_path##*/}" 2>/dev/null || true
  systemctl stop "${panel_update_service##*/}" 2>/dev/null || true
  systemctl disable "${panel_update_service##*/}" 2>/dev/null || true
  rm -f -- "$panel_update_service" "$panel_update_path" "$panel_update_helper"
fi
install -o root -g root -m 0600 -- "$record" "$removed"
rm -- "$unit" "$target"
# /usr/local/bin/qui is not part of the recorded manifest, so it has no digest to
# verify; it is shared by both components.
# /usr/local/bin/qui-update and /usr/local/lib/proxycontrol/service.sh have no
# digest either and are shared by both components, so they stay while the other
# component is still installed and its scripts still source the library.
other=panel
if [[ $component == panel ]]; then other=agent; fi
if ! $purge && [[ ! -e $registry/$other.manifest && ! -L $registry/$other.manifest ]]; then
  rm -f -- "$qui" /usr/local/lib/proxycontrol/uninstall.sh
  rm -f -- "$update"
  rm -f -- "$library"
  # The boot-time hook and its service definition have nothing to recover once
  # the updater that writes the journals is gone, so they go with it. A host
  # installed before the hook existed has neither, and disabling a service that
  # was never enabled is not a failed uninstall.
  service_disable proxycontrol-update-recovery || true
  rm -f -- "$recovery_service" "$recovery"
  # rmdir refuses a directory that still holds anything else, which is the only
  # case in which it should stay.
  rmdir -- "${library%/*}" 2>/dev/null || true
fi
if $host_network_units; then rm -- "$host_network_service" "$host_network_path"; fi
for core in "${record_cores[@]}"; do rm -- "/usr/local/lib/proxy-agent/$core"; done
service_reload_definitions
# The enforced-source and Hysteria2 port-hop tables belong to this Agent, so
# removing them restores the host's normal routing. Either may be absent.
if [[ $component == agent ]] && command -v nft >/dev/null; then
  for table in proxy_agent_source proxy_agent_hop; do
    if nft list table inet "$table" >/dev/null 2>&1; then
      nft delete table inet "$table"
    fi
  done
fi
rm -- "$record"
# What this script deliberately leaves behind differs per component: the panel
# keeps its database and backups, the Agent its revisions, spool and enforced
# source directory. Identity, configuration and the service account stay in both.
case $component in
  panel) printf '%s removed. Identity, configuration, database, backups and service account remain.\n' "$name";;
  agent) printf '%s removed. Identity, configuration, data, revisions and service account remain.\n' "$name";;
esac
# Only the Agent holds a node identity the panel can revoke; the panel has no
# counterpart to close.
if [[ $component == agent ]]; then
  printf 'This does not revoke the node; delete it in the panel.\n'
fi
}

registry=/var/lib/proxycontrol-install
[[ -d $registry && ! -L $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail 'No protected installation registry.'
[[ ! -L $registry/operation.lock ]] || fail 'Symlinked installation lock.'
exec 9>"$registry/operation.lock"
flock -n 9 || fail 'Another installation operation is running.'
components=()
if [[ $requested == all ]]; then
  for component in agent panel; do
    if [[ -e $registry/$component.manifest || -L $registry/$component.manifest ]]; then components+=("$component"); fi
  done
else
  components+=("$requested")
fi
if ! $purge; then uninstall_component "$requested"; exit 0; fi
[[ -t 0 ]] || fail '清除操作必须在终端交互确认。'
failures=()
cleaned=()
paths=()
ports=()
for component in "${components[@]}"; do
  paths+=("/etc/proxy-$component" "/var/lib/proxy-$component" "$registry/$component.removed")
  printf '将卸载组件：%s；删除账号与同名组：proxy-%s\n' "$component" "$component"
  if [[ $component == agent ]]; then
    paths+=(/usr/local/lib/proxy-agent /etc/sysctl.d/99-proxy-tuning.conf /etc/sysctl.d/.proxy-tuning-snapshot.json)
    printf '删除 nft 表：inet proxy_agent_source、inet proxy_agent_hop\n这不会在面板上吊销节点，请到面板删除该节点。\n'
  else
    [[ ! -L /etc/proxy-panel/config.yaml ]] || fail 'Symlinked panel configuration.'
    for key in http_address grpc_address; do
      value=$(sed -n "s/^[[:space:]]*$key[[:space:]]*:[[:space:]]*//p" /etc/proxy-panel/config.yaml)
      value=${value%$'\r'}; value=${value#\"}; value=${value%\"}; value=${value#\'}; value=${value%\'}
      port=${value##*:}
      [[ $value == *:* && $port =~ ^[0-9]+$ ]] || fail "无法读取 $key 端口。"
      ports+=("$port")
      printf '删除 ufw/firewalld TCP 放行：%s/tcp\n' "$port"
    done
    printf '清除后所有节点失去管理，节点 Agent 会按最后一次成功的配置继续运行；数据库与备份会一起删除，需要保留请先下载备份或复制 backups 目录。\n'
  fi
done
shared=true
for component in agent panel; do
  if [[ -e $registry/$component.manifest && $requested != all && $requested != "$component" ]]; then shared=false; fi
done
if $shared; then
  paths+=(/usr/local/bin/qui-update /usr/local/bin/qui /usr/local/lib/proxycontrol "$registry")
fi
for path in "${paths[@]}"; do
  printf '清除路径：%s ' "$path"
  if [[ -e $path && ! -L $path ]]; then du -sh -- "$path"; else printf '(不存在或为符号链接)\n'; fi
done
printf '同时删除组件程序、服务、helper 单元与记录内核心；最后停用并删除 proxycontrol-update-recovery 单元。\n'
read -r -p '确认卸载并清除全部数据？[y/N] ' answer || answer=''
[[ $answer == y ]] || { printf '已取消\n'; exit 0; }
read -r -p '请输入 purge：' answer || answer=''
[[ $answer == purge ]] || { printf '已取消\n'; exit 0; }
for path in "${paths[@]}"; do
  [[ ! -L $path ]] || fail "拒绝清除符号链接：$path"
done
# Keep errexit active inside the shared uninstall body.
trap 'printf "前置阶段失败：组件 %s，行 %s；数据清除尚未开始。已执行的程序/单元删除不会回滚。\n" "$component" "$LINENO" >&2' ERR
set -E
for component in "${components[@]}"; do uninstall_component "$component"; done
trap - ERR
remove_path() {
  local path=$1
  if [[ -L $path ]]; then failures+=("$path（符号链接）")
  elif [[ -e $path ]]; then
    if rm -rf -- "$path"; then cleaned+=("$path"); else failures+=("$path"); fi
  fi
}
# Only an active firewall is touched, the same condition panel-install.sh used to
# add the rules, and only a rule that is actually present is deleted: a missing
# rule is not a failure.
for port in "${ports[@]}"; do
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi '^Status: active'; then
    if ufw status 2>/dev/null | grep -qE "^$port/tcp[[:space:]]"; then
      ufw --force delete allow "$port/tcp" >/dev/null || failures+=("ufw $port/tcp")
    fi
  elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
    if firewall-cmd --permanent --query-port="$port/tcp" >/dev/null 2>&1; then
      firewall-cmd --permanent --remove-port="$port/tcp" >/dev/null || failures+=("firewalld $port/tcp")
      firewall-cmd --reload >/dev/null || failures+=("firewalld reload")
    fi
  fi
done
for component in "${components[@]}"; do
  account=proxy-$component
  if id "$account" >/dev/null 2>&1; then
    if pgrep -u "$account" >/dev/null; then failures+=("账号 $account（仍有进程）"); continue; fi
    if command -v userdel >/dev/null; then
      if ! userdel "$account"; then failures+=("账号 $account"); fi
    elif ! deluser "$account"; then failures+=("账号 $account"); fi
  fi
  if getent group "$account" >/dev/null; then
    if command -v groupdel >/dev/null; then
      if ! groupdel "$account"; then failures+=("组 $account"); fi
    elif ! delgroup "$account"; then failures+=("组 $account"); fi
  fi
done
if $shared; then
  if ! service_disable proxycontrol-update-recovery; then failures+=(proxycontrol-update-recovery); fi
  remove_path "$recovery_service"
  service_reload_definitions || failures+=(service-reload)
fi
for path in "${paths[@]}"; do
  [[ $path == "$registry" ]] || remove_path "$path"
done
# Release before removing the directory containing the lock.
if $shared && [[ ${#failures[@]} == 0 ]]; then
  flock -u 9
  exec 9>&-
  remove_path "$registry"
fi
printf 'journald/syslog 历史日志未删除，可按需用 journalctl --vacuum-time=... 自行处理。\n'
if [[ ${#failures[@]} != 0 ]]; then printf '清除失败：%s\n' "${failures[@]}" >&2; exit 1; fi
printf '清理清单：\n'; printf '  %s\n' "${cleaned[@]}"
