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
# deploy/linux/install.sh of the archive; configuration, node identity and every
# database are left untouched.
#
# An update is reversible. While the service is stopped and before install.sh runs,
# the files install.sh may replace are copied into a fresh root-only 0700 directory
# under /var/lib/proxycontrol-install/ (cp -a, so owner, group and mode are kept), and
# a panel update also copies the database into /var/lib/proxy-panel/backups/, because
# database migrations are not reversible. After the update the new version has to stay
# up: both init systems restart a crashed program after five seconds
# (Restart=on-failure RestartSec=5, supervise-daemon respawn_delay=5), so a crash loop
# looks active at every poll but changes its main PID, and that change is what the
# ten-second health check catches. When install.sh fails, when the installed version
# is not the requested one, or when the new version does not stay up, the service is
# stopped, the snapshot is put back and the previous version is started again; a panel
# rollback restores the pre-update database copy as well. The snapshot directory is
# removed after a successful update and after a successful rollback, and kept (with
# its path printed) when the rollback itself does not come up healthy.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

repository=jingniao/qui

fail() { printf '更新被拒绝：%s\n' "$*" >&2; exit 1; }
usage() {
  cat <<USAGE
用法：qui-update [--version X.Y.Z] [--yes] [--bundle FILE --sums FILE]

  --version X.Y.Z   安装指定发布版本（默认：GitHub 上的最新发布）
  --yes             不询问确认，直接更新
  --bundle FILE     离线更新：本地发布归档（proxy-control-X.Y.Z-linux-ARCH.tar.gz）
  --sums FILE       离线更新：与该归档配套的 SHA256SUMS
                    （两个参数必须同时给出，并同时用 --version 指定版本；此时不
                    访问 GitHub，摘要校验与下载时完全相同）

脚本从 https://github.com/$repository/releases 下载归档与 SHA256SUMS，校验摘要
后调用归档里的 deploy/linux/install.sh 原地替换程序、服务定义（systemd 单元或
OpenRC 初始化脚本）、随包脚本与安装记录。配置、身份与数据不会被修改；面板更新前
会在服务停止时把数据库备份到 /var/lib/proxy-panel/backups/。更新前会把可能被替换
的文件快照到 /var/lib/proxycontrol-install/rollback.*，新版本启动后十秒内不重启才
算成功；失败时恢复快照（面板还会恢复到更新前的数据库备份）。同一台主机同时装有
面板与节点时，两个组件都从同一个归档更新（先节点，后面板）。
USAGE
}

# Paths that do not come from the options, so they are set once here and read by every
# function below.
registry=/var/lib/proxycontrol-install
backups=/var/lib/proxy-panel/backups
panel_config=/etc/proxy-panel/config.yaml
program_directory=/usr/local/bin
# Filled by main() before the update loop runs: the version being installed, the
# extracted archive and the temporary download directory the EXIT trap removes.
version=''
directory=''
source_dir=''
# Filled by take_snapshot and read by restore_snapshot and the success paths; empty
# means no snapshot was taken, which is what keeps the EXIT trap out of it.
snapshot=''

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

# --no-insecure overrides a -k that a curl wrapper or alias may add in front of
# the arguments, so nothing is fetched without a verified certificate.
download() {
  curl --no-insecure -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 5 --output "$2" -- "$1" \
    || fail "下载失败：$1"
}

# Print the database path from the installed panel configuration, with the quotes YAML
# allows around it removed. Shared by the backup and by the rollback, so both read the
# same file the panel reads.
database_path() {
  local database=''
  [[ -f $panel_config && ! -L $panel_config ]] || fail "缺少配置文件：$panel_config"
  database=$(read_value "$panel_config" database) || fail "配置里找不到 database：$panel_config"
  if [[ ${#database} -ge 2 ]]; then
    case $database in
      \"*\"|\'*\') database=${database:1:-1};;
    esac
  fi
  [[ $database == /* ]] || fail '配置里的 database 必须是绝对路径。'
  printf '%s\n' "$database"
}

# Copy the panel database while the panel is stopped and print the copy's path.
# The path comes from the installed configuration; the copy keeps the service
# account as owner and 0600, so only root and the panel can read it.
backup_database() {
  local previous=$1 database='' stamp='' destination=''
  database=$(database_path)
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

# Put the database copy back after a failed panel update. The new panel may already
# have migrated the database, and migrations are not reversible, so the copy replaces
# it: the log files of the stopped new version are removed first, otherwise SQLite
# would replay the new version's pages onto the old file. The file itself is replaced
# through a temporary file in its own directory and one rename, so an interrupted
# restore never leaves a half-written database behind.
restore_database() {
  local backup=$1 database='' temporary=''
  [[ -f $backup && ! -L $backup ]] || { printf '没有可恢复的数据库备份：%s\n' "$backup" >&2; return 1; }
  database=$(database_path) || return 1
  rm -f -- "$database-wal" "$database-shm"
  temporary=${database%/*}/.${database##*/}.restoring.$$
  if ! install -o proxy-panel -g proxy-panel -m 0600 -- "$backup" "$temporary" \
    || ! sync -f -- "$temporary" \
    || ! mv -T -- "$temporary" "$database"; then
    rm -f -- "$temporary"
    printf '无法恢复数据库：%s\n' "$database" >&2
    return 1
  fi
  sync -f -- "${database%/*}"
  if [[ -f $backup-wal && ! -L $backup-wal ]]; then
    install -o proxy-panel -g proxy-panel -m 0600 -- "$backup-wal" "$database-wal" \
      || printf '无法恢复数据库日志：%s-wal\n' "$backup" >&2
  fi
  printf '已恢复更新前的数据库：%s\n' "$backup"
}

# Print the log hint, the downgrade command and the database copy after a failure.
report_failure() {
  local name=$1 previous=$2 backup=$3
  printf '日志：%s\n' "$(service_log_hint "$name" || true)"
  if [[ $previous =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf '降级：用 qui-update --version %s 装回旧版本。\n' "$previous"
  else
    printf '降级：用 qui-update --version X.Y.Z 装回旧版本。\n'
  fi
  if [[ -n $backup ]]; then
    printf '数据库迁移不可逆：回到旧版本时用备份 %s 覆盖配置里的 database 路径。\n' "$backup"
  fi
}

# The files the archive's install.sh replaces for a component: the program, the main
# service definition, the updater and the init-system library that every host gets,
# the terminal menu on a panel, the enforced-source pair on an agent, and the install
# record. Defined once because the snapshot and its restore have to agree: a file
# added to install.sh's publish calls belongs here too, or a rollback leaves it at the
# new version.
component_files() {
  local component=$1
  printf '%s\n' "/usr/local/bin/proxy-$component"
  if [[ $init_system == systemd ]]; then
    printf '%s\n' "/etc/systemd/system/proxy-$component.service"
  else
    printf '%s\n' "/etc/init.d/proxy-$component"
  fi
  printf '%s\n' /usr/local/bin/qui-update /usr/local/lib/proxycontrol/service.sh
  if [[ $component == panel ]]; then
    printf '%s\n' /usr/local/bin/qui
  elif [[ $init_system == systemd ]]; then
    printf '%s\n' /etc/systemd/system/proxy-agent-host-network.service \
      /etc/systemd/system/proxy-agent-host-network.path
  else
    printf '%s\n' /etc/init.d/proxy-agent-host-network \
      /usr/local/lib/proxy-agent/host-network-watch
  fi
  printf '%s\n' "$registry/$component.manifest"
}

# Copy everything component_files lists into a fresh root-only 0700 directory inside
# the 0700 registry, keeping owner, group and mode with cp -a. A file that does not
# exist yet is simply absent from the snapshot, and restore_snapshot then removes the
# one a failed install created. A snapshot that cannot be completed is removed again
# and reported as a failure: an update must not start without one.
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
}

# Put the recorded files back. Each copy goes to a temporary file in the destination's
# own directory and is renamed into place, the same publish pattern install.sh uses, so
# a file is never half-written and no temporary is left behind; a path the snapshot
# does not hold is removed, because install.sh created it and the previous version did
# not have it. The init-system definitions are read again afterwards, and on OpenRC the
# agent's watcher is restarted so the previous watcher runs instead of the one the
# failed install started.
restore_snapshot() {
  local component=$1 path source temporary
  [[ -n $snapshot && -d $snapshot ]] || { printf '没有可恢复的快照。\n' >&2; return 1; }
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
  if [[ $init_system == openrc && $component == agent ]] && service_is_active proxy-agent-host-network; then
    service_restart proxy-agent-host-network || return 1
  fi
  printf '[proxy-%s] 已恢复更新前的文件。\n' "$component"
}

# Put the component back to the version that ran before the update: stop whatever the
# new version left running, restore the snapshot, restore the panel database copy when the
# new panel had already run (and may have migrated it), start the previous version and
# check that it stays up. The snapshot is kept and its path printed when the previous
# version does not come up, because it is then the only copy of the files that were
# replaced. Returns 0 when the previous version is back (and running, if it was), 1
# otherwise; the caller prints the downgrade instructions only in the second case.
rollback_component() {
  local component=$1 previous=$2 backup=$3 was_active=$4 reason=$5 restore_database_copy=${6:-false}
  local name=proxy-$1
  printf '\n[%s] %s：回滚到 %s ...\n' "$name" "$reason" "$previous"
  # The stop is not conditional on service_is_active: a unit that crashed and waits for
  # its restart is "activating", which is not "active", and that is exactly the crash
  # loop this rollback usually runs in. Stopping a service that is already stopped is a
  # no-op on both init systems, and a refusal is reported rather than fatal, because the
  # files have to be put back either way.
  service_stop "$name" || printf '[%s] 停止 %s 失败。\n' "$name" "$name"
  if ! restore_snapshot "$component"; then
    printf '[%s] 无法恢复更新前的文件：快照保留在 %s。\n' "$name" "${snapshot:-（无）}"
    return 1
  fi
  if [[ $component == panel && -n $backup ]] && $restore_database_copy; then
    printf '[%s] 恢复更新前的数据库 ...\n' "$name"
    restore_database "$backup" || printf '[%s] 数据库没有恢复，备份仍保留在 %s。\n' "$name" "$backup"
  fi
  if ! $was_active; then
    printf '[%s] 更新失败，已恢复到 %s；服务原本未运行，未启动。\n' "$name" "$previous"
    printf '[%s] 服务状态：%s\n' "$name" "$(service_state "$name" || true)"
    rm -rf -- "$snapshot"; snapshot=''
    return 0
  fi
  if service_start "$name" && service_healthy "$name" 10; then
    printf '[%s] 更新失败，已恢复到 %s，服务运行正常（10 秒内无重启）。\n' "$name" "$previous"
    printf '[%s] 服务状态：%s\n' "$name" "$(service_state "$name" || true)"
    rm -rf -- "$snapshot"; snapshot=''
    return 0
  fi
  printf '[%s] 已恢复到 %s，但旧版本没有恢复健康。\n' "$name" "$previous"
  printf '[%s] 服务状态：%s\n' "$name" "$(service_state "$name" || true)"
  printf '[%s] 日志：%s\n' "$name" "$(service_log_hint "$name" || true)"
  printf '[%s] 快照保留在 %s。\n' "$name" "$snapshot"
  return 1
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
  local was_active=false
  [[ $component == agent ]] && mode=--node
  previous=$(installed_version "$component")
  if service_is_active "$name"; then was_active=true; fi
  printf '\n[%s] 停止 %s ...\n' "$name" "$name"
  service_stop "$name" || fail "无法停止 $name，未做任何修改。"
  if [[ $component == panel ]]; then
    backup=$(backup_database "$previous")
    printf '[%s] 数据库已备份：%s\n' "$name" "$backup"
  fi
  if ! take_snapshot "$component"; then
    # Nothing has been replaced yet, so the host only needs its service back.
    if $was_active; then
      service_start "$name" || printf '[%s] 启动 %s 失败。\n' "$name" "$name"
    fi
    fail "[$name] 无法保存回滚快照，更新没有开始。"
  fi
  printf '[%s] 回滚快照：%s\n' "$name" "$snapshot"
  digest=$(sha256sum -- "$binary"); digest=${digest%% *}
  if ! "$source_dir/deploy/linux/install.sh" "$mode" --binary "$binary" --sha256 "$digest"; then
    # The new program never ran, so the database was not migrated and only the files have
    # to be put back.
    rollback_component "$component" "$previous" "$backup" "$was_active" 'install.sh 失败' \
      || report_failure "$name" "$previous" "$backup"
    exit 1
  fi
  installed=$(installed_version "$component")
  if [[ $installed != "$version" ]]; then
    rollback_component "$component" "$previous" "$backup" "$was_active" \
      "install.sh 报告成功，但程序版本是 $installed 而不是 $version" \
      || report_failure "$name" "$previous" "$backup"
    exit 1
  fi
  if ! $was_active; then
    printf '[%s] 已更新到 %s；服务原本未运行，未启动。\n' "$name" "$version"
    rm -rf -- "$snapshot"; snapshot=''
    return 0
  fi
  printf '[%s] 启动 %s 并检查十秒内是否重启 ...\n' "$name" "$name"
  if service_start "$name" && service_healthy "$name" 10; then
    printf '[%s] 已更新到 %s，服务运行正常（10 秒内无重启）。\n' "$name" "$version"
    rm -rf -- "$snapshot"; snapshot=''
    return 0
  fi
  printf '[%s] 新版本 %s 没有持续运行。\n' "$name" "$version"
  printf '[%s] 日志：%s\n' "$name" "$(service_log_hint "$name" || true)"
  rollback_component "$component" "$previous" "$backup" "$was_active" "新版本 $version 未能持续运行" true \
    || report_failure "$name" "$previous" "$backup"
  exit 1
}

# Everything else runs here, and the call of this function is the last line of the file.
# install.sh replaces /usr/local/bin/qui-update while bash is running it, and bash reads
# a script incrementally, so a replaced file could otherwise feed it different commands
# than the ones it started with. Calling a function that was fully parsed before anything
# was replaced removes that risk; the rename install.sh uses already gives the new file a
# new inode, so this is the second line of defence rather than the only one.
main() {
  local assume_yes=false bundle_file='' sums_file='' arch='' base='' bundle=''
  local release='' tag='' expected='' actual='' up_to_date=true answer=''
  local components=() component tool file required service_library=''
  while (($#)); do
    case $1 in
      --version)
        (($# >= 2)) || fail '缺少 --version 的值。'
        version=$2; shift 2;;
      --bundle|--sums)
        (($# >= 2)) || fail "缺少 $1 的值。"
        if [[ $1 == --bundle ]]; then bundle_file=$2; else sums_file=$2; fi
        shift 2;;
      --yes) assume_yes=true; shift;;
      --help) usage; exit 0;;
      *) fail "未知参数：$1（用 --help 查看用法）";;
    esac
  done
  [[ -z $version || $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail '版本号必须是 X.Y.Z 形式。'
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
  service_library=/usr/local/lib/proxycontrol/service.sh
  if [[ -f $(dirname -- "${BASH_SOURCE[0]}")/service.sh ]]; then
    service_library=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/service.sh
  fi
  [[ -f $service_library && ! -L $service_library && $(stat -c '%u' "$service_library" 2>/dev/null) == 0 ]] \
    || fail "缺少初始化系统库 $service_library：这是旧版安装。请用与安装版本相同的发布归档执行一次 deploy/linux/install.sh，再重跑本脚本。"
  # shellcheck source=service.sh
  . "$service_library"
  init_system=$(detect_init) || fail '既不是 systemd 也不是 OpenRC：不支持本机的初始化系统。'
  # date comes from the same coreutils package as install and sha256sum; cp, cmp, mkdir
  # and sync are what the snapshot and its restore use; rc-service and rc-update are the
  # OpenRC equivalents of systemctl.
  required_tools=(curl tar sha256sum install mktemp uname grep sed cp cmp mkdir mv sync chmod stat date rm)
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
    if record_complete "$component"; then components+=("$component"); fi
  done
  [[ ${#components[@]} -gt 0 ]] || fail "这台主机上没有有效的安装记录：$registry"

  # An offline update always knows its version, so only the online path asks GitHub for
  # the latest release.
  if [[ -z $version ]]; then
    printf '查询 GitHub 最新发布 ...\n'
    release=$(curl --no-insecure -fsSL --proto '=https' --tlsv1.2 "https://api.github.com/repos/$repository/releases/latest") \
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
  # Only the temporary download directory is removed by the trap: a rollback snapshot is
  # removed by the update that took it, or kept for the operator when the rollback itself
  # did not come up.
  cleanup() { if [[ -n $directory ]]; then rm -rf -- "$directory"; fi; }
  trap cleanup EXIT
  directory=$(mktemp -d) || fail '无法创建临时目录。'
  if [[ -n $bundle_file ]]; then
    # Offline update: the two local files are copied under the names the download path
    # uses, so the digest check below is the same code for both and the archive is
    # verified against its SHA256SUMS either way.
    install -m 0600 -- "$bundle_file" "$directory/$bundle.tar.gz" || fail '无法复制离线归档。'
    install -m 0600 -- "$sums_file" "$directory/SHA256SUMS" || fail '无法复制离线 SHA256SUMS。'
    printf '使用离线归档 %s 与其 SHA256SUMS。\n' "${bundle_file##*/}"
  else
    printf '下载 %s ...\n' "$bundle.tar.gz"
    download "$base/$bundle.tar.gz" "$directory/$bundle.tar.gz"
    download "$base/SHA256SUMS" "$directory/SHA256SUMS"
  fi

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
      "$(service_state "proxy-$component" || true)"
  done
}

main "$@"
