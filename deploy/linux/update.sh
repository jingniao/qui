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

fail() { printf '更新被拒绝：%s\n' "$*" >&2; exit 1; }
usage() {
  cat <<USAGE
用法：qui-update [--version X.Y.Z[-rc.N]] [--component panel|agent] [--yes] [--database FILE]
                 [--bundle FILE --sums FILE]

  --version X.Y.Z[-rc.N] 安装指定发布版本（默认：GitHub 上的最新发布）
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
  --recover DIR     内部使用：恢复一个被中断的更新（[--boot] 由开机单元调用）

脚本从 https://github.com/$repository/releases 下载归档与 SHA256SUMS，校验摘要
后调用归档里的 deploy/linux/install.sh 原地替换程序、服务定义（systemd 单元或
OpenRC 初始化脚本）、随包脚本与安装记录。配置、身份与数据不会被修改；面板更新前
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
# True when --version is older than the installed panel, which changes the messages
# after a failure.
downgrade=false
# Backup files written through a .partial name that the signal handler has to remove.
partial_files=()

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

# Copy the panel database (and its write-ahead log) to DESTINATION so that an
# interrupted copy never leaves something that looks like a usable backup: both files
# are written under a .partial name, each is synced and compared with its source, and
# only then is the log renamed first and the database second. The final names therefore
# appear complete or not at all, and a truncated pre-update copy - the one thing a
# rollback must never be given - cannot exist.
copy_database_atomic() {
  local source=$1 destination=$2 wal=$1-wal
  local partial=$2.partial wal_partial=$2-wal.partial
  partial_files+=("$partial" "$wal_partial")
  if ! install -o "$panel_account" -g "$panel_account" -m 0600 -- "$source" "$partial" \
    || ! sync -f -- "$partial" \
    || ! cmp -s -- "$source" "$partial"; then
    rm -f -- "$partial"
    printf '无法复制数据库：%s\n' "$source" >&2
    return 1
  fi
  if [[ -f $wal && ! -L $wal ]]; then
    if ! install -o "$panel_account" -g "$panel_account" -m 0600 -- "$wal" "$wal_partial" \
      || ! sync -f -- "$wal_partial" \
      || ! cmp -s -- "$wal" "$wal_partial"; then
      rm -f -- "$partial" "$wal_partial"
      printf '无法复制数据库日志：%s\n' "$wal" >&2
      return 1
    fi
    rm -f -- "$destination-wal"
    if ! mv -T -- "$wal_partial" "$destination-wal"; then
      rm -f -- "$partial"
      printf '无法写入数据库备份日志：%s-wal\n' "$destination" >&2
      return 1
    fi
  fi
  if ! mv -T -- "$partial" "$destination"; then
    printf '无法写入数据库备份：%s\n' "$destination" >&2
    return 1
  fi
  partial_files=()
  sync -f -- "${destination%/*}"
}

# Copy the panel database while the panel is stopped and print the copy's path.
# The path comes from the installed configuration; the copy keeps the service
# account as owner and 0600, so only root and the panel can read it.
backup_database() {
  local previous=$1 database='' stamp='' destination=''
  database=$(database_path)
  [[ -f $database && ! -L $database ]] || fail "找不到数据库文件：$database"
  if [[ ! -d $backups ]]; then
    install -d -o "$panel_account" -g "$panel_account" -m 0700 -- "$backups" \
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
  database=$(database_path) || return 1
  [[ -f $database && ! -L $database ]] || return 0
  if [[ ! -d $backups ]]; then
    install -d -o "$panel_account" -g "$panel_account" -m 0700 -- "$backups" || return 1
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
  local backup=$1 database=''
  database=$(database_path) || return 1
  [[ -f $database && ! -L $database ]] || return 0
  cmp -s -- "$backup" "$database" || return 0
  if [[ -f $backup-wal && ! -L $backup-wal ]]; then
    if [[ ! -f $database-wal || -L $database-wal ]]; then return 0; fi
    cmp -s -- "$backup-wal" "$database-wal" || return 0
    return 1
  fi
  if [[ -f $database-wal && ! -L $database-wal && -s $database-wal ]]; then return 0; fi
  return 1
}

# Put the database copy back after a failed panel update. The new panel may already
# have migrated the database, and migrations are not reversible, so the copy replaces
# it: the log files of the stopped new version are removed first, otherwise SQLite
# would replay the new version's pages onto the old file. Both files are replaced
# through a temporary file in their own directory and one rename, so an interrupted
# restore never leaves a half-written database behind.
restore_database() {
  local backup=$1 database='' temporary='' wal_temporary=''
  [[ -f $backup && ! -L $backup ]] || { printf '没有可恢复的数据库备份：%s\n' "$backup" >&2; return 1; }
  database=$(database_path) || return 1
  rm -f -- "$database-wal" "$database-shm"
  temporary=${database%/*}/.${database##*/}.restoring.$$
  if ! install -o "$panel_account" -g "$panel_account" -m 0600 -- "$backup" "$temporary" \
    || ! sync -f -- "$temporary" \
    || ! mv -T -- "$temporary" "$database"; then
    rm -f -- "$temporary"
    printf '无法恢复数据库：%s\n' "$database" >&2
    return 1
  fi
  sync -f -- "${database%/*}"
  if [[ -f $backup-wal && ! -L $backup-wal ]]; then
    wal_temporary=${database%/*}/.${database##*/}.restoring-wal.$$
    if ! install -o "$panel_account" -g "$panel_account" -m 0600 -- "$backup-wal" "$wal_temporary" \
      || ! sync -f -- "$wal_temporary" \
      || ! mv -T -- "$wal_temporary" "$database-wal"; then
      rm -f -- "$wal_temporary"
      printf '无法恢复数据库日志：%s-wal\n' "$backup" >&2
      return 1
    fi
    sync -f -- "${database%/*}"
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
  if qui_valid_release_version "$previous"; then
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
# the terminal menu on a panel, the enforced-source pair on an agent, and the install
# record. Defined once because the snapshot and its restore have to agree: a file
# added to install.sh's publish calls belongs here too, or a rollback leaves it at the
# new version.
component_files() {
  local component=$1
  printf '%s\n' "$program_directory/proxy-$component"
  if [[ $init_system == systemd ]]; then
    printf '%s\n' "$root/etc/systemd/system/proxy-$component.service"
  else
    printf '%s\n' "$root/etc/init.d/proxy-$component"
  fi
  printf '%s\n' "$program_directory/qui-update" "$root/usr/local/lib/proxycontrol/service.sh"
  if [[ $component == panel ]]; then
    printf '%s\n' "$program_directory/qui"
  elif [[ $init_system == systemd ]]; then
    printf '%s\n' "$root/etc/systemd/system/proxy-agent-host-network.service" \
      "$root/etc/systemd/system/proxy-agent-host-network.path"
  else
    printf '%s\n' "$root/etc/init.d/proxy-agent-host-network" \
      "$root/usr/local/lib/proxy-agent/host-network-watch"
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
# Print the value of one key=value line of a journal, or fail. The journal is read with
# a regex per line and is never sourced and never eval'd: it is written by whichever
# updater ran last, and a file that is read as root must not be executable code.
journal_value() {
  local file=$1 key=$2 line
  while IFS= read -r line || [[ -n $line ]]; do
    if [[ $line =~ ^([a-z0-9_]+)=(.*)$ ]] && [[ ${BASH_REMATCH[1]} == "$key" ]]; then
      printf '%s' "${BASH_REMATCH[2]}"
      return 0
    fi
  done < "$file"
  return 1
}

# Write the journal of the snapshot in $snapshot from the active_* globals. The whole
# file goes to a dot-prefixed temporary in the snapshot, is synced, renamed over the
# journal and the snapshot directory is synced again, so a power loss leaves either the
# old phase or the new one and never half a line.
journal_write() {
  local phase=$1 temporary=$snapshot/.journal.tmp
  {
    printf 'proxycontrol-update-journal-v1\n'
    printf 'component=%s\n' "$active_component"
    printf 'previous=%s\n' "$active_previous"
    printf 'target=%s\n' "$active_target"
    printf 'was_active=%s\n' "$active_was_active"
    printf 'backup=%s\n' "$active_backup"
    printf 'database_source=%s\n' "$active_database_source"
    printf 'phase=%s\n' "$phase"
  } > "$temporary" || { rm -f -- "$temporary"; return 1; }
  if ! chmod 0600 -- "$temporary" \
    || ! sync -f -- "$temporary" \
    || ! mv -T -- "$temporary" "$snapshot/journal"; then
    rm -f -- "$temporary"
    return 1
  fi
  sync -f -- "$snapshot"
}

# Read the journal of FILE into the journal_* locals of the caller (bash resolves those
# dynamically) and validate every field the recovery relies on. A field that is missing,
# a component or phase this version does not know, and a backup that is not a regular
# file directly inside $backups are all a refusal: guessing here decides which files and
# which database a host ends up with.
journal_read() {
  local file=$1
  journal_component='' journal_previous='' journal_target='' journal_phase=''
  journal_was_active='' journal_backup='' journal_database_source=''
  [[ -f $file && ! -L $file ]] || return 1
  [[ $(sed -n '1p' -- "$file") == proxycontrol-update-journal-v1 ]] || return 1
  journal_component=$(journal_value "$file" component) || return 1
  case $journal_component in agent|panel) ;; *) return 1 ;; esac
  journal_previous=$(journal_value "$file" previous) || journal_previous='未知'
  journal_target=$(journal_value "$file" target) || journal_target='未知'
  journal_was_active=$(journal_value "$file" was_active) || return 1
  case $journal_was_active in true|false) ;; *) return 1 ;; esac
  journal_backup=$(journal_value "$file" backup) || journal_backup=''
  journal_database_source=$(journal_value "$file" database_source) || journal_database_source=''
  journal_phase=$(journal_value "$file" phase) || return 1
  case $journal_phase in prepared|started|committed|restored) ;; *) return 1 ;; esac
  if [[ -n $journal_backup ]]; then
    [[ $journal_backup == "$backups"/* ]] || return 1
    [[ -f $journal_backup && ! -L $journal_backup ]] || return 1
  fi
  return 0
}

# Remove a finished snapshot in one step that an interrupted rm -rf cannot confuse: the
# directory is renamed inside the registry and the registry is synced first, so the
# journal stops answering to a rollback.* name before anything is deleted, and a boot
# that happens in the middle of the deletion finds a discard.* directory that the next
# run removes instead of a half-removed rollback that looks like a pending update.
dispose_snapshot() {
  local discard=''
  [[ -n $snapshot ]] || return 0
  if [[ ! -d $snapshot ]]; then snapshot=''; return 0; fi
  discard=$registry/discard.${snapshot##*/rollback.}
  if ! mv -T -- "$snapshot" "$discard"; then
    printf '无法移除快照 %s；下次运行会再试。\n' "$snapshot" >&2
    snapshot=''
    return 1
  fi
  snapshot=''
  sync -f -- "$registry" || true
  rm -rf -- "$discard"
  return 0
}

# Delete the .partial copies a backup or a preserve left behind. backup_database runs
# inside a command substitution, so the array that call fills is not visible here; the
# glob is what a signal handler can rely on, and the update lock makes sure no other run
# is writing one at the same time.
remove_database_partials() {
  local file=''
  for file in "${partial_files[@]}"; do rm -f -- "$file"; done
  partial_files=()
  for file in "$backups"/*.partial; do
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
recover_snapshot() {
  local directory_arg=$1 mode=$2 name='' component='' previous='' target='' was_active=false
  local backup='' phase='' preserved='' restore=false
  local journal_component='' journal_previous='' journal_target='' journal_phase=''
  local journal_was_active='' journal_backup='' journal_database_source=''
  if [[ ! -d $directory_arg || -L $directory_arg ]]; then
    printf '不是更新快照目录：%s\n' "$directory_arg" >&2
    return 1
  fi
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
  name=proxy-$component
  case $phase in
    committed)
      dispose_snapshot
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
      dispose_snapshot
      printf '[%s] 已恢复到 %s。\n' "$name" "$previous"
      return 0
      ;;
  esac
  if [[ $mode != boot ]]; then
    # Not conditional on service_is_active for the reason the old rollback always was: a
    # unit waiting for its restart is "activating", and that is the state a failed update
    # usually leaves behind. A refusal is reported, not fatal: the files go back anyway.
    service_stop "$name" || printf '[%s] 停止 %s 失败。\n' "$name" "$name"
  fi
  if ! restore_snapshot "$component"; then
    printf '[%s] 无法恢复更新前的文件：快照保留在 %s。\n' "$name" "$directory_arg"
    return 1
  fi
  remove_install_staging
  if [[ $component == panel && -n $backup ]]; then
    case $phase in
      started) restore=true ;;
      prepared) if database_differs "$backup"; then restore=true; fi ;;
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
  if ! journal_write restored; then
    printf '[%s] 无法记录恢复进度；下次运行会重新恢复（重复恢复是安全的）。\n' "$name"
  fi
  if [[ $mode == boot ]]; then
    dispose_snapshot
    printf '[%s] 已恢复到 %s（开机时完成被中断的更新回滚）。\n' "$name" "$previous"
    return 0
  fi
  if ! $was_active; then
    dispose_snapshot
    printf '[%s] 已恢复到 %s；服务原本未运行，未启动。\n' "$name" "$previous"
    printf '[%s] 服务状态：%s\n' "$name" "$(service_state "$name" || true)"
    return 0
  fi
  if service_start "$name" && service_healthy "$name" 10; then
    service_stopped=false
    dispose_snapshot
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
  printf '\n[%s] %s：回滚到 %s ...\n' "$name" "$reason" "$previous"
  recover_snapshot "$snapshot" interactive
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
  [[ -d $registry && ! -L $registry ]] || fail "缺少安装登记目录：$registry"
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
  local file=$1 binary=$source_dir/bin/proxy-panel output='' status=0 checker=''
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
  if output=$("$checker" --check-database "$file" 2>&1); then status=0; else status=$?; fi
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

# Stop the component, back up the panel database while it is stopped, snapshot the files
# install.sh may replace, run the bundled install.sh, then start the service again when
# it was active and check that the new version stays up for ten seconds. Every failure
# after the snapshot is taken ends in rollback_component, so an update either leaves the
# new version running or leaves the host as it was. The service name carries no .service
# suffix: the init-system library adds it on systemd.
update_component() {
  local component=$1 name=proxy-$1 mode=--panel
  local binary=$source_dir/bin/proxy-$1 digest='' previous='' backup='' installed=''
  local was_active=false status=0
  [[ $component == agent ]] && mode=--node
  previous=$(installed_version "$component")
  if service_is_active "$name"; then was_active=true; fi
  # The globals the journal and the signal handler read are filled before the service
  # stops, so a signal from here on finds a complete picture of what is being replaced.
  active_component=$component
  active_previous=$previous
  active_target=$version
  active_was_active=$was_active
  active_backup=''
  active_database_source=''
  if [[ $component == panel && -n $database_file ]]; then active_database_source=$database_file; fi
  printf '\n[%s] 停止 %s ...\n' "$name" "$name"
  # Set before the stop: bash runs a trapped signal only after the foreground command,
  # so a Ctrl-C during systemctl/rc-service stop is handled the moment the stop returns,
  # and the handler has to know the service may be down. Starting one that still runs
  # is harmless.
  service_stopped=true
  service_stop "$name" || fail "无法停止 $name，未做任何修改。"
  if [[ $component == panel ]]; then
    # backup_database runs in a command substitution, so a fail inside it only ends that
    # subshell; this status check is what keeps a failed backup from leaving the panel
    # stopped.
    if ! backup=$(backup_database "$previous"); then
      remove_database_partials
      restore_service_after_stop
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
      preflight_database "$backup" || status=$?
      case $status in
        0) ;;
        2) printf '[%s] 目标版本 %s 不支持数据库检查，跳过预检。\n' "$name" "$version";;
        3)
          # Nothing was replaced and the database is the one the copy was taken from, so
          # the copy is removed again instead of piling up with every refused attempt.
          rm -f -- "$backup" "$backup-wal"
          restore_service_after_stop
          downgrade_guidance "$version"
          exit 1;;
        *)
          restore_service_after_stop
          fail "[$name] 数据库无法用于这次更新，更新没有开始（备份保留在 $backup）。";;
      esac
    fi
  fi
  if ! take_snapshot "$component"; then
    # Nothing has been replaced yet, so the host only needs its service back.
    restore_service_after_stop
    fail "[$name] 无法保存回滚快照，更新没有开始。"
  fi
  printf '[%s] 回滚快照：%s\n' "$name" "$snapshot"
  if ! journal_write prepared; then
    # Every later phase would fail the same way, so the update must not start; the
    # snapshot holds nothing install.sh replaced and is removed again.
    dispose_snapshot || true
    restore_service_after_stop
    fail "[$name] 无法写入回滚快照的进度日志，更新没有开始。"
  fi
  digest=$(sha256sum -- "$binary"); digest=${digest%% *}
  if ! "$source_dir/deploy/linux/install.sh" "$mode" --binary "$binary" --sha256 "$digest"; then
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
      if ! restore_database "$database_file" '已换用 --database 指定的数据库'; then
        rollback_component "$component" "$previous" "$was_active" \
          '无法写入 --database 指定的数据库' \
          || report_failure "$name" "$previous" "$backup"
        exit 1
      fi
    fi
    # The update of this component is complete: record that, then remove the snapshot. A
    # snapshot left at an unfinished phase would make the next run roll a working update
    # back, so it is removed even when the record cannot be written.
    if ! journal_write committed; then
      printf '[%s] 无法把更新记录为完成。\n' "$name"
    fi
    dispose_snapshot || true
    printf '[%s] 已更新到 %s；服务原本未运行，未启动。\n' "$name" "$version"
    active_component=''
    return 0
  fi
  if ! journal_write started; then
    rollback_component "$component" "$previous" "$was_active" '无法记录更新进度' \
      || report_failure "$name" "$previous" "$backup"
    exit 1
  fi
  if [[ $component == panel && -n $database_file ]]; then
    if ! restore_database "$database_file" '已换用 --database 指定的数据库'; then
      rollback_component "$component" "$previous" "$was_active" \
        '无法写入 --database 指定的数据库' \
        || report_failure "$name" "$previous" "$backup"
      exit 1
    fi
  fi
  printf '[%s] 启动 %s 并检查十秒内是否重启 ...\n' "$name" "$name"
  if service_start "$name" && service_healthy "$name" 10; then
    service_stopped=false
    if ! journal_write committed; then
      printf '[%s] 无法把更新记录为完成。\n' "$name"
    fi
    dispose_snapshot || true
    printf '[%s] 已更新到 %s，服务运行正常（10 秒内无重启）。\n' "$name" "$version"
    active_component=''
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
  if [[ $signal == HUP ]]; then
    if [[ ! -e $log ]]; then
      (set -o noclobber; : > "$log") 2>/dev/null || true
      chmod 0600 -- "$log" 2>/dev/null || true
    fi
    if [[ -f $log && ! -L $log ]]; then exec >>"$log" 2>&1 || true; fi
  fi
  printf '\n更新被中断（%s）...\n' "$signal"
  # The private copy preflight_database runs, if the signal came during the check.
  rm -f -- "$registry"/.check-panel.* 2>/dev/null
  if [[ -n $snapshot && -f $snapshot/journal ]]; then
    # A snapshot with a journal knows what was already replaced, so the one recovery
    # routine decides how much of the update has to be undone.
    recover_snapshot "$snapshot" signal
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
recover_interrupted_updates() {
  local pending='' updater=''
  local journal_component='' journal_previous='' journal_target='' journal_phase=''
  local journal_was_active='' journal_backup='' journal_database_source=''
  for pending in "$registry"/discard.*; do
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
    # journal_read fills the journal_* locals declared above (bash resolves them
    # dynamically) and validates every field a recovery depends on.
    if ! journal_read "$pending/journal"; then
      fail "快照 $pending 的进度日志无效（请检查后手工处理）。"
    fi
    printf '发现被中断的更新：%s（%s %s → %s，阶段 %s），先恢复 ...\n' \
      "$pending" "$journal_component" "$journal_previous" "$journal_target" "$journal_phase"
    updater=$pending/updater
    if [[ ! -f $updater || -L $updater ]]; then
      fail "快照 $pending 没有 updater，无法恢复（请检查后手工处理）。"
    fi
    PROXYCONTROL_UPDATE_LOCKED=1 bash "$updater" --recover "$pending" \
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
main() {
  local assume_yes=false bundle_file='' sums_file='' arch='' base='' bundle=''
  local release='' tag='' expected='' actual='' up_to_date=true answer=''
  local components=() component tool file required service_library=''
  local selected_component=''
  local recover_directory='' boot=false status=0 installed_panel='' output=''
  local snapshot_directory='' registry_directory='' log=''
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
      --database)
        (($# >= 2)) || fail '缺少 --database 的值。'
        database_file=$2; shift 2;;
      --recover)
        (($# >= 2)) || fail '缺少 --recover 的值。'
        recover_directory=$2; shift 2;;
      --boot) boot=true; shift;;
      --yes) assume_yes=true; shift;;
      --help) usage; exit 0;;
      *) fail "未知参数：$1（用 --help 查看用法）";;
    esac
  done
  # --recover and --boot are the internal entry points of a snapshot's updater and of the
  # boot unit. They undo what an earlier run left behind and nothing else, so mixing them
  # with update options would be ambiguous.
  if [[ -n $recover_directory || $boot == true ]]; then
    [[ -n $recover_directory ]] || fail '--boot 只能与 --recover 一起使用。'
    if [[ -n $selected_component || -n $version || -n $database_file || -n $bundle_file || -n $sums_file || $assume_yes == true ]]; then
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
  required_tools=(curl tar sha256sum install mktemp uname grep sed cp cmp mkdir mv sync chmod stat date rm flock sort realpath)
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

  # One update at a time, taken before this run writes a snapshot, a service definition or
  # the database: the registry is what the next run reads to finish what an interrupted one
  # left behind, and two updaters must not write it at once. An update that an earlier run
  # did not finish is completed or undone here, before the up-to-date check below can exit
  # and before anything is downloaded.
  take_update_lock 0
  recover_interrupted_updates

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
  done
  if $up_to_date; then
    printf '已是最新版本 %s。\n' "$version"
    exit 0
  fi

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
  cleanup() { if [[ -n $directory ]]; then rm -rf -- "$directory"; fi; }
  trap cleanup EXIT
  directory=$(mktemp -d) || fail '无法创建临时目录。'
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

  # --database moves the panel down a version together with a copy from before the newer
  # one ran: a copy the target cannot open, or a target that is not older, would either
  # start a broken panel or migrate the copy forward again and make it pointless. Both
  # versions must use the supported release grammar to be ordered, which version_is_older also checks.
  if [[ -n $database_file ]]; then
    if ! version_is_older "$version" "$installed_panel"; then
      fail '--database 只用于把面板降级到更旧的版本。'
    fi
    # Nothing has stopped yet, so a copy the target cannot use is refused while the panel
    # is still up.
    status=0
    preflight_database "$database_file" || status=$?
    case $status in
      0) ;;
      2)
        # The target predates --check-database (1.0.2 and older). The installed panel is
        # newer than the target, so a copy it reads as compatible is one the older target
        # can open; every other verdict is refused with the report it printed.
        output=''
        status=0
        output=$("$program_directory/proxy-panel" --check-database "$database_file" 2>&1) || status=$?
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

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then main "$@"; fi
