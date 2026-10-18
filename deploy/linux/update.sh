#!/usr/bin/env bash
# Update the panel and/or the node agent in place from a GitHub release,
# installed as /usr/local/bin/qui-update by deploy/linux/install.sh.
#
# Trust in the archive rests on the GitHub repository: SHA256SUMS is published in
# the same release as the archives, so its digest only proves that the download
# matches what the repository published. The script replaces only the program,
# the systemd units and the packaged scripts, and it does so by running the
# deploy/linux/install.sh of the downloaded release; configuration, node
# identity and every database are left untouched. A panel update copies the
# database into /var/lib/proxy-panel/backups/ while the service is stopped,
# because database migrations are not reversible and a downgrade needs that copy.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

repository=jingniao/qui

fail() { printf '更新被拒绝：%s\n' "$*" >&2; exit 1; }
usage() {
  cat <<USAGE
用法：qui-update [--version X.Y.Z] [--yes]

  --version X.Y.Z   安装指定发布版本（默认：GitHub 上的最新发布）
  --yes             不询问确认，直接更新

脚本从 https://github.com/$repository/releases 下载归档与 SHA256SUMS，校验
摘要后调用归档里的 deploy/linux/install.sh 原地替换程序与 systemd 单元。
配置、身份与数据不会被修改；面板更新前会在服务停止时把数据库备份到
/var/lib/proxy-panel/backups/。同一台主机同时装有面板与节点时，两个组件都
从同一个归档更新（先节点，后面板）。
USAGE
}

version=''
assume_yes=false
while (($#)); do
  case $1 in
    --version)
      (($# >= 2)) || fail '缺少 --version 的值。'
      version=$2; shift 2;;
    --yes) assume_yes=true; shift;;
    --help) usage; exit 0;;
    *) fail "未知参数：$1（用 --help 查看用法）";;
  esac
done
[[ -z $version || $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail '版本号必须是 X.Y.Z 形式。'

[[ $(uname -s) == Linux && $EUID -eq 0 ]] || fail '请以 root 身份在 Linux 上运行。'
# date comes from the same coreutils package as install and sha256sum.
for tool in systemctl curl tar sha256sum install mktemp uname grep sed; do
  command -v "$tool" >/dev/null || fail "缺少命令：$tool"
done

registry=/var/lib/proxycontrol-install
backups=/var/lib/proxy-panel/backups
panel_config=/etc/proxy-panel/config.yaml
program_directory=/usr/local/bin

# The install record is the fixed seven-line file install.sh writes: line 1 names
# the format, line 2 the component and line 7 the completion marker.
record_complete() {
  local component=$1 record
  record=$registry/$component.manifest
  [[ -f $record && ! -L $record ]] || return 1
  [[ $(sed -n '1p' "$record") == proxycontrol-install-v1 ]] || return 1
  [[ $(sed -n '2p' "$record") == "$component" ]] || return 1
  [[ $(sed -n '7p' "$record") == installed ]] || return 1
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

download() {
  curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 5 --output "$2" -- "$1" \
    || fail "下载失败：$1"
}

# Copy the panel database while the panel is stopped and print the copy's path.
# The path comes from the installed configuration; the copy keeps the service
# account as owner and 0600, so only root and the panel can read it.
backup_database() {
  local previous=$1 database='' stamp='' destination=''
  [[ -f $panel_config && ! -L $panel_config ]] || fail "缺少配置文件：$panel_config"
  database=$(read_value "$panel_config" database) || fail "配置里找不到 database：$panel_config"
  if [[ ${#database} -ge 2 ]]; then
    case $database in
      \"*\"|\'*\') database=${database:1:-1};;
    esac
  fi
  [[ $database == /* ]] || fail '配置里的 database 必须是绝对路径。'
  [[ -f $database && ! -L $database ]] || fail "找不到数据库文件：$database"
  if [[ ! -d $backups ]]; then install -d -o proxy-panel -g proxy-panel -m 0700 "$backups"; fi
  stamp=$(date +%Y%m%d-%H%M%S)
  destination=$backups/pre-update-$previous-$stamp.db
  install -o proxy-panel -g proxy-panel -m 0600 -- "$database" "$destination"
  # A write-ahead log left behind by the stopped service is copied next to the
  # database, where SQLite looks for it.
  if [[ -f $database-wal && ! -L $database-wal ]]; then
    install -o proxy-panel -g proxy-panel -m 0600 -- "$database-wal" "$destination-wal"
  fi
  printf '%s\n' "$destination"
}

# Print the log, the downgrade command and the database copy after a failure.
report_failure() {
  local service=$1 previous=$2 backup=$3
  printf '日志：journalctl -u %s -n 50\n' "$service"
  if [[ $previous =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf '降级：用 qui-update --version %s 装回旧版本。\n' "$previous"
  else
    printf '降级：用 qui-update --version X.Y.Z 装回旧版本。\n'
  fi
  if [[ -n $backup ]]; then
    printf '数据库迁移不可逆：回退前先把备份 %s 恢复到配置里的 database 路径。\n' "$backup"
  fi
}

# Stop the component, back up the panel database while it is stopped, run the
# bundled install.sh and start the service again when it was active. install.sh
# publishes the program with a rename, so a failure leaves the previous binary in
# place and the service can be started on it again.
update_component() {
  local component=$1 name=proxy-$1 service=proxy-$1.service mode=--panel
  local binary=$source_dir/bin/proxy-$1 digest='' previous='' backup='' was_active=false
  [[ $component == agent ]] && mode=--node
  previous=$(installed_version "$component")
  if systemctl is-active --quiet "$service"; then was_active=true; fi
  printf '\n[%s] 停止 %s ...\n' "$name" "$service"
  systemctl stop "$service" || fail "无法停止 $service，未做任何修改。"
  if [[ $component == panel ]]; then
    backup=$(backup_database "$previous")
    printf '[%s] 数据库已备份：%s\n' "$name" "$backup"
  fi
  digest=$(sha256sum -- "$binary"); digest=${digest%% *}
  if ! "$source_dir/deploy/linux/install.sh" "$mode" --binary "$binary" --sha256 "$digest"; then
    printf '[%s] 更新失败，程序未被替换。\n' "$name"
    if $was_active; then
      systemctl start "$service" || printf '[%s] 启动 %s 失败。\n' "$name" "$service"
      printf '[%s] 服务状态：%s\n' "$name" "$(systemctl is-active "$service" 2>/dev/null || true)"
    fi
    report_failure "$service" "$previous" "$backup"
    exit 1
  fi
  if $was_active; then
    if systemctl start "$service"; then
      printf '[%s] 已更新到 %s，服务状态：%s\n' "$name" "$version" \
        "$(systemctl is-active "$service" 2>/dev/null || true)"
    else
      printf '[%s] 已更新到 %s，但启动失败。\n' "$name" "$version"
      report_failure "$service" "$previous" "$backup"
      exit 1
    fi
  else
    printf '[%s] 已更新到 %s；服务原本未运行，未启动。\n' "$name" "$version"
  fi
}

# The agent is updated before the panel, so a panel that cannot be replaced still
# leaves a working node.
components=()
for component in agent panel; do
  if record_complete "$component"; then components+=("$component"); fi
done
[[ ${#components[@]} -gt 0 ]] || fail "这台主机上没有有效的安装记录：$registry"

if [[ -z $version ]]; then
  printf '查询 GitHub 最新发布 ...\n'
  release=$(curl -fsSL --proto '=https' --tlsv1.2 "https://api.github.com/repos/$repository/releases/latest") \
    || fail '无法查询 GitHub 最新发布；请检查网络，或用 --version 指定版本。'
  tag=$(printf '%s\n' "$release" | grep -m1 '"tag_name"' | sed -e 's/.*"tag_name"[[:space:]]*:[[:space:]]*"//' -e 's/".*//') \
    || fail 'GitHub 返回的内容里没有 tag_name。'
  [[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "最新发布的标签不是 vX.Y.Z：$tag"
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
done
if $up_to_date; then
  printf '已是最新版本 %s。\n' "$version"
  exit 0
fi

bundle=proxy-control-$version-linux-$arch
base=https://github.com/$repository/releases/download/v$version
directory=''
cleanup() { if [[ -n $directory ]]; then rm -rf -- "$directory"; fi; }
trap cleanup EXIT
directory=$(mktemp -d) || fail '无法创建临时目录。'
printf '下载 %s ...\n' "$bundle.tar.gz"
download "$base/$bundle.tar.gz" "$directory/$bundle.tar.gz"
download "$base/SHA256SUMS" "$directory/SHA256SUMS"

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

printf '\n目标版本：%s\n' "$version"
for component in "${components[@]}"; do
  printf 'proxy-%s：%s → %s\n' "$component" "$(installed_version "$component")" "$version"
done
printf '归档 SHA256 与 SHA256SUMS 一致：%s\n' "$expected"
if ! $assume_yes; then
  printf '确认更新？[y/N] '
  answer=''
  read -r answer || answer=''
  case $answer in
    y|Y) ;;
    *) printf '已取消，未做任何修改。\n'; exit 0;;
  esac
fi

for component in "${components[@]}"; do
  update_component "$component"
done

printf '\n更新完成：\n'
for component in "${components[@]}"; do
  printf 'proxy-%s %s（服务状态：%s）\n' "$component" "$(installed_version "$component")" \
    "$(systemctl is-active "proxy-$component.service" 2>/dev/null || true)"
done
