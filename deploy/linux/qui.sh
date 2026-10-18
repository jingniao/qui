#!/usr/bin/env bash
# Root-only terminal menu for the panel host, installed as /usr/local/bin/qui by
# deploy/linux/install.sh --panel and started by typing "qui" in an SSH session.
#
# It is a thin wrapper around the packaged program: the Owner credential reset
# runs the panel binary offline (--set-owner-credentials), a port change edits
# only the port of the http_address line and lets the binary validate the
# candidate file before it replaces the configuration, the remaining items call
# systemctl, and the update item runs the packaged updater. Nothing else is
# offered: no shell, no log viewer, no arbitrary commands.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

fail() { printf 'qui：%s\n' "$*" >&2; exit 1; }

service=proxy-panel.service
program=/usr/local/bin/proxy-panel
config=/etc/proxy-panel/config.yaml
account=proxy-panel

# Print the raw value of the first top-level "key: value" line of a file, with a
# trailing carriage return removed. Returns 1 when the key is absent.
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

# Print the port part of an address value; returns 1 when it has none.
address_port() {
  local body=${1%$'\r'}
  body=${body#\"}; body=${body%\"}
  body=${body#\'}; body=${body%\'}
  [[ $body == *:* && ${body##*:} =~ ^[0-9]+$ ]] || return 1
  printf '%s\n' "${body##*:}"
}

# Print an address value with its port replaced. The host, the IPv6 brackets and
# the quoting of the original value are preserved:
#   0.0.0.0:443 -> 0.0.0.0:8080   "[::]:443" -> "[::]:8080   :443 -> :8080
address_with_port() {
  local value=${1%$'\r'} port=$2 body='' quote='' rest=''
  case $value in
    \"*\") quote='"'; body=${value#\"}; body=${body%\"};;
    \'*\') quote="'"; body=${value#\'}; body=${body%\'};;
    *) body=$value;;
  esac
  [[ $body == *:* && ${body##*:} =~ ^[0-9]+$ ]] || return 1
  rest=${body%:*}
  printf '%s%s:%s%s\n' "$quote" "$rest" "$port" "$quote"
}

# Print a configuration with the port of its http_address line replaced; every
# other line is copied unchanged, including its line ending. Returns 1 when
# there is no usable http_address line.
config_with_port() {
  local file=$1 port=$2 line='' value='' replaced='' ending='' found=false
  while IFS= read -r line || [[ -n $line ]]; do
    if [[ $line =~ ^([[:space:]]*http_address[[:space:]]*:[[:space:]]*)(.*)$ ]]; then
      value=${BASH_REMATCH[2]}
      if replaced=$(address_with_port "$value" "$port"); then
        ending=''
        case $value in *$'\r') ending=$'\r';; esac
        printf '%s%s%s\n' "${BASH_REMATCH[1]}" "$replaced" "$ending"
        found=true
        continue
      fi
    fi
    printf '%s\n' "$line"
  done < "$file"
  $found || return 1
}

service_active() { systemctl is-active --quiet "$service"; }
service_state() { systemctl is-active "$service" 2>/dev/null || true; }

# 1. Replace the Owner credentials. The panel must be stopped for the reset, so
# the service is stopped briefly and started again afterwards when it was active
# before. The two lines go in over a pipe: the password never reaches argv, is
# never echoed and is dropped from the shell once the command returned.
reset_owner_credentials() {
  local username='' password='' confirmation='' answer='' status=0 was_active=false
  printf '修改管理员账号密码：面板会短暂停止，重置后所有 Owner 会话立即失效。\n'
  printf '新用户名（留空保持不变）: '
  read -r username || username=''
  printf '新密码（输入时不回显）: '
  IFS= read -r -s password || password=''
  printf '\n再次输入新密码: '
  IFS= read -r -s confirmation || confirmation=''
  printf '\n'
  if [[ -z $password ]]; then printf '密码不能为空，未做任何修改。\n'; return 0; fi
  if [[ $password != "$confirmation" ]]; then printf '两次输入的密码不一致，未做任何修改。\n'; return 0; fi
  unset confirmation
  printf '继续将停止 %s，然后以服务账号运行 --set-owner-credentials。继续？[y/N] ' "$service"
  read -r answer || answer=''
  if [[ $answer != y && $answer != Y ]]; then printf '已取消，未做任何修改。\n'; unset password; return 0; fi
  if service_active; then was_active=true; fi
  if ! systemctl stop "$service"; then
    printf '无法停止 %s，未修改凭据。\n' "$service"
    if $was_active; then systemctl start "$service" || true; fi
    unset password
    return 0
  fi
  # Run as the service account so the database, its WAL files and the panel lock
  # keep their owner. The password policy is enforced by the program, and its
  # error message is shown as the program prints it.
  printf '%s\n%s\n' "$username" "$password" | runuser -u "$account" -- "$program" \
    --config "$config" --set-owner-credentials || status=$?
  unset password username
  if $was_active; then
    systemctl start "$service" || printf '启动 %s 失败；查看日志：journalctl -u %s -n 50\n' "$service" "$service"
  fi
  if ((status == 0)); then
    printf '管理员凭据已更新，所有 Owner 会话已注销。\n'
  else
    printf '管理员凭据更新失败（退出码 %d）。\n' "$status"
  fi
}

# 2. Change the web port only. The candidate file is written next to the
# configuration, validated by the program, and only then moved over the original
# with the original owner and mode; the previous configuration is kept as
# config.yaml.bak. grpc_address is never touched.
change_port() {
  local current='' port='' grpc='' directory='' temporary='' owner_uid='' owner_gid='' check_output='' answer=''
  current=$(read_value "$config" http_address) || { printf '配置里找不到 http_address。\n'; return 0; }
  printf '当前 http_address: %s\n' "$current"
  printf '新端口（1-65535）: '
  read -r port || port=''
  if [[ ! $port =~ ^[1-9][0-9]{0,4}$ ]] || ((10#$port > 65535)); then
    printf '端口必须是 1-65535 的整数，未做任何修改。\n'; return 0
  fi
  if [[ $port == "$(address_port "$current" || true)" ]]; then
    printf '新端口与当前端口相同，未做任何修改。\n'; return 0
  fi
  grpc=$(read_value "$config" grpc_address || true)
  if [[ -n $grpc && $port == "$(address_port "$grpc" || true)" ]]; then
    printf '注意：该端口与 grpc_address 的端口相同，节点连接会受影响。\n'
  fi
  owner_uid=$(stat -c '%u' "$config")
  owner_gid=$(stat -c '%g' "$config")
  directory=${config%/*}
  temporary=$(mktemp --tmpdir="$directory" .config.yaml.XXXXXXXX) || {
    printf '无法在 %s 创建临时文件，未做任何修改。\n' "$directory"; return 0
  }
  if ! config_with_port "$config" "$port" > "$temporary"; then
    rm -f -- "$temporary"
    printf '配置里的 http_address 无法解析，未做任何修改。\n'; return 0
  fi
  # The candidate keeps the owner of the installed configuration, so the service
  # account can read it for the check below.
  chown "$owner_uid:$owner_gid" -- "$temporary"
  chmod 0600 -- "$temporary"
  if ! check_output=$(runuser -u "$account" -- "$program" --config "$temporary" --check-config 2>&1); then
    printf '新配置未通过校验，保留原配置：\n%s\n' "$check_output"
    rm -f -- "$temporary"
    return 0
  fi
  if ! install -o "$owner_uid" -g "$owner_gid" -m 0600 -- "$config" "$config.bak"; then
    rm -f -- "$temporary"
    printf '无法创建备份 %s.bak，未做任何修改。\n' "$config"; return 0
  fi
  mv -T -- "$temporary" "$config"
  printf 'http_address 已改为 %s；原配置备份在 %s.bak。\n' "$(read_value "$config" http_address || true)" "$config"
  printf '请在防火墙放行新端口；订阅链接里含旧端口的用户需要刷新订阅。\n'
  printf '现在重启 %s？[y/N] ' "$service"
  read -r answer || answer=''
  if [[ $answer == y || $answer == Y ]]; then
    restart_panel
  else
    printf '未重启；新端口在下次启动或重启时生效。\n'
  fi
}

# 3. Restart.
restart_panel() {
  if systemctl restart "$service"; then
    printf '%s 已重启。\n' "$service"
  else
    printf '重启 %s 失败；查看日志：journalctl -u %s -n 50\n' "$service" "$service"
  fi
  printf '服务状态：%s\n' "$(service_state)"
}

# 4. Start.
start_panel() {
  if service_active; then printf '%s 已在运行。\n' "$service"; return 0; fi
  if systemctl start "$service"; then
    printf '%s 已启动。\n' "$service"
  else
    printf '启动 %s 失败；查看日志：journalctl -u %s -n 50\n' "$service" "$service"
  fi
  printf '服务状态：%s\n' "$(service_state)"
}

# 5. Stop.
stop_panel() {
  if ! service_active; then printf '%s 已处于停止状态。\n' "$service"; return 0; fi
  if systemctl stop "$service"; then
    printf '%s 已停止。\n' "$service"
  else
    printf '停止 %s 失败；查看日志：journalctl -u %s -n 50\n' "$service" "$service"
  fi
  printf '服务状态：%s\n' "$(service_state)"
}

# 6. Update from GitHub. The packaged updater owns the whole flow, including the
# version choice, the confirmation prompt and the database backup it makes while
# the panel is stopped.
update_panel() {
  local updater=/usr/local/bin/qui-update
  [[ -x $updater && ! -L $updater ]] || fail "缺少更新程序：$updater"
  "$updater"
}

print_menu() {
  printf '\033[1m%s\033[0m\n' '代理控制平台 终端菜单'
  printf '服务状态：%s\n' "$(service_state)"
  printf '当前程序版本：%s\n' "$("$program" --version 2>/dev/null || printf '未知')"
  printf '当前 Web 端口：%s\n' "$(read_value "$config" http_address || printf '未知')"
  printf '\n1. 修改管理员账号密码\n'
  printf '2. 修改端口\n'
  printf '3. 重启\n'
  printf '4. 启动\n'
  printf '5. 停止\n'
  printf '6. 更新（从 GitHub）\n'
  printf '0. 退出\n\n'
}

main() {
  local choice=''
  while true; do
    print_menu
    printf '请选择：'
    if ! read -r choice; then printf '\n'; return 0; fi
    case $choice in
      1) reset_owner_credentials;;
      2) change_port;;
      3) restart_panel;;
      4) start_panel;;
      5) stop_panel;;
      6) update_panel;;
      0) printf '再见。\n'; return 0;;
      *) printf '无效选择：%s\n' "$choice"; continue;;
    esac
    printf '\n按 Enter 返回菜单。'
    read -r _ || true
    printf '\n'
  done
}

[[ $(uname -s) == Linux && $EUID -eq 0 ]] || fail '请以 root 身份在 Linux 面板主机上运行。'
for tool in systemctl runuser stat mktemp chown chmod install mv rm getent; do
  command -v "$tool" >/dev/null || fail "缺少命令：$tool"
done
[[ -x $program && ! -L $program ]] || fail "缺少程序：$program"
[[ -f $config && ! -L $config ]] || fail "缺少配置文件：$config"
getent passwd "$account" >/dev/null || fail "缺少服务账号：$account"

main "$@"
