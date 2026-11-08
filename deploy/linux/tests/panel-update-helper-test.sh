#!/usr/bin/env bash
# Regression test for deploy/linux/panel-update-request.sh, the root-side half of "update
# the panel from the web UI".
#
# Run it on any host with bash and coreutils; root is not required:
#
#   bash deploy/linux/tests/panel-update-helper-test.sh
#
# Nothing here touches a real installation. The script under test honours
# QUI_PANEL_UPDATE_TEST_ROOT as a prefix for every absolute path and takes the two
# programs it uses from the environment, so the whole scenario runs below one temporary
# directory against fake programs. The updater fixture runs no update: it checks that it
# was called with exactly --component panel --version X.Y.Z --yes, rewrites the fake
# panel's reported version and exits with the status the case asked for, which is the
# contract the real qui-update has.
#
# Three cases need a filesystem that really applies ownership and symlinks: a request file
# owned by another account, a symlinked request file, and a symlink planted at status.json.
# Where the fixtures cannot create them - a Windows Git Bash, or a host where chown does
# nothing - those cases skip with a reason instead of reporting a pass.
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
helper=$script_dir/../panel-update-request.sh
if [[ ! -f $helper ]]; then
  printf 'panel-update-helper-test: missing %s\n' "$helper" >&2
  exit 1
fi
missing=''
for tool in grep sed mktemp stat chmod cat tail sort date ln wc; do
  command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
done
if [[ -n $missing ]]; then
  printf 'panel-update-helper-test: missing commands:%s\n' "$missing"
  exit 1
fi

TOP=$(mktemp -d "${TMPDIR:-/tmp}/qui-panel-update-test.XXXXXXXX") || exit 1
cleanup() { rm -rf -- "$TOP"; }
trap cleanup EXIT

# The account the helper trusts is the one running this test, so the fixture request files
# are owned by exactly that account without needing a second one.
account=$(id -un)
account_id=$(id -u)

# The fake installation: request directory, registry for the log, and the two programs.
root=$TOP/root
request_directory=$root/var/lib/proxy-panel/panel-update
request_file=$request_directory/request.json
status_file=$request_directory/status.json
log_directory=$root/var/lib/proxycontrol-install
log_file=$log_directory/panel-update.log
updater=$root/usr/local/bin/qui-update
panel=$root/usr/local/bin/proxy-panel
state=$TOP/state
version_file=$state/panel-version
calls_file=$state/updater-calls

passes=0
failed=0
skipped=0
LAST_STATUS=0
LAST_OUTPUT=''

ok() { passes=$((passes + 1)); printf 'ok - %s\n' "$1"; }
not_ok() { failed=$((failed + 1)); printf 'not ok - %s (%s)\n' "$1" "$2"; }
skip() { skipped=$((skipped + 1)); printf 'skip - %s (%s)\n' "$1" "$2"; }


# Replace the fake installation with a fresh one. $1 is the version the fake panel
# reports; the fixtures keep their state in $state, which the cases also use.
new_case() {
  local installed=$1
  rm -rf -- "$root" "$state"
  mkdir -p -- "$request_directory" "$log_directory" "${updater%/*}" "$state"
  chmod 0700 -- "$request_directory"
  printf '%s' "$installed" > "$version_file"
  : > "$calls_file"
  cat > "$panel" <<'FAKE_PANEL'
#!/usr/bin/env bash
# Fake proxy-panel: reports the version in $FAKE_STATE/panel-version, in the form
# cmd/panel/main.go prints.
[ "${1:-}" = --version ] || exit 2
printf 'proxy-panel %s\n' "$(cat -- "$FAKE_STATE/panel-version")"
FAKE_PANEL
  cat > "$updater" <<'FAKE_UPDATER'
#!/usr/bin/env bash
# Fake qui-update: asserts the arguments the helper is allowed to pass, records them,
# applies at most the version change of the case and exits with its status.
set -u
printf '%s\n' "$*" >> "$FAKE_STATE/updater-calls"
expected="--component panel --version ${FAKE_UPDATE_VERSION:?} --yes"
if [[ $* != "$expected" ]]; then
  printf 'fake qui-update: 参数不符：%s\n' "$*" >&2
  exit 4
fi
printf 'fake qui-update: 面板更新 %s\n' "$FAKE_UPDATE_VERSION"
case ${FAKE_UPDATE:-succeed} in
  succeed) printf '%s' "$FAKE_UPDATE_VERSION" > "$FAKE_STATE/panel-version" ;;
  unchanged) : ;;
  *) printf 'fake qui-update: 未知的 FAKE_UPDATE：%s\n' "$FAKE_UPDATE" >&2; exit 5 ;;
esac
exit "${FAKE_UPDATE_STATUS:-0}"
FAKE_UPDATER
  chmod 0755 -- "$panel" "$updater"
  LAST_STATUS=0
  LAST_OUTPUT=''
  FAKE_UPDATE=succeed
  FAKE_UPDATE_VERSION=''
  FAKE_UPDATE_STATUS=0
}

REQUEST_ID=0123456789abcdef0123456789abcdef

# Write a request document the way the panel does, with the rename included so no case
# accidentally tests a torn file.
write_request() { # version [requestId]
  local version=$1 id=${2:-$REQUEST_ID}
  printf '{"requestId":"%s","version":"%s","requestedAt":"2026-11-08T10:00:00Z","requestedBy":"owner"}\n' \
    "$id" "$version" > "$request_directory/.request.tmp"
  mv -f -- "$request_directory/.request.tmp" "$request_file"
}

# Run the helper as the unit runs it, with every path and program overridden. The exit
# status lands in LAST_STATUS, because some cases are about a helper that refuses.
run_helper() {
  local status=0
  FAKE_STATE=$state \
    FAKE_UPDATE=${FAKE_UPDATE:-succeed} \
    FAKE_UPDATE_VERSION=${FAKE_UPDATE_VERSION:-} \
    FAKE_UPDATE_STATUS=${FAKE_UPDATE_STATUS:-0} \
    QUI_PANEL_UPDATE_TEST_ROOT=$root \
    QUI_PANEL_UPDATE_ACCOUNT=$account \
    QUI_PANEL_UPDATE_PROGRAM=$panel \
    QUI_PANEL_UPDATE_UPDATER=$updater \
    bash "$helper" > "$TOP/helper.out" 2>&1 || status=$?
  LAST_STATUS=$status
  LAST_OUTPUT=$(cat -- "$TOP/helper.out")
  return 0
}

# Print one string field of status.json. The document is a single line with fixed keys and
# these values never contain a quote.
status_value() {
  [[ -f $status_file ]] || return 0
  sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p" "$status_file"
}

check_status() { # description key expected
  local actual
  actual=$(status_value "$2")
  if [[ $actual == "$3" ]]; then ok "$1"; else not_ok "$1" "status.json $2=\"$actual\"，期望 \"$3\""; fi
}

check_updater_calls() { # description expected-count
  local count
  count=$(wc -l < "$calls_file" | tr -d ' ')
  if [[ $count == "$2" ]]; then ok "$1"; else not_ok "$1" "qui-update 被调用 $count 次，期望 $2"; fi
}

check_absent() { # description path
  if [[ -e $2 || -L $2 ]]; then not_ok "$1" "$2 不应存在"; else ok "$1"; fi
}

check_regular() { # description path
  if [[ -f $2 && ! -L $2 ]]; then ok "$1"; else not_ok "$1" "缺少普通文件 $2"; fi
}

check_mode() { # description path expected-mode
  local mode
  mode=$(stat -c '%a' -- "$2" 2>/dev/null || printf 'missing')
  if [[ $mode == "$3" ]]; then ok "$1"; else not_ok "$1" "$2 的权限是 $mode，期望 $3"; fi
}

# Capability probes for the cases that need a real filesystem.
ownership_supported=false
if printf 'x\n' > "$TOP/probe-owner" && chown 0:0 "$TOP/probe-owner" 2>/dev/null \
  && [[ $(stat -c '%u' -- "$TOP/probe-owner") == 0 && $account_id != 0 ]]; then
  ownership_supported=true
fi
symlink_supported=false
if ln -s "$TOP/probe-owner" "$TOP/probe-link" 2>/dev/null && [[ -L $TOP/probe-link ]]; then
  symlink_supported=true
fi
# A normal Linux filesystem keeps the mode; a Windows Git Bash reports 644 for every file,
# so the permission checks skip there instead of failing.
modes_supported=false
if printf 'x\n' > "$TOP/probe-mode" && chmod 0600 "$TOP/probe-mode" 2>/dev/null \
  && [[ $(stat -c '%a' -- "$TOP/probe-mode") == 600 ]]; then
  modes_supported=true
fi
# An account other than the one running the test, for the wrong-owner case.
other_account=''
if $ownership_supported; then
  for candidate in nobody daemon; do
    if uid=$(id -u "$candidate" 2>/dev/null) && [[ $uid != "$account_id" ]]; then other_account=$candidate; break; fi
  done
fi

check_rfc3339() { # description key
  local actual
  actual=$(status_value "$2")
  if [[ $actual =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
    ok "$1"
  else
    not_ok "$1" "status.json $2=\"$actual\" 不是 RFC3339 UTC"
  fi
}

check_exit() { # description expected
  if [[ $LAST_STATUS == "$2" ]]; then ok "$1"; else not_ok "$1" "退出状态 $LAST_STATUS，期望 $2"; fi
}

# --- the request --------------------------------------------------------------

# No request at all: the path unit also fires for a host that never asked for an update.
new_case 1.4.3
run_helper
check_exit '缺少请求时静默结束' 0
check_absent '缺少请求时不写 status.json' "$status_file"
check_absent '缺少请求时不写更新日志' "$log_file"
check_updater_calls '缺少请求时不运行 qui-update' 0

# A document that is not the one the panel writes: refused with a recorded reason.
new_case 1.4.3
printf 'no request here\n' > "$request_file"
run_helper
check_exit '无效请求不把单元标成失败' 0
check_status '无效请求记为 failed' state failed
check_status '无效请求的原因' code invalid_request
check_status '无效请求没有 requestId' requestId ''
check_updater_calls '无效请求不运行 qui-update' 0

# An id that is not 32 lowercase hexadecimal characters is unusable, because the panel
# matches the answer by that id.
new_case 1.4.3
write_request 1.5.0 0123456789ABCDEF0123456789ABCDEF
run_helper
check_status '大写 requestId 被拒绝' code invalid_request
new_case 1.4.3
write_request 1.5.0 0123456789abcdef0123456789abcde
run_helper
check_status '短 requestId 被拒绝' code invalid_request

# A document without a version, and one whose requestId is missing entirely.
new_case 1.4.3
printf '{"requestId":"%s","requestedAt":"2026-11-08T10:00:00Z"}\n' "$REQUEST_ID" > "$request_file"
run_helper
check_status '缺少 version 被拒绝' code invalid_request
new_case 1.4.3
printf '{"version":"1.5.0","requestedAt":"2026-11-08T10:00:00Z"}\n' > "$request_file"
run_helper
check_status '缺少 requestId 被拒绝' code invalid_request

# Only official releases are offered by the web UI, so an -rc, a two-part version, a
# leading zero and a v prefix are all refused rather than handed to the updater.
for bad_version in 1.5 1.5.0-rc.1 01.5.0 v1.5.0 1.5.0-1 1.5.0.1; do
  new_case 1.4.3
  write_request "$bad_version"
  run_helper
  check_status "版本 $bad_version 被拒绝" code invalid_request
done

# The size limit is what keeps a huge file from being read as a request at all.
new_case 1.4.3
write_request 1.5.0
printf '%*s' 5000 '' >> "$request_file"
run_helper
check_status '超过 4096 字节的请求被拒绝' code invalid_request

if $symlink_supported; then
  new_case 1.4.3
  write_request 1.5.0
  mv -f -- "$request_file" "$TOP/real-request.json"
  ln -s "$TOP/real-request.json" "$request_file"
  run_helper
  check_status '符号链接的请求被拒绝' code invalid_request
  check_updater_calls '符号链接的请求不运行 qui-update' 0
else
  skip '符号链接的请求被拒绝' '本机不能创建真正的符号链接'
fi

if $ownership_supported && [[ -n $other_account ]]; then
  new_case 1.4.3
  write_request 1.5.0
  chown "$other_account:$other_account" "$request_file"
  run_helper
  check_status '不属于面板账号的请求被拒绝' code invalid_request
  check_updater_calls '不属于面板账号的请求不运行 qui-update' 0
else
  skip '不属于面板账号的请求被拒绝' '本机不能把文件改归另一个账号（需要 root 与 chown）'
fi


# --- ordering against the installed panel --------------------------------------

# Same version and older version: nothing to do, and the updater is not started.
new_case 1.4.3
write_request 1.4.3
run_helper
check_exit '同版本请求结束' 0
check_status '同版本记为 failed' state failed
check_status '同版本的原因' code not_newer
check_status '同版本报告已安装版本' previousVersion 1.4.3
check_updater_calls '同版本不运行 qui-update' 0

new_case 1.4.3
write_request 1.4.2
run_helper
check_status '旧版本请求的原因' code not_newer
check_updater_calls '旧版本不运行 qui-update' 0

# A development build reports something that cannot be ordered; the request is applied and
# the status reports what was there instead of inventing an order.
new_case dev
FAKE_UPDATE_VERSION=1.5.0
write_request 1.5.0
run_helper
check_exit '无法比较版本时仍然更新' 0
check_status '无法比较版本时更新成功' code succeeded
check_status '无法比较版本时报告原版本' previousVersion dev
check_updater_calls '无法比较版本时运行 qui-update' 1

# --- the update ----------------------------------------------------------------

# The happy path: the updater is called with exactly one version and no other argument,
# and the answer names both versions.
new_case 1.4.3
FAKE_UPDATE_VERSION=1.5.0
write_request 1.5.0
run_helper
check_exit '更新成功' 0
check_status '更新成功记为 succeeded' state succeeded
check_status '更新成功的代码' code succeeded
check_status '更新成功的版本' version 1.5.0
check_status '更新成功的原版本' previousVersion 1.4.3
check_status '更新成功保留 requestId' requestId "$REQUEST_ID"
check_rfc3339 'startedAt 是 RFC3339 UTC' startedAt
check_rfc3339 'finishedAt 是 RFC3339 UTC' finishedAt
check_updater_calls '更新成功运行一次 qui-update' 1
if $modes_supported; then
  check_mode 'status.json 为 0600' "$status_file" 600
  check_mode '更新日志为 0600' "$log_file" 600
else
  skip 'status.json 为 0600' '本机不保留文件权限位'
  skip '更新日志为 0600' '本机不保留文件权限位'
fi
check_regular '请求文件由面板保留' "$request_file"
if grep -q -F 'fake qui-update: 面板更新 1.5.0' "$log_file"; then
  ok '更新日志记录 qui-update 的输出'
else
  not_ok '更新日志记录 qui-update 的输出' "$log_file 里没有 qui-update 的输出"
fi

# A failed update has already been rolled back by qui-update; the helper only reports it.
# The version before and after is the same one, which is what a rollback leaves behind.
new_case 1.4.3
FAKE_UPDATE=unchanged
FAKE_UPDATE_STATUS=1
FAKE_UPDATE_VERSION=1.5.0
write_request 1.5.0
run_helper
check_exit '更新失败仍记录结果' 0
check_status '更新失败记为 failed' state failed
check_status '更新失败的原因' code update_failed
check_status '更新失败保留目标版本' version 1.5.0
check_status '更新失败报告原版本' previousVersion 1.4.3
check_rfc3339 '更新失败也有 finishedAt' finishedAt
check_updater_calls '更新失败运行一次 qui-update' 1

# qui-update exits 0 without the requested version being installed: the helper does not
# take that as success.
new_case 1.4.3
FAKE_UPDATE=unchanged
FAKE_UPDATE_VERSION=1.5.0
write_request 1.5.0
run_helper
check_status '版本不符记为 failed' state failed
check_status '版本不符的原因' code version_mismatch
check_status '版本不符报告原版本' previousVersion 1.4.3

# The same request arriving twice - the path unit re-arms for one file - is answered from
# the status that is already there, and nothing runs a second time.
new_case 1.4.3
FAKE_UPDATE_VERSION=1.5.0
write_request 1.5.0
run_helper
cp -- "$status_file" "$TOP/status.first"
run_helper
check_exit '重复的请求结束' 0
check_updater_calls '重复的请求不再运行 qui-update' 1
if cmp -s -- "$TOP/status.first" "$status_file"; then
  ok '重复的请求不重写 status.json'
else
  not_ok '重复的请求不重写 status.json' "status.json 变了"
fi
# A different request after a finished one is a new attempt and does run.
write_request 1.6.0 0123456789abcdef0123456789abcdea
FAKE_UPDATE_VERSION=1.6.0
run_helper
check_status '新 requestId 触发新的更新' version 1.6.0
check_updater_calls '新 requestId 再次运行 qui-update' 2

if $symlink_supported; then
  # The status file is replaced through a rename, so a symlink planted at its name is
  # replaced instead of being written through: whatever it pointed at stays untouched.
  new_case 1.4.3
  printf 'do not touch\n' > "$TOP/victim"
  ln -s "$TOP/victim" "$status_file"
  FAKE_UPDATE_VERSION=1.5.0
  write_request 1.5.0
  run_helper
  check_status '符号链接位置写入状态' code succeeded
  check_regular 'status.json 变成普通文件' "$status_file"
  if [[ $(cat -- "$TOP/victim") == 'do not touch' ]]; then
    ok '不通过符号链接写入'
  else
    not_ok '不通过符号链接写入' '符号链接指向的文件被改写'
  fi
else
  skip '不通过符号链接写入' '本机不能创建真正的符号链接'
fi

printf '\npanel-update-helper-test: %s passed, %s failed, %s skipped\n' "$passes" "$failed" "$skipped"
if ((failed > 0)); then exit 1; fi
exit 0

