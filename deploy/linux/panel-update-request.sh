#!/usr/bin/env bash
# Apply a panel update that was requested in the web UI, installed as
# /usr/local/lib/proxycontrol/panel-update-request by deploy/linux/install.sh and run
# as the ExecStart of proxy-panel-update.service, which proxy-panel-update.path arms.
#
# Why this exists: proxy-panel runs as the unprivileged account proxy-panel under
# ProtectSystem=strict and cannot replace /usr/local/bin, /etc/systemd/system or its own
# database, so it cannot update itself the way qui-update does. It writes a request file
# into its own state directory instead, and this root process runs
# /usr/local/bin/qui-update with the version from that file. Everything after that is
# the updater's own machinery: pre-update database backup, install, ten-second health
# check, and rollback to the previous version when the new one does not come up.
#
# The contract with the panel is two single-line JSON documents in
# /var/lib/proxy-panel/panel-update/:
#   request.json  {"requestId","version","requestedAt","requestedBy"}
#   status.json   {"requestId","version","previousVersion","state","code","startedAt","finishedAt"}
# state is running|succeeded|failed; code is one of running, succeeded, invalid_request,
# not_newer, update_failed and version_mismatch. status.json carries no free text and no
# log content, because the panel reads it and the updater's output is root-only: that
# output is appended to /var/lib/proxycontrol-install/panel-update.log (root 0600).
#
# The request is the only thing that decides what happens, so it is validated rather
# than trusted: it has to be a regular file owned by the panel account, at most 4096
# bytes, with a 32-character lowercase hexadecimal requestId and an official X.Y.Z
# version without an -rc suffix, and that version has to be strictly newer than the
# installed panel. No flag, path or program name is ever taken from it; the only program
# started here is the installed updater.
#
# systemd re-arms the path unit for the same file, so a status.json that already names
# the request is the answer and nothing runs again. The request file is left in place:
# removing it is the panel's job, because only the panel knows when it has read the
# status.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

# Test-only prefix for every absolute path, plus the three overrides below, so
# deploy/linux/tests/panel-update-helper-test.sh can exercise this script without a real
# installation. They are empty in production, and only root can set the environment of a
# root unit, so they are trusted the same way $PATH is.
root=${QUI_PANEL_UPDATE_TEST_ROOT:-}
request_directory=$root/var/lib/proxy-panel/panel-update
request_file=$request_directory/request.json
status_file=$request_directory/status.json
log_file=$root/var/lib/proxycontrol-install/panel-update.log
updater=${QUI_PANEL_UPDATE_UPDATER:-$root/usr/local/bin/qui-update}
panel_program=${QUI_PANEL_UPDATE_PROGRAM:-$root/usr/local/bin/proxy-panel}
account=${QUI_PANEL_UPDATE_ACCOUNT:-proxy-panel}

# Filled from the request and reported back in status.json; empty is the correct value
# while a document is unreadable.
request_id=''
request_version=''
previous_version=''
started_at=''
status_temporary=''

cleanup() {
  if [[ -n $status_temporary ]]; then rm -f -- "$status_temporary"; fi
}
trap cleanup EXIT

# Print the version the installed panel reports (the form cmd/panel/main.go prints), or
# nothing when it cannot be read. Never fails: an unreadable version only means the two
# versions cannot be ordered. The value ends up in status.json, so it is restricted to
# what a version string may contain instead of being escaped.
installed_version() {
  local output
  output=$("$panel_program" --version 2>/dev/null || true)
  case $output in
  'proxy-panel '*) previous_version=${output#proxy-panel } ;;
  *) previous_version='' ;;
  esac
  previous_version=${previous_version//[^0-9A-Za-z._+-]/}
  printf '%s' "$previous_version"
}

# The release grammar accepted here: X.Y.Z without leading zeros and without an -rc
# suffix, because a release candidate is not something the web UI offers.
official_version() {
  [[ $1 =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
}

# Succeed when the candidate sorts after the installed version. An installed version
# that is not official - a development build, or a program that prints something else -
# is unordered, so nothing can be compared and the request is let through; the panel then
# reports what it actually runs.
is_newer() {
  local candidate=$1 installed=$2
  official_version "$installed" || return 0
  [[ $candidate != "$installed" ]] || return 1
  [[ $(printf '%s\n%s\n' "$candidate" "$installed" | sort -V | tail -n 1) == "$candidate" ]]
}

# Write status.json atomically: a temporary file in the same directory renamed over the
# target, so the panel never reads half a document and a symlink left at the target is
# replaced instead of written through. The file is created by this root process and
# handed to the panel account, which owns the directory but nothing else here.
write_status() {
  local state=$1 code=$2 finished=$3
  status_temporary=$(mktemp "$request_directory/.status.XXXXXXXX")
  printf '{"requestId":"%s","version":"%s","previousVersion":"%s","state":"%s","code":"%s","startedAt":"%s","finishedAt":"%s"}\n' \
    "$request_id" "$request_version" "$previous_version" "$state" "$code" "$started_at" "$finished" \
    > "$status_temporary"
  chown "$account_id:$account_gid" "$status_temporary"
  chmod 0600 "$status_temporary"
  mv -f -- "$status_temporary" "$status_file"
  status_temporary=''
}

# Answer a request that must not be acted on. The panel polls status.json for the
# verdict, so the reason for the operator goes to stderr, where the unit's journal keeps
# it. Exiting 0 here is deliberate: a refused or failed request is an outcome the panel
# reports, and a non-zero status is reserved for a helper that could not even record one.
reject() {
  printf '面板更新请求被拒绝：%s\n' "$1" >&2
  write_status failed invalid_request "$started_at"
  exit 0
}

# --- the request -------------------------------------------------------------

# No request, no work: the path unit also fires for a host that never asks for an update.
[[ -e $request_file || -L $request_file ]] || exit 0

# The owner of the request file is what makes it the panel's request rather than
# anyone's, so the account has to resolve before anything else can be judged.
if ! account_id=$(id -u "$account") || ! account_gid=$(id -g "$account"); then
  printf '面板更新请求无法处理：服务账号 %s 不存在。\n' "$account" >&2
  exit 1
fi

started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
[[ ! -L $request_file ]] || reject '请求文件是符号链接。'
[[ -f $request_file ]] || reject '请求文件不是普通文件。'
(( $(stat -c '%s' -- "$request_file") <= 4096 )) || reject '请求文件超过 4096 字节。'
[[ $(stat -c '%u' -- "$request_file") == "$account_id" ]] || reject '请求文件不属于面板账号。'

# Read with grep and sed rather than a JSON parser: the panel writes exactly one line, and
# a second matching line therefore yields an invalid id or version, which is the same
# answer as any other unparsable document.
if ! request_document=$(cat -- "$request_file" 2>/dev/null); then
  reject '请求文件无法读取。'
fi
request_id=$(sed -n 's/.*"requestId":"\([0-9a-f]\{32\}\)".*/\1/p' <<< "$request_document")
request_version=$(sed -n 's/.*"version":"\([^"]*\)".*/\1/p' <<< "$request_document")
[[ $request_id =~ ^[0-9a-f]{32}$ ]] || reject '请求里没有合法的 requestId。'
official_version "$request_version" || reject '请求里的版本号不是 X.Y.Z。'

# The path unit fires more than once for one request; a status that already names it is
# the answer to that request, whatever state it records.
if [[ -f $status_file && ! -L $status_file ]] \
  && grep -q -F -- "\"requestId\":\"$request_id\"" "$status_file"; then
  exit 0
fi

previous_version=$(installed_version)
if ! is_newer "$request_version" "$previous_version"; then
  printf '面板已经是 %s，不再安装 %s。\n' "${previous_version:-未知版本}" "$request_version" >&2
  write_status failed not_newer "$started_at"
  exit 0
fi

# --- the update --------------------------------------------------------------

started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
write_status running running ''
# The updater's output is the only account of a failure that survives, and the panel must
# not be able to read it, so it goes to a root-only log beside the other update material.
# The last lines are what a diagnosis needs; anything older is in the backups and the
# journal anyway.
if [[ ! -e $log_file ]]; then
  (set -o noclobber; : > "$log_file") || exit 1
  chmod 0600 -- "$log_file"
fi
[[ -f $log_file && ! -L $log_file ]] || exit 1
tail -n 2000 -- "$log_file" > "$log_file.trim"
chmod 0600 -- "$log_file.trim"
mv -f -- "$log_file.trim" "$log_file"
printf '面板更新：%s -> %s（requestId %s，提交时间 %s）\n' \
  "${previous_version:-未知版本}" "$request_version" "$request_id" "$started_at" >> "$log_file"

update_status=0
# The exit status is read in the else branch: inside the negation of an if, $? is always 0.
if "$updater" --component panel --version "$request_version" --yes >> "$log_file" 2>&1; then
  update_status=0
else
  update_status=$?
fi
finished_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
if ((update_status != 0)); then
  printf '面板更新失败（退出状态 %s）；qui-update 已回滚，详见 %s。\n' "$update_status" "$log_file" >&2
  write_status failed update_failed "$finished_at"
  exit 0
fi
# qui-update reports success only after the new program stayed up, and it already refuses
# to keep a version other than the requested one. Reading the version here is the second,
# independent check that the value the panel asked for is the value that is installed.
installed=$(installed_version)
if [[ $installed != "$request_version" ]]; then
  printf '面板更新结束后报告的版本是 %s，不是 %s。\n' "${installed:-未知版本}" "$request_version" >&2
  write_status failed version_mismatch "$finished_at"
  exit 0
fi
write_status succeeded succeeded "$finished_at"
printf '面板已更新到 %s（原 %s）。\n' "$request_version" "${previous_version:-未知版本}"
