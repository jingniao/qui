#!/usr/bin/env bash
# Regression test for deploy/linux/update.sh (installed as /usr/local/bin/qui-update)
# and for the boot hook deploy/linux/update-recovery.sh.
#
# Run it as root on Linux:
#
#   sudo bash deploy/linux/tests/update-test.sh
#
# Everything happens below one temporary directory: update.sh and update-recovery.sh
# honour QUI_UPDATE_TEST_ROOT as a prefix for every absolute path they touch, so no
# real installation, service manager or database is involved. Nothing is downloaded:
# every scenario uses an offline archive (--bundle/--sums) built on the spot.
#
# The init-system library is a fake. update.sh sources the service.sh that lives next
# to it, so the test copies update.sh next to a generated fake service.sh; the fake is
# also what install.sh "installs", which means the snapshot of an interrupted update
# carries the fake as well and a recovery in a child process behaves like the update
# that wrote the journal. The fake keeps all of its state in files below $FAKE_STATE,
# because recoveries run in separate processes.
#
# Knobs the fake honours (exported per scenario, see fake_reset):
#   FAKE_INIT            systemd | openrc            (default systemd)
#   FAKE_INSTALL         ok | fail-after-replace | kill-parent | sleep | probe
#   FAKE_CHECK           compatible | newer | foreign | unsupported
#   FAKE_TARGET          version whose start migrates panel.db
#   FAKE_START_WRITES=1  starting the panel with the target version migrates panel.db
#   FAKE_START_KILL=1    ... and then kills the updater (power loss at "started")
#   FAKE_STOP_SLEEP=1    stopping the panel takes three seconds
#   FAKE_UNHEALTHY_VERSION  version whose service_healthy always fails
#
# QUI_UPDATE_TEST_ALLOW_SKIP=1 turns the "not root / not Linux" refusal into a skip
# with exit status 0. QUI_UPDATE_TEST_KEEP=1 keeps the temporary directory.
set -euo pipefail

if [[ $(uname -s) != Linux || ${EUID} -ne 0 ]]; then
  printf 'SKIP update-test: needs Linux and root (uname -s %s, EUID %s).\n' \
    "$(uname -s)" "${EUID}"
  if [[ $(uname -s) == MINGW* || $(uname -s) == MSYS* || ${QUI_UPDATE_TEST_ALLOW_SKIP:-0} == 1 ]]; then exit 0; fi
  exit 1
fi

missing=''
# update.sh itself needs every one of these (it refuses to run without them), so the
# test says which one is missing instead of failing inside a scenario.
for tool in timeout flock sha256sum realpath stat cmp tar mktemp sed grep install cp rm mv \
  curl cut uname; do
  command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
done
if [[ -n $missing ]]; then
  printf 'update-test: missing commands:%s (coreutils, findutils and util-linux flock).\n' \
    "$missing"
  exit 1
fi

script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
repo_dir=$(cd -- "$script_dir/../../.." && pwd -P)
update_script=$repo_dir/deploy/linux/update.sh
recovery_script=$repo_dir/deploy/linux/update-recovery.sh
for file in "$update_script" "$recovery_script"; do
  if [[ ! -f $file ]]; then
    printf 'update-test: missing script under test: %s\n' "$file" >&2
    exit 1
  fi
done

case $(uname -m) in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *)
    printf 'SKIP update-test: update.sh does not support the architecture %s.\n' "$(uname -m)"
    exit 0
    ;;
esac

TOP=$(mktemp -d "${TMPDIR:-/tmp}/qui-update-test.XXXXXXXX") || exit 1
failure_file=$TOP/failures
dir=''
ROOT=''
WORK=''
REGISTRY=''
BACKUPS=''
BUNDLE=''
SUMS=''
FOUND_GLOB=''
LAST_STATUS=0
UPDATE_PID=''

cleanup_top() {
  if [[ ${QUI_UPDATE_TEST_KEEP:-0} == 1 ]]; then
    printf 'update-test: temporary directory kept: %s\n' "$TOP"
    return 0
  fi
  rm -rf -- "$TOP"
}
trap cleanup_top EXIT

mkdir -p -- "$TOP/harness"


# Write the fake init-system library. update.sh and the updater a snapshot carries both
# source it, so it must work as a sourced file and take everything from the environment.
write_fake_service_library() {
  cat > "$1" <<'FAKE_SERVICE'
#!/usr/bin/env bash
# Fake stand-in for deploy/linux/service.sh, used by deploy/linux/tests/update-test.sh.
# All state lives in files below $FAKE_STATE because recoveries are child processes.
fake_state=${FAKE_STATE:?FAKE_STATE must name the fake state directory}
mkdir -p -- "$fake_state/active" || exit 1

fake_log() { printf '%s\n' "$*" >> "$fake_state/calls.log"; }

# Print the version of the installed program of a component, the way update.sh reads it.
fake_program_version() {
  local component=$1 path=${QUI_UPDATE_TEST_ROOT:-}/usr/local/bin/proxy-$1 output='' version=''
  if [[ -x $path ]]; then output=$("$path" --version 2>/dev/null || true); fi
  case $output in
    "proxy-$component "*) version=${output#proxy-$component } ;;
    *) return 0 ;;
  esac
  version=${version//$'\n'/}
  printf '%s' "$version"
}

detect_init() { printf '%s\n' "${FAKE_INIT:-systemd}"; }
init_system_known() { return 0; }

service_is_active() { fake_log "is-active $1"; [[ -f $fake_state/active/$1 ]]; }

service_start() {
  local name=$1 version=''
  fake_log "start $name"
  : > "$fake_state/active/$name"
  [[ $name == proxy-panel ]] || return 0
  [[ ${FAKE_START_WRITES:-0} == 1 ]] || return 0
  version=$(fake_program_version panel)
  [[ -n ${FAKE_TARGET:-} && $version == "${FAKE_TARGET}" ]] || return 0
  # A real panel migrates the database while it starts; only the new version does.
  printf 'MIGRATED by %s\n' "$version" >> "${FAKE_PANEL_DB:?}"
  if [[ ${FAKE_START_KILL:-0} == 1 ]]; then
    # This function is part of the library update.sh sourced, so $$ is the updater:
    # the update dies exactly where a power loss would, after the migration.
    kill -9 $$
  fi
  return 0
}

service_stop() {
  local name=$1
  fake_log "stop $name"
  # The stop has completed before the call returns, exactly like systemctl stop, which
  # returns once the process is gone; FAKE_STOP_SLEEP only makes the call itself slow,
  # so a signal sent meanwhile arrives after the service is already down.
  rm -f -- "$fake_state/active/$name"
  if [[ $name == proxy-panel && ${FAKE_STOP_SLEEP:-0} == 1 ]]; then sleep 3; fi
  return 0
}

service_restart() { fake_log "restart $1"; : > "$fake_state/active/$1"; return 0; }
service_enable() { fake_log "enable $1"; return 0; }
service_disable() { fake_log "disable $1"; return 0; }
service_reload_definitions() { fake_log 'reload-definitions'; return 0; }

service_state() {
  fake_log "state $1"
  if [[ -f $fake_state/active/$1 ]]; then printf 'active\n'; else printf 'inactive\n'; fi
  return 0
}

service_main_pid() {
  if [[ -f $fake_state/active/$1 ]]; then printf '4242\n'; return 0; fi
  return 1
}

service_healthy() {
  local name=$1 component=${1#proxy-} version=''
  fake_log "healthy $name"
  version=$(fake_program_version "$component")
  if [[ -n ${FAKE_UNHEALTHY_VERSION:-} && $version == "${FAKE_UNHEALTHY_VERSION}" ]]; then
    return 1
  fi
  return 0
}

service_log_hint() { printf 'fake log of %s\n' "$1"; }

# update.sh refuses to run without the tools of the init system it detected. That check
# runs `command -v`, which also finds shell functions, so a host that has no OpenRC gets
# the names without anything being installed on it.
if ! command -v systemctl >/dev/null 2>&1; then systemctl() { return 0; }; fi
if ! command -v rc-service >/dev/null 2>&1; then rc-service() { return 0; }; fi
if ! command -v rc-update >/dev/null 2>&1; then rc-update() { return 0; }; fi
FAKE_SERVICE
}


# Write the fake deploy/linux/install.sh that every test archive carries. It replaces
# exactly the files update.sh's component_files lists, so a snapshot and its restore
# agree with what the "installed" version put there.
write_fake_install() {
  cat > "$1" <<'FAKE_INSTALL'
#!/usr/bin/env bash
# qui_write_record compatible fixture installer
# Fake deploy/linux/install.sh, used by deploy/linux/tests/update-test.sh.
# Called by update.sh as:  install.sh --panel|--node --binary FILE --sha256 DIGEST
set -eu

component=''
binary=''
while [ $# -gt 0 ]; do
  case $1 in
    --panel) component=panel; shift ;;
    --node) component=agent; shift ;;
    --binary) binary=$2; shift 2 ;;
    --sha256) shift 2 ;;
    *) printf 'fake install.sh: unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done
[ -n "$component" ] || { printf 'fake install.sh: no component\n' >&2; exit 2; }
[ -n "$binary" ] || { printf 'fake install.sh: no --binary\n' >&2; exit 2; }

root=${QUI_UPDATE_TEST_ROOT:?}
state=${FAKE_STATE:?}
init=${FAKE_INIT:-systemd}
registry=$root/var/lib/proxycontrol-install
bin_dir=$root/usr/local/bin
library_dir=$root/usr/local/lib/proxycontrol
host_network_library_dir=$root/usr/local/lib/proxy-agent
version=$(sed -n 's/^version=//p' "$binary" | sed -n '1p')
program=$bin_dir/proxy-$component

if [ "$init" = openrc ]; then
  unit=$root/etc/init.d/proxy-$component
else
  unit=$root/etc/systemd/system/proxy-$component.service
fi

install_program() {
  install -m 0755 "$binary" "$program"
}

write_unit() {
  if [ "$init" = openrc ]; then
    printf '#!/sbin/openrc-run\n# fake init script for proxy-%s %s\nexit 0\n' \
      "$component" "$version" > "$state/unit.tmp.new"
    install -m 0755 "$state/unit.tmp.new" "$unit"
  else
    printf '# fake systemd unit for proxy-%s %s\n' \
      "$component" "$version" > "$state/unit.tmp.new"
    install -m 0644 "$state/unit.tmp.new" "$unit"
  fi
}

install_everything() {
  install_program
  if [ "${FAKE_MULTI_CORE:-0}" = 1 ] && [ "$component" = agent ]; then
    for core in xray sing-box; do printf '%s-%s\n' "$core" "$version" > "$host_network_library_dir/$core"; done
    printf 'cores: [] # changed by fixture %s\n' "$version" > "$root/etc/proxy-agent/config.yaml"
  fi
  write_unit
  install -m 0755 "$FAKE_UPDATER_SOURCE" "$bin_dir/qui-update"
  printf '\n# qui-update of version %s, written by the fake install.sh\n' "$version" \
    >> "$bin_dir/qui-update"
  install -m 0644 "$FAKE_SERVICE_SOURCE" "$library_dir/service.sh"
  printf '\n# service.sh of version %s, written by the fake install.sh\n' "$version" \
    >> "$library_dir/service.sh"
  if [ "$component" = panel ]; then
    printf '#!/bin/sh\n# fake terminal menu of version %s\nexit 0\n' "$version" \
      > "$state/qui.tmp.new"
    install -m 0755 "$state/qui.tmp.new" "$bin_dir/qui"
  else
    if [ "$init" = openrc ]; then
      printf '#!/sbin/openrc-run\n# fake enforced-source init script %s\nexit 0\n' "$version" \
        > "$state/host-network.tmp.new"
      install -m 0755 "$state/host-network.tmp.new" \
        "$root/etc/init.d/proxy-agent-host-network"
      printf '#!/bin/sh\n# fake inotifyd watcher %s\nexit 0\n' "$version" \
        > "$state/host-network-watch.tmp.new"
      install -m 0755 "$state/host-network-watch.tmp.new" \
        "$host_network_library_dir/host-network-watch"
    else
      printf '# fake enforced-source unit %s\n' "$version" \
        > "$state/host-network.tmp.new"
      install -m 0644 "$state/host-network.tmp.new" \
        "$root/etc/systemd/system/proxy-agent-host-network.service"
      printf '# fake enforced-source path unit %s\n' "$version" \
        > "$state/host-network-path.tmp.new"
      install -m 0644 "$state/host-network-path.tmp.new" \
        "$root/etc/systemd/system/proxy-agent-host-network.path"
    fi
  fi
  program_digest=$(sha256sum "$program")
  program_digest=${program_digest%% *}
  unit_digest=$(sha256sum "$unit")
  unit_digest=${unit_digest%% *}
  . "$FAKE_SERVICE_SOURCE"
  qui_record_cores "$registry/$component.manifest" "$component"
  digest=$program_digest; init_system=$init
  host_network_service_digest=''; host_network_path_digest=''
  tuning_service_digest=''; tuning_path_digest=''
  if [[ ${FAKE_MULTI_CORE:-0} == 1 && $component == agent ]]; then
    for i in "${!record_cores[@]}"; do
      value=$(sha256sum "$host_network_library_dir/${record_cores[i]}")
      record_digests[i]=${value%% *}
    done
  fi
  qui_write_record "$state/manifest.tmp.new"
  install -m 0600 "$state/manifest.tmp.new" "$registry/$component.manifest"
}



case ${FAKE_INSTALL:-ok} in
  ok)
    install_everything
    ;;
  probe)
    # Record the backups directory at the moment install.sh runs: the pre-update copy
    # has to exist under its final name and nothing may be partial.
    ls -a "$root/var/lib/proxy-panel/backups" > "$state/backups-at-install.txt" 2>/dev/null || true
    install_everything
    ;;
  fail-after-replace)
    if [ "${FAKE_MULTI_CORE:-0}" = 1 ]; then install_everything; else install_program; fi
    printf 'fake install.sh: failing after replacing the program\n' >&2
    exit 1
    ;;
  kill-parent)
    install_everything
    kill -9 "$PPID"
    exit 0
    ;;
  sleep)
    install_everything
    : > "$state/install.started"
    sleep 5
    ;;
  *)
    printf 'fake install.sh: unknown FAKE_INSTALL: %s\n' "${FAKE_INSTALL}" >&2
    exit 2
    ;;
esac
exit 0
FAKE_INSTALL
  chmod 0755 -- "$1"
}

# Write a fake program that answers --version and the panel's --check-database. The
# installed copy and the copy inside the test archive are the same script with anothe
# version, which is what lets the version check in update.sh see the difference.
write_program() {
  local path=$1 component=$2 version=$3
  mkdir -p -- "${path%/*}"
  {
    printf '#!/bin/sh\n'
    printf '# Fake proxy-%s %s, written by deploy/linux/tests/update-test.sh.\n' \
      "$component" "$version"
    printf 'component=%s\n' "$component"
    printf 'version=%s\n' "$version"
    cat <<'FAKE_PROGRAM'
case ${1:-} in
  --version) printf 'proxy-%s %s\n' "$component" "$version"; exit 0 ;;
  --check-database)
    case ${FAKE_CHECK:-compatible} in
      compatible) printf 'database: compatible\n'; exit 0 ;;
      newer) printf 'database: written by a newer panel\n' >&2; exit 3 ;;
      foreign) printf 'database: not a panel database\n' >&2; exit 4 ;;
      unsupported) printf 'flag provided but not defined: -check-database\n' >&2; exit 1 ;;
      *) printf 'fake program: unknown FAKE_CHECK\n' >&2; exit 4 ;;
    esac ;;
esac
printf 'fake proxy-%s: only --version and --check-database are implemented\n' "$component" >&2
exit 2
FAKE_PROGRAM
  } > "$path"
  chmod 0755 -- "$path"
}

# Start a scenario from a fresh QUI_UPDATE_TEST_ROOT: empty registry, an installed
# update.sh copy next to the fake service.sh, and a scenario-local TMPDIR so the
# extracted archives of a killed update are cleaned up with everything else.
new_scenario() {
  local name=$1
  dir=$TOP/$name
  rm -rf -- "$dir"
  mkdir -p -- \
    "$dir/root/var/lib/proxycontrol-install" \
    "$dir/root/var/lib/proxy-panel" \
    "$dir/root/etc/proxy-panel" \
    "$dir/root/usr/local/bin" \
    "$dir/root/usr/local/lib/proxycontrol" \
    "$dir/root/usr/local/lib/proxy-agent" \
    "$dir/root/etc/systemd/system" \
    "$dir/root/etc/init.d" \
    "$dir/state/active" \
    "$dir/work" \
    "$dir/tmp"
  chmod 0700 -- "$dir/root/var/lib/proxycontrol-install"
  ROOT=$dir/root
  WORK=$dir/work
  REGISTRY=$ROOT/var/lib/proxycontrol-install
  BACKUPS=$ROOT/var/lib/proxy-panel/backups
  BUNDLE=''
  SUMS=''
  LAST_STATUS=0
  UPDATE_PID=''
  export QUI_UPDATE_TEST_ROOT=$ROOT
  export QUI_UPDATE_TEST_ACCOUNT=root
  export FAKE_STATE=$dir/state
  export FAKE_PANEL_DB=$ROOT/var/lib/proxy-panel/panel.db
  export FAKE_UPDATER_SOURCE=$WORK/qui-update
  export FAKE_SERVICE_SOURCE=$WORK/service.sh
  export TMPDIR=$dir/tmp
  install -m 0755 -- "$update_script" "$WORK/qui-update"
  install -m 0755 -- "$TOP/harness/service.sh" "$WORK/service.sh"
  : > "$FAKE_STATE/calls.log"
  printf 'database: %s\n' "$FAKE_PANEL_DB" > "$ROOT/etc/proxy-panel/config.yaml"
}

# Every fake knob back to its default, so a scenario cannot inherit the previous one's
# architecture (the scenarios only run in this file's subshells, but explicitness beats
# a subtle leak).
fake_reset() {
  unset FAKE_INIT FAKE_INSTALL FAKE_CHECK FAKE_TARGET FAKE_START_WRITES \
    FAKE_START_KILL FAKE_STOP_SLEEP FAKE_UNHEALTHY_VERSION FAKE_MULTI_CORE || true
  export FAKE_INSTALL=ok FAKE_CHECK=compatible
}


# Install a component the way deploy/linux/install.sh leaves it: program, service
# definition, updater, library, terminal menu or enforced-source pair, and the install
# record, with the service running.
install_component() {
  local component=$1 version=$2 init=${FAKE_INIT:-systemd} digest unit_digest program unit
  program=$ROOT/usr/local/bin/proxy-$component
  if [[ $init == systemd ]]; then
    unit=$ROOT/etc/systemd/system/proxy-$component.service
    printf '# fake systemd unit for proxy-%s %s\n' "$component" "$version" > "$unit"
    chmod 0644 -- "$unit"
  else
    unit=$ROOT/etc/init.d/proxy-$component
    printf '#!/sbin/openrc-run\n# fake init script for proxy-%s %s\nexit 0\n' \
      "$component" "$version" > "$unit"
    chmod 0755 -- "$unit"
  fi
  write_program "$program" "$component" "$version"
  install -m 0755 -- "$WORK/qui-update" "$ROOT/usr/local/bin/qui-update"
  install -m 0644 -- "$WORK/service.sh" "$ROOT/usr/local/lib/proxycontrol/service.sh"
  if [[ $component == panel ]]; then
    printf '#!/bin/sh\n# fake terminal menu of version %s\nexit 0\n' "$version" \
      > "$ROOT/usr/local/bin/qui"
    chmod 0755 -- "$ROOT/usr/local/bin/qui"
  elif [[ $init == systemd ]]; then
    printf '# fake enforced-source unit %s\n' "$version" \
      > "$ROOT/etc/systemd/system/proxy-agent-host-network.service"
    chmod 0644 -- "$ROOT/etc/systemd/system/proxy-agent-host-network.service"
    printf '# fake enforced-source path unit %s\n' "$version" \
      > "$ROOT/etc/systemd/system/proxy-agent-host-network.path"
    chmod 0644 -- "$ROOT/etc/systemd/system/proxy-agent-host-network.path"
  else
    printf '#!/sbin/openrc-run\n# fake enforced-source init script %s\nexit 0\n' "$version" \
      > "$ROOT/etc/init.d/proxy-agent-host-network"
    chmod 0755 -- "$ROOT/etc/init.d/proxy-agent-host-network"
    printf '#!/bin/sh\n# fake inotifyd watcher %s\nexit 0\n' "$version" \
      > "$ROOT/usr/local/lib/proxy-agent/host-network-watch"
    chmod 0755 -- "$ROOT/usr/local/lib/proxy-agent/host-network-watch"
  fi
  digest=$(sha256sum -- "$program")
  digest=${digest%% *}
  unit_digest=$(sha256sum -- "$unit")
  unit_digest=${unit_digest%% *}
  {
    printf 'proxycontrol-install-v1\n'
    printf '%s\n' "$component"
    printf '%s\n' "$digest"
    printf '%s\n' "$unit_digest"
    printf '\n'
    printf '\n'
    printf 'installed\n'
    if [[ $component == agent ]]; then printf '%s\n%s\n' "$version" "$version"; fi
    if [[ $init == openrc ]]; then printf 'openrc\n'; fi
  } > "$REGISTRY/$component.manifest"
  chmod 0600 -- "$REGISTRY/$component.manifest"
  : > "$FAKE_STATE/active/proxy-$component"
}

# The panel database the scenarios protect: plain text, nothing in these tests parses
# it as SQLite, and a copy of the original is kept outside the backups directory.
create_database() {
  printf 'PANEL-DATABASE-CONTENT\n' > "$FAKE_PANEL_DB"
  cp -- "$FAKE_PANEL_DB" "$dir/original.db"
}

# The files update.sh snapshots and restores for a component. This is a mirror of
# component_files in update.sh: it has to stay in sync with that function, otherwise a
# scenario would "pass" while a file of the component was left at the new version.
component_files_list() {
  local component=$1 init=${FAKE_INIT:-systemd}
  printf '%s\n' "$ROOT/usr/local/bin/proxy-$component"
  if [[ $init == systemd ]]; then
    printf '%s\n' "$ROOT/etc/systemd/system/proxy-$component.service"
  else
    printf '%s\n' "$ROOT/etc/init.d/proxy-$component"
  fi
  printf '%s\n' "$ROOT/usr/local/bin/qui-update" \
    "$ROOT/usr/local/lib/proxycontrol/service.sh"
  if [[ $component == panel ]]; then
    printf '%s\n' "$ROOT/usr/local/bin/qui"
  elif [[ $init == systemd ]]; then
    printf '%s\n' "$ROOT/etc/systemd/system/proxy-agent-host-network.service" \
      "$ROOT/etc/systemd/system/proxy-agent-host-network.path"
  else
    printf '%s\n' "$ROOT/etc/init.d/proxy-agent-host-network" \
      "$ROOT/usr/local/lib/proxy-agent/host-network-watch"
  fi
  if [[ $component == agent ]]; then
    printf '%s\n' "$ROOT/usr/local/lib/proxy-agent/xray" "$ROOT/usr/local/lib/proxy-agent/sing-box" "$ROOT/etc/proxy-agent/config.yaml"
  fi
  printf '%s\n' "$REGISTRY/$component.manifest"
}


# Record the digests of every file of a component, so a scenario can prove that a
# failed update left the host exactly as it was.
snapshot_file_hashes() {
  local component=$1 out=$2 path digest
  : > "$out"
  while IFS= read -r path; do
    if [[ -f $path && ! -L $path ]]; then
      digest=$(sha256sum -- "$path")
      printf '%s  %s\n' "${digest%% *}" "$path" >> "$out"
    else
      printf 'missing  %s\n' "$path" >> "$out"
    fi
  done < <(component_files_list "$component")
}

# Build the offline archive update.sh is pointed at: the two programs and the fake
# install.sh, plus a SHA256SUMS file naming the archive exactly as the release does.
make_bundle() {
  local version=$1 base name
  base=$dir/bundle/proxy-control-$version-linux-$arch
  name=proxy-control-$version-linux-$arch
  rm -rf -- "$dir/bundle"
  mkdir -p -- "$base/bin" "$base/deploy/linux"
  write_program "$base/bin/proxy-panel" panel "$version"
  write_program "$base/bin/proxy-agent" agent "$version"
  install -m 0755 -- "$TOP/harness/install.sh" "$base/deploy/linux/install.sh"
  BUNDLE=$dir/bundle/$name.tar.gz
  SUMS=$dir/bundle/SHA256SUMS
  ( cd -- "$dir/bundle" && tar -czf "$name.tar.gz" "$name" )
  # The digest line is written by hand in the format update.sh reads (digest, two
  # spaces, the archive name), so a sha256sum that marks binary input ("*NAME") cannot
  # change what SHA256SUMS looks like.
  ( cd -- "$dir/bundle" \
    && printf '%s  %s\n' "$(sha256sum -- "$name.tar.gz" | cut -d' ' -f1)" "$name.tar.gz" \
      > SHA256SUMS )
}

# --- assertions ------------------------------------------------------------
# Every expect_* records a reason instead of failing, so one scenario reports all of
# what is wrong, and the scenario subshell cannot be aborted by a broken expectation.
fail_note() { printf '%s\n' "$*" >> "$failure_file"; return 0; }

expect_status() { # description expected actual
  if [[ $3 != "$2" ]]; then fail_note "$1：退出状态是 $3，期望 $2"; fi
  return 0
}

expect_eq() { # description expected actual
  if [[ $3 != "$2" ]]; then fail_note "$1：是 \"$3\"，期望 \"$2\""; fi
  return 0
}

expect_exists() { # description path
  if [[ ! -e $2 ]]; then fail_note "$1：缺少 $2"; fi
  return 0
}

expect_absent() { # description path
  if [[ -e $2 || -L $2 ]]; then fail_note "$1：不应存在 $2"; fi
  return 0
}

expect_same() { # description file1 file2
  if ! cmp -s -- "$2" "$3"; then fail_note "$1：$2 与 $3 的内容不同"; fi
  return 0
}

expect_grep() { # description pattern file
  if [[ ! -f $3 ]] || ! grep -Fq -- "$2" "$3"; then
    fail_note "$1：$3 里没有 \"$2\""
  fi
  return 0
}

expect_same_tree() { # description before afte
  if ! cmp -s -- "$2" "$3"; then
    fail_note "$1：文件列表或内容与更新前不同（见 $2 与 $3）"
  fi
  return 0
}

expect_no_glob() { # description directory pattern
  local desc=$1 glob_dir=$2 pattern=$3 path found=''
  for path in "$glob_dir"/$pattern; do
    if [[ -e $path || -L $path ]]; then found="$found $path"; fi
  done
  if [[ -n $found ]]; then fail_note "$desc：仍然存在$found"; fi
  return 0
}

expect_glob_exists() { # description directory pattern -> sets FOUND_GLOB
  local desc=$1 glob_dir=$2 pattern=$3 path
  FOUND_GLOB=''
  for path in "$glob_dir"/$pattern; do
    if [[ -e $path || -L $path ]]; then FOUND_GLOB=$path; break; fi
  done
  if [[ -z $FOUND_GLOB ]]; then fail_note "$desc：没有 $glob_dir/$pattern"; fi
  return 0
}


# --- running the scripts under test ----------------------------------------

# The version the installed program of a component reports.
program_version() {
  local component=$1 output=''
  output=$("$ROOT/usr/local/bin/proxy-$component" --version 2>/dev/null || true)
  case $output in
    "proxy-$component "*) printf '%s' "${output#proxy-$component }" ;;
    *) printf 'unreadable' ;;
  esac
}

# active or inactive, according to the fake init system.
service_state_file() {
  if [[ -f $FAKE_STATE/active/proxy-$1 ]]; then printf 'active'; else printf 'inactive'; fi
}

# Print the start/stop/restart lines of the call log, so a scenario can compare "before"
# and "after" without the is-active/healthy/state noise.
record_service_calls() {
  grep -E '^(start|stop|restart) ' "$FAKE_STATE/calls.log" > "$1" 2>/dev/null || true
  return 0
}

# Run the update under test with the offline archive. The status lands in LAST_STATUS
# instead of aborting the scenario, because most scenarios expect it to be non-zero.
run_update() {
  local status=0
  timeout 60 bash "$WORK/qui-update" "$@" > "$dir/update.out" 2>&1 || status=$?
  LAST_STATUS=$status
  return 0
}

# Run the update to VERSION with --bundle/--sums/--yes, plus any extra arguments.
update_bundle() {
  local target=$1
  shift
  run_update --version "$target" --bundle "$BUNDLE" --sums "$SUMS" --yes "$@"
}

# Start the update in the background so a scenario can signal it; the wait below is
# bounded so a stuck run fails the scenario instead of hanging the test.
start_update_bg() {
  # A non-interactive bash starts background jobs with SIGINT and SIGQUIT ignored, and
  # a signal that is ignored when a shell starts cannot be trapped by it. A Ctrl-C from
  # a terminal reaches a foreground qui-update, which can trap it, so the job gets the
  # default disposition back (coreutils env, 8.31 and later) to look like that case.
  env --default-signal=INT bash "$WORK/qui-update" --version "$1" --bundle "$BUNDLE" --sums "$SUMS" --yes \
    > "$dir/update.out" 2>&1 &
  UPDATE_PID=$!
}

await_update_bg() {
  local i status=0
  for ((i = 0; i < 120; i += 1)); do
    if ! kill -0 "$UPDATE_PID" 2>/dev/null; then break; fi
    sleep 1
  done
  if kill -0 "$UPDATE_PID" 2>/dev/null; then
    kill -9 "$UPDATE_PID" 2>/dev/null || true
    fail_note '更新进程没有在 120 秒内结束'
  fi
  wait "$UPDATE_PID" || status=$?
  LAST_STATUS=$status
  UPDATE_PID=''
  return 0
}

# Poll a condition for a minute; a timeout is a scenario failure, not a hang.
wait_for() {
  local desc=$1 i
  shift
  for ((i = 0; i < 60; i += 1)); do
    if "$@"; then return 0; fi
    sleep 1
  done
  fail_note "等待超时：$desc"
  return 0
}

# The boot hook from the repository, as the boot unit runs it (only the test root is
# added to the environment; every fake knob is already exported by the scenario).
run_boot_recovery() {
  local status=0
  timeout 60 sh "$recovery_script" > "$dir/recovery.out" 2>&1 || status=$?
  LAST_STATUS=$status
  return 0
}

# A snapshot's own updater, exactly as update.sh and the boot hook run it.
run_snapshot_recovery() {
  local snapshot=$1 status=0
  timeout 60 bash "$snapshot/updater" --recover "$snapshot" --boot \
    > "$dir/recovery.out" 2>&1 || status=$?
  LAST_STATUS=$status
  return 0
}

# Hand-built snapshot whose journal already says "restored": a recovery may only remove
# the directory, without touching files or services.
make_restored_snapshot() {
  local snapshot=$1
  mkdir -p -- "$snapshot"
  chmod 0700 -- "$snapshot"
  install -m 0600 -- "$WORK/qui-update" "$snapshot/updater"
  install -m 0600 -- "$WORK/service.sh" "$snapshot/service.sh"
  {
    printf 'proxycontrol-update-journal-v1\n'
    printf 'component=panel\n'
    printf 'previous=1.0.3\n'
    printf 'target=1.0.4\n'
    printf 'was_active=true\n'
    printf 'backup=\n'
    printf 'database_source=\n'
    printf 'phase=restored\n'
  } > "$snapshot/journal"
  chmod 0600 -- "$snapshot/journal"
}


# --- scenarios -------------------------------------------------------------

# 1. A plain update of both components succeeds and leaves nothing behind.
scenario_1() {
  new_scenario scenario-1
  fake_reset
  install_component agent 1.0.3
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  update_bundle 1.0.4
  expect_status '两个组件更新成功' 0 "$LAST_STATUS"
  expect_eq 'agent 版本' 1.0.4 "$(program_version agent)"
  expect_eq 'panel 版本' 1.0.4 "$(program_version panel)"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_no_glob '不应留下待删除目录' "$REGISTRY" 'discard.*'
  expect_glob_exists '数据库备份' "$BACKUPS" 'pre-update-1.0.3-*.db'
  if [[ -n $FOUND_GLOB ]]; then expect_same '数据库备份内容' "$dir/original.db" "$FOUND_GLOB"; fi
  expect_no_glob '备份目录里不应有 partial' "$BACKUPS" '*.partial'
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
  expect_eq 'agent 运行中' active "$(service_state_file agent)"
}

# 2. install.sh fails after replacing the program: everything goes back, the database
#    is untouched and the panel is started again.
scenario_2() {
  new_scenario scenario-2
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_INSTALL=fail-after-replace
  update_bundle 1.0.4
  expect_status 'install.sh 失败后退出' 1 "$LAST_STATUS"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '文件应回滚' "$dir/before.sha" "$dir/after.sha"
  expect_same '数据库不应变化' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_no_glob '不应保留 failed-update 备份' "$BACKUPS" 'failed-update-*'
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_grep 'panel 应被重新启动' 'start proxy-panel' "$FAKE_STATE/calls.log"
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
}

# 3. The new version starts but does not stay healthy: files and database go back, and
#    the migrated database is kept under failed-update-<target>.
scenario_3() {
  new_scenario scenario-3
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_START_WRITES=1 FAKE_TARGET=1.0.4 FAKE_UNHEALTHY_VERSION=1.0.4
  update_bundle 1.0.4
  expect_status '新版本不健康导致失败' 1 "$LAST_STATUS"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '文件应回滚' "$dir/before.sha" "$dir/after.sha"
  expect_same '数据库应恢复' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_glob_exists '更新后的数据库应被保留' "$BACKUPS" 'failed-update-1.0.4-*.db'
  if [[ -n $FOUND_GLOB ]]; then expect_grep '保留的数据库应是迁移后的' 'MIGRATED' "$FOUND_GLOB"; fi
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
}

# 4. Power loss while install.sh runs (journal at "prepared"): the boot hook restores
#    the files and the database and starts nothing.
scenario_4() {
  new_scenario scenario-4
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_INSTALL=kill-parent
  update_bundle 1.0.4
  expect_status '被 SIGKILL 结束' 137 "$LAST_STATUS"
  expect_glob_exists '应留下快照' "$REGISTRY" 'rollback.*'
  local snapshot=$FOUND_GLOB
  expect_exists '快照应有进度日志' "$snapshot/journal"
  if [[ -f $snapshot/journal ]]; then
    expect_grep '阶段应是 prepared' 'phase=prepared' "$snapshot/journal"
  fi
  # What a killed install.sh leaves behind: its staging directory, next to the lock file
  # it holds while it runs. Nobody holds the lock now, so the recovery removes it.
  mkdir -p -- "$REGISTRY/staging.killed"
  printf 'x\n' > "$REGISTRY/staging.killed/program"
  [[ -e $REGISTRY/operation.lock ]] || { : > "$REGISTRY/operation.lock"; chmod 0600 "$REGISTRY/operation.lock"; }
  record_service_calls "$dir/calls-before.log"
  run_boot_recovery
  expect_status '开机恢复成功' 0 "$LAST_STATUS"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '文件应回滚' "$dir/before.sha" "$dir/after.sha"
  expect_same '数据库不应变化' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_no_glob '快照应被移除' "$REGISTRY" 'rollback.*'
  expect_no_glob '被杀掉的 install.sh 的暂存目录应被移除' "$REGISTRY" 'staging.*'
  record_service_calls "$dir/calls-after.log"
  expect_same '开机恢复不应启动或停止服务' "$dir/calls-before.log" "$dir/calls-after.log"
  expect_exists '开机恢复应写日志' "$REGISTRY/update-recovery.log"
}

# 5. Power loss right after the new panel started, so it already migrated: the boot
#    hook restores the files and the pre-update database and keeps the migrated one.
scenario_5() {
  new_scenario scenario-5
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_START_WRITES=1 FAKE_START_KILL=1 FAKE_TARGET=1.0.4
  update_bundle 1.0.4
  expect_status '启动后被 SIGKILL 结束' 137 "$LAST_STATUS"
  expect_glob_exists '应留下快照' "$REGISTRY" 'rollback.*'
  local snapshot=$FOUND_GLOB
  if [[ -f $snapshot/journal ]]; then
    expect_grep '阶段应是 started' 'phase=started' "$snapshot/journal"
  fi
  # An install.sh that holds operation.lock is still running, so its staging directory
  # is its own and has to stay.
  mkdir -p -- "$REGISTRY/staging.running"
  [[ -e $REGISTRY/operation.lock ]] || { : > "$REGISTRY/operation.lock"; chmod 0600 "$REGISTRY/operation.lock"; }
  flock "$REGISTRY/operation.lock" sleep 20 &
  local holder=$!
  sleep 1
  run_boot_recovery
  kill "$holder" 2>/dev/null || true
  wait "$holder" 2>/dev/null || true
  expect_exists '持有 operation.lock 时暂存目录应保留' "$REGISTRY/staging.running"
  expect_status '开机恢复成功' 0 "$LAST_STATUS"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '文件应回滚' "$dir/before.sha" "$dir/after.sha"
  expect_same '数据库应恢复' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_glob_exists '迁移后的数据库应被保留' "$BACKUPS" 'failed-update-1.0.4-*.db'
  if [[ -n $FOUND_GLOB ]]; then expect_grep '保留的数据库应是迁移后的' 'MIGRATED' "$FOUND_GLOB"; fi
  expect_no_glob '快照应被移除' "$REGISTRY" 'rollback.*'
}


# 6. Ctrl-C while install.sh is running: the same recovery runs inline.
scenario_6() {
  new_scenario scenario-6
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_INSTALL=sleep
  start_update_bg 1.0.4
  wait_for 'install.sh 开始替换文件' test -f "$FAKE_STATE/install.started"
  kill -INT "$UPDATE_PID" 2>/dev/null || true
  await_update_bg
  expect_status 'SIGINT 退出 130' 130 "$LAST_STATUS"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '文件应回滚' "$dir/before.sha" "$dir/after.sha"
  expect_same '数据库不应变化' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_no_glob '备份目录里不应有 partial' "$BACKUPS" '*.partial'
  expect_grep 'panel 应被重新启动' 'start proxy-panel' "$FAKE_STATE/calls.log"
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
}

# 7. SIGHUP (the SSH session went away): status 129 and a report in the registry,
#    because there is no terminal left to write to.
scenario_7() {
  new_scenario scenario-7
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_INSTALL=sleep
  start_update_bg 1.0.4
  wait_for 'install.sh 开始替换文件' test -f "$FAKE_STATE/install.started"
  kill -HUP "$UPDATE_PID" 2>/dev/null || true
  await_update_bg
  expect_status 'SIGHUP 退出 129' 129 "$LAST_STATUS"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '文件应回滚' "$dir/before.sha" "$dir/after.sha"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_exists '应写中断日志' "$REGISTRY/interrupted-update.log"
  expect_grep '中断日志应说明原因' '中断' "$REGISTRY/interrupted-update.log"
}

# 8. SIGTERM while the panel is still being stopped, before any snapshot exists: the
#    service is the whole undo, so it has to be started again.
scenario_8() {
  new_scenario scenario-8
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  export FAKE_STOP_SLEEP=1
  start_update_bg 1.0.4
  wait_for 'panel 正在停止' grep -Fqx 'stop proxy-panel' "$FAKE_STATE/calls.log"
  kill -TERM "$UPDATE_PID" 2>/dev/null || true
  await_update_bg
  expect_status 'SIGTERM 退出 143' 143 "$LAST_STATUS"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_no_glob '备份目录里不应有 partial' "$BACKUPS" '*.partial'
  expect_same '数据库不应变化' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_grep 'panel 应被重新启动' 'start proxy-panel' "$FAKE_STATE/calls.log"
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
}

# 9. A snapshot a killed run left behind is recovered by the next run, which then
#    completes the update it was asked for.
scenario_9() {
  new_scenario scenario-9
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  export FAKE_INSTALL=kill-parent
  update_bundle 1.0.4
  expect_status '第一次运行被 SIGKILL 结束' 137 "$LAST_STATUS"
  expect_glob_exists '应留下快照' "$REGISTRY" 'rollback.*'
  export FAKE_INSTALL=ok
  update_bundle 1.0.4
  expect_status '第二次运行成功' 0 "$LAST_STATUS"
  expect_grep '应报告被中断的更新' '发现被中断的更新' "$dir/update.out"
  expect_eq 'panel 版本' 1.0.4 "$(program_version panel)"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_no_glob '不应留下待删除目录' "$REGISTRY" 'discard.*'
  expect_same '数据库不应变化' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
}

# 10. Recovery is idempotent: running it twice converges, and a snapshot that already
#     says "restored" is only removed.
scenario_10() {
  new_scenario scenario-10
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_INSTALL=kill-parent
  update_bundle 1.0.4
  expect_status '被 SIGKILL 结束' 137 "$LAST_STATUS"
  expect_glob_exists '应留下快照' "$REGISTRY" 'rollback.*'
  local snapshot=$FOUND_GLOB
  cp -a -- "$snapshot" "$dir/snapshot-aside"
  run_snapshot_recovery "$snapshot"
  expect_status '第一次恢复' 0 "$LAST_STATUS"
  rm -rf -- "$snapshot"
  cp -a -- "$dir/snapshot-aside" "$snapshot"
  run_snapshot_recovery "$snapshot"
  expect_status '第二次恢复（重复）' 0 "$LAST_STATUS"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '文件应保持回滚后的状态' "$dir/before.sha" "$dir/after.sha"
  expect_no_glob '快照应被移除' "$REGISTRY" 'rollback.*'
  make_restored_snapshot "$REGISTRY/rollback.restored"
  record_service_calls "$dir/calls-before.log"
  snapshot_file_hashes panel "$dir/before-restored.sha"
  run_snapshot_recovery "$REGISTRY/rollback.restored"
  expect_status '已恢复的快照' 0 "$LAST_STATUS"
  expect_absent '已恢复的快照应被移除' "$REGISTRY/rollback.restored"
  snapshot_file_hashes panel "$dir/after-restored.sha"
  expect_same_tree '已恢复的快照不应改动文件' "$dir/before-restored.sha" "$dir/after-restored.sha"
  record_service_calls "$dir/calls-after.log"
  expect_same '已恢复的快照不应调用服务' "$dir/calls-before.log" "$dir/calls-after.log"
}


# 11. A second run refuses while another one holds the update lock, and changes nothing.
scenario_11() {
  new_scenario scenario-11
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  flock "$REGISTRY/update.lock" sleep 20 &
  local holder=$!
  wait_for '更新锁被占用' test -f "$REGISTRY/update.lock"
  update_bundle 1.0.4
  expect_status '应拒绝并发运行' 1 "$LAST_STATUS"
  expect_grep '应说明锁被占用' '另一个 qui-update 正在运行' "$dir/update.out"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '不应改动文件' "$dir/before.sha" "$dir/after.sha"
  expect_same '数据库不应变化' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_eq '不应停止服务' '' "$(grep -F 'stop proxy-panel' "$FAKE_STATE/calls.log" || true)"
  kill "$holder" 2>/dev/null || true
  wait "$holder" 2>/dev/null || true
}

# 12. Downgrading the panel together with a database copy: the copy is installed, the
#     previous database is kept as a pre-update backup, and the copy itself survives.
scenario_12() {
  new_scenario scenario-12
  fake_reset
  install_component panel 1.0.4
  create_database
  printf 'OLD-SCHEMA\n' > "$dir/downgrade.db"
  cp -- "$dir/downgrade.db" "$dir/downgrade.db.original"
  make_bundle 1.0.3
  update_bundle 1.0.3 --database "$dir/downgrade.db"
  expect_status '降级成功' 0 "$LAST_STATUS"
  expect_eq 'panel 版本' 1.0.3 "$(program_version panel)"
  expect_same '数据库应换成 --database 指定的副本' "$dir/downgrade.db" "$FAKE_PANEL_DB"
  expect_same '--database 文件不应被修改' "$dir/downgrade.db.original" "$dir/downgrade.db"
  expect_glob_exists '应保留降级前的数据库' "$BACKUPS" 'pre-update-1.0.4-*.db'
  if [[ -n $FOUND_GLOB ]]; then expect_same '保留的数据库应是降级前的内容' "$dir/original.db" "$FOUND_GLOB"; fi
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
}

# 12b. The same downgrade behind an unhealthy new version: files and database go back
#      to the version that ran before.
scenario_12b() {
  new_scenario scenario-12b
  fake_reset
  install_component panel 1.0.4
  create_database
  printf 'OLD-SCHEMA\n' > "$dir/downgrade.db"
  make_bundle 1.0.3
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_UNHEALTHY_VERSION=1.0.3
  update_bundle 1.0.3 --database "$dir/downgrade.db"
  expect_status '降级后不健康应失败' 1 "$LAST_STATUS"
  expect_eq 'panel 版本应回到 1.0.4' 1.0.4 "$(program_version panel)"
  expect_same '数据库应回到降级前' "$dir/original.db" "$FAKE_PANEL_DB"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '文件应回滚到 1.0.4' "$dir/before.sha" "$dir/after.sha"
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
}

# 13. --database is refused for an upgrade, before anything is stopped.
scenario_13() {
  new_scenario scenario-13
  fake_reset
  install_component panel 1.0.3
  create_database
  printf 'OLD-SCHEMA\n' > "$dir/downgrade.db"
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  update_bundle 1.0.4 --database "$dir/downgrade.db"
  expect_status '升级时 --database 应被拒绝' 1 "$LAST_STATUS"
  expect_grep '应说明 --database 只用于降级' '只用于把面板降级' "$dir/update.out"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '不应改动文件' "$dir/before.sha" "$dir/after.sha"
  expect_same '数据库不应变化' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_eq '不应停止服务' '' "$(grep -F 'stop proxy-panel' "$FAKE_STATE/calls.log" || true)"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
}

# 14. The target cannot open the database and no --database was given: the update stops
#     before the service does, and the output says which copy to use.
scenario_14() {
  new_scenario scenario-14
  fake_reset
  install_component panel 1.0.4
  create_database
  make_bundle 1.0.3
  mkdir -p -- "$BACKUPS"
  printf 'OLDER\n' > "$BACKUPS/pre-update-1.0.3-20260101-000000.db"
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_CHECK=newer
  update_bundle 1.0.3
  expect_status '数据库更新的目标版本应失败' 1 "$LAST_STATUS"
  expect_grep '应列出可用的备份' 'pre-update-1.0.3-20260101-000000.db' "$dir/update.out"
  expect_grep '应提示 --database' '--database' "$dir/update.out"
  expect_no_glob '被拒绝时不应留下多余的数据库备份' "$BACKUPS" 'pre-update-1.0.4-*.db'
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '不应改动文件' "$dir/before.sha" "$dir/after.sha"
  expect_same '数据库不应变化' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
  expect_grep 'panel 应被重新启动' 'start proxy-panel' "$FAKE_STATE/calls.log"
}

# 15. The target is too old to know --check-database: the update goes on and says so.
scenario_15() {
  new_scenario scenario-15
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  export FAKE_CHECK=unsupported
  update_bundle 1.0.4
  expect_status '目标版本不支持检查时应继续' 0 "$LAST_STATUS"
  expect_grep '应说明跳过预检' '跳过预检' "$dir/update.out"
  expect_eq 'panel 版本' 1.0.4 "$(program_version panel)"
}

# 16. The pre-update copy is written atomically: install.sh sees the final name and no
#     .partial file next to it.
scenario_16() {
  new_scenario scenario-16
  fake_reset
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  export FAKE_INSTALL=probe
  update_bundle 1.0.4
  expect_status '更新成功' 0 "$LAST_STATUS"
  expect_exists '应记录安装时的备份目录' "$FAKE_STATE/backups-at-install.txt"
  expect_grep '备份应是最终名字' 'pre-update-1.0.3-' "$FAKE_STATE/backups-at-install.txt"
  expect_eq '安装时不应有 partial 文件' '' \
    "$(grep -F 'partial' "$FAKE_STATE/backups-at-install.txt" || true)"
}


# 17. The same three paths on an OpenRC host: /etc/init.d instead of units, and the
#     enforced-source pair of the agent.
scenario_17a() {
  new_scenario scenario-17a
  fake_reset
  export FAKE_INIT=openrc
  install_component agent 1.0.3
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  update_bundle 1.0.4
  expect_status 'OpenRC 上更新成功' 0 "$LAST_STATUS"
  expect_eq 'agent 版本' 1.0.4 "$(program_version agent)"
  expect_eq 'panel 版本' 1.0.4 "$(program_version panel)"
  expect_eq '记录第 10 行应是 openrc' openrc "$(sed -n '10p' "$REGISTRY/agent.manifest")"
  expect_exists '应使用 init.d 目录' "$ROOT/etc/init.d/proxy-panel"
  expect_exists '应更新 watcher' "$ROOT/usr/local/lib/proxy-agent/host-network-watch"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_glob_exists '数据库备份' "$BACKUPS" 'pre-update-1.0.3-*.db'
  if [[ -n $FOUND_GLOB ]]; then expect_same '数据库备份内容' "$dir/original.db" "$FOUND_GLOB"; fi
}

scenario_17b() {
  new_scenario scenario-17b
  fake_reset
  export FAKE_INIT=openrc
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes panel "$dir/before.sha"
  export FAKE_START_WRITES=1 FAKE_TARGET=1.0.4 FAKE_UNHEALTHY_VERSION=1.0.4
  update_bundle 1.0.4
  expect_status 'OpenRC 上新版本不健康' 1 "$LAST_STATUS"
  snapshot_file_hashes panel "$dir/after.sha"
  expect_same_tree '文件应回滚' "$dir/before.sha" "$dir/after.sha"
  expect_same '数据库应恢复' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_glob_exists '迁移后的数据库应被保留' "$BACKUPS" 'failed-update-1.0.4-*.db'
  expect_eq 'panel 运行中' active "$(service_state_file panel)"
}

scenario_17c() {
  new_scenario scenario-17c
  fake_reset
  export FAKE_INIT=openrc
  install_component agent 1.0.3
  install_component panel 1.0.3
  create_database
  make_bundle 1.0.4
  snapshot_file_hashes agent "$dir/agent-before.sha"
  snapshot_file_hashes panel "$dir/panel-before.sha"
  export FAKE_INSTALL=kill-parent
  update_bundle 1.0.4
  expect_status '断电（SIGKILL）退出 137' 137 "$LAST_STATUS"
  record_service_calls "$dir/calls-before.log"
  run_boot_recovery
  expect_status 'OpenRC 开机恢复成功' 0 "$LAST_STATUS"
  snapshot_file_hashes agent "$dir/agent-after.sha"
  expect_same_tree 'agent 文件应回滚' "$dir/agent-before.sha" "$dir/agent-after.sha"
  snapshot_file_hashes panel "$dir/panel-after.sha"
  expect_same_tree 'panel 文件不应改动' "$dir/panel-before.sha" "$dir/panel-after.sha"
  expect_same '数据库不应变化' "$dir/original.db" "$FAKE_PANEL_DB"
  expect_no_glob '快照应被移除' "$REGISTRY" 'rollback.*'
  record_service_calls "$dir/calls-after.log"
  expect_same '开机恢复不应启动或停止服务' "$dir/calls-before.log" "$dir/calls-after.log"
  expect_exists '开机恢复应写日志' "$REGISTRY/update-recovery.log"
}

# 18. A node host without a panel: the agent alone, and the same power-loss recovery.
scenario_18a() {
  new_scenario scenario-18a
  fake_reset
  install_component agent 1.0.3
  make_bundle 1.0.4
  update_bundle 1.0.4
  expect_status '仅 agent 的主机更新成功' 0 "$LAST_STATUS"
  expect_eq 'agent 版本' 1.0.4 "$(program_version agent)"
  expect_no_glob '不应留下快照' "$REGISTRY" 'rollback.*'
  expect_absent '不应创建备份目录' "$BACKUPS"
  expect_eq 'agent 运行中' active "$(service_state_file agent)"
}

scenario_18b() {
  new_scenario scenario-18b
  fake_reset
  install_component agent 1.0.3
  make_bundle 1.0.4
  snapshot_file_hashes agent "$dir/before.sha"
  export FAKE_INSTALL=kill-parent
  update_bundle 1.0.4
  expect_status '被 SIGKILL 结束' 137 "$LAST_STATUS"
  expect_glob_exists '应留下快照' "$REGISTRY" 'rollback.*'
  local snapshot=$FOUND_GLOB
  if [[ -f $snapshot/journal ]]; then
    expect_grep '阶段应是 prepared' 'phase=prepared' "$snapshot/journal"
    expect_grep '组件应是 agent' 'component=agent' "$snapshot/journal"
  fi
  run_boot_recovery
  expect_status '开机恢复成功' 0 "$LAST_STATUS"
  snapshot_file_hashes agent "$dir/after.sha"
  expect_same_tree 'agent 文件应回滚' "$dir/before.sha" "$dir/after.sha"
  expect_no_glob '快照应被移除' "$REGISTRY" 'rollback.*'
}


# RC updates on a combined host: only the selected component participates in
# install/rollback/recovery. Shared updater/library files belong to either update.
scenario_19() {
  local selected other outcome
  for selected in panel agent; do
    if [[ $selected == panel ]]; then other=agent; else other=panel; fi
    for outcome in ok fail-after-replace kill-parent; do
      new_scenario "scenario-19-$selected-$outcome"
      fake_reset
      install_component agent 1.0.9
      install_component panel 1.0.9
      create_database
      make_bundle 1.1.0-rc.1
      snapshot_file_hashes "$selected" "$dir/selected-before.sha"
      # Exclude the two shared files, but include the other program, unit,
      # manifest, panel menu or Agent host-network pair.
      snapshot_file_hashes "$other" "$dir/all-before.sha"
      grep -vE '/(qui-update|service.sh)$' "$dir/all-before.sha" > "$dir/other-before.sha"
      cp -- "$FAKE_PANEL_DB" "$dir/db-before"
      export FAKE_INSTALL=$outcome
      update_bundle 1.1.0-rc.1 --component "$selected"
      case $outcome in
        ok) expect_status 'RC component update' 0 "$LAST_STATUS"
            expect_eq 'selected RC version' 1.1.0-rc.1 "$(program_version "$selected")";;
        fail-after-replace) expect_status 'failed install rolled back' 1 "$LAST_STATUS";;
        kill-parent) expect_status 'SIGKILL' 137 "$LAST_STATUS"
                     expect_glob_exists 'pending RC journal' "$REGISTRY" 'rollback.*'
                     expect_grep 'selected journal component' "component=$selected" "$FOUND_GLOB/journal"
                     expect_grep 'RC target' 'target=1.1.0-rc.1' "$FOUND_GLOB/journal"
                     run_boot_recovery
                     expect_status 'RC boot recovery' 0 "$LAST_STATUS";;
      esac
      if [[ $outcome != ok ]]; then
        snapshot_file_hashes "$selected" "$dir/selected-after.sha"
        expect_same_tree 'selected files restored' "$dir/selected-before.sha" "$dir/selected-after.sha"
      fi
      snapshot_file_hashes "$other" "$dir/all-after.sha"
      grep -vE '/(qui-update|service.sh)$' "$dir/all-after.sha" > "$dir/other-after.sha"
      expect_same_tree 'other component untouched' "$dir/other-before.sha" "$dir/other-after.sha"
      expect_eq 'other version unchanged' 1.0.9 "$(program_version "$other")"
      if grep -E "^(start|stop|restart) proxy-$other$" "$FAKE_STATE/calls.log"; then
        printf 'other service was touched\n' >> "$failure_file"
      fi
      if [[ $selected == agent ]]; then
        expect_same 'agent update leaves database alone' "$dir/db-before" "$FAKE_PANEL_DB"
        expect_absent 'no panel backup' "$BACKUPS"
      fi
    done
  done
}

scenario_20() {
  new_scenario scenario-20
  fake_reset
  install_component panel 1.1.0
  create_database
  make_bundle 1.1.0-rc.2
  local backup=$BACKUPS/pre-update-1.0.9-20261028-010101.db
  mkdir -p -- "$BACKUPS"
  cp -- "$FAKE_PANEL_DB" "$backup"
  update_bundle 1.1.0-rc.2 --component panel --database "$backup"
  expect_status 'stable to RC is a downgrade with database' 0 "$LAST_STATUS"
  make_bundle 1.1.0-rc.1
  update_bundle 1.1.0-rc.1 --component panel --database "$backup"
  expect_status 'RC2 to RC1 is a downgrade' 0 "$LAST_STATUS"
  make_bundle 1.1.0-rc.2
  update_bundle 1.1.0-rc.2 --component panel --database "$backup"
  expect_status 'RC1 to RC2 database downgrade option refused' 1 "$LAST_STATUS"
  update_bundle 1.1.0-rc.2 --component agent
  expect_status 'uninstalled selected component refused' 1 "$LAST_STATUS"
  run_update --component both
  expect_status 'invalid component refused' 1 "$LAST_STATUS"
  run_update --component
  expect_status 'missing component value refused' 1 "$LAST_STATUS"
}

scenario_21() {
  new_scenario scenario-21
  fake_reset
  install_component agent 1.0.9
  install_component panel 1.1.0-rc.1
  create_database
  make_bundle 1.1.0-rc.1
  update_bundle 1.1.0-rc.1 --component panel
  expect_status 'selected panel already up to date' 0 "$LAST_STATUS"
  expect_grep 'selected version equality is a no-op' '已是最新版本 1.1.0-rc.1' "$dir/update.out"
  expect_eq 'old unselected Agent stays old' 1.0.9 "$(program_version agent)"
  expect_absent 'no database backup on no-op' "$BACKUPS"
  update_bundle 1.1.0-rc.1 --component agent --database "$FAKE_PANEL_DB"
  expect_status 'agent selection refuses database option' 1 "$LAST_STATUS"
  expect_grep 'database refusal reason' '--database 不能与 --component agent 一起使用' "$dir/update.out"
  run_update --recover "$REGISTRY/rollback.invalid" --component panel
  expect_status 'component cannot override a recovery journal' 1 "$LAST_STATUS"
  expect_grep 'recovery option refusal reason' '--recover 不能与更新参数一起使用' "$dir/update.out"
  make_bundle 1.1.0-rc.2
  update_bundle 1.1.0-rc.2
  expect_status 'default RC update both components' 0 "$LAST_STATUS"
  expect_eq 'default order still Agent first' 'stop proxy-agent' "$(grep '^stop ' "$FAKE_STATE/calls.log" | head -n1)"
  expect_eq 'agent RC2' 1.1.0-rc.2 "$(program_version agent)"
  expect_eq 'panel RC2' 1.1.0-rc.2 "$(program_version panel)"
}

# --- runner ----------------------------------------------------------------
# Exercise both binaries + configuration + v2 manifest across inline rollback
# and SIGKILL/boot recovery; paths are part of the before/after tree comparison.
scenario_22() {
  local outcome init core
  for init in systemd openrc; do
    for outcome in ok fail-after-replace kill-parent; do
      new_scenario "scenario-22-$init-$outcome"
      fake_reset
      export FAKE_INIT=$init FAKE_MULTI_CORE=1
      install_component agent 1.1.0
      mkdir -p "$ROOT/etc/proxy-agent"
      for core in xray sing-box; do printf 'original-%s\n' "$core" > "$ROOT/usr/local/lib/proxy-agent/$core"; done
      printf 'core_name: xray\ncores: [original]\n' > "$ROOT/etc/proxy-agent/config.yaml"
      # Turn the legacy fixture into a real v2 record with both core digests.
      . "$repo_dir/deploy/linux/service.sh"
      component=agent; init_system=$init
      digest=$(sed -n '3p' "$REGISTRY/agent.manifest")
      unit_digest=$(sed -n '4p' "$REGISTRY/agent.manifest")
      host_network_service_digest=''; host_network_path_digest=''
      tuning_service_digest=''; tuning_path_digest=''
      record_cores=(xray sing-box); record_digests=()
      for core in xray sing-box; do
        value=$(sha256sum "$ROOT/usr/local/lib/proxy-agent/$core"); record_digests+=("${value%% *}")
      done
      qui_write_record "$REGISTRY/agent.manifest"
      # Reload fake service implementations after using the real record writer.
      . "$WORK/service.sh"
      make_bundle 1.2.0
      snapshot_file_hashes agent "$dir/before.sha"
      export FAKE_INSTALL=$outcome
      update_bundle 1.2.0 --component agent
      if [[ $outcome == kill-parent ]]; then
        expect_status 'multi-core SIGKILL' 137 "$LAST_STATUS"
        run_boot_recovery
        expect_status 'multi-core boot recovery' 0 "$LAST_STATUS"
      elif [[ $outcome == ok ]]; then
        expect_status 'multi-core successful carry-forward' 0 "$LAST_STATUS"
        qui_record_cores "$REGISTRY/agent.manifest" agent
        expect_eq 'all cores retained' 'xray sing-box' "${record_cores[*]}"
        expect_eq 'v2 retained' proxycontrol-install-v2 "$(sed -n '1p' "$REGISTRY/agent.manifest")"
        continue
      else expect_status 'multi-core failed install rollback' 1 "$LAST_STATUS"; fi
      snapshot_file_hashes agent "$dir/after.sha"
      expect_same_tree 'both cores, config and v2 record restored' "$dir/before.sha" "$dir/after.sha"
      unset FAKE_MULTI_CORE
    done
  done
}

scenario_23() {
  new_scenario scenario-23
  fake_reset
  install_component agent 1.1.0
  printf 'legacy-xray\n' > "$ROOT/usr/local/lib/proxy-agent/xray"
  local legacy_digest
  legacy_digest=$(sha256sum "$ROOT/usr/local/lib/proxy-agent/xray"); legacy_digest=${legacy_digest%% *}
  sed -i "5s/.*/xray/;6s/.*/$legacy_digest/" "$REGISTRY/agent.manifest"
  make_bundle 1.2.0
  update_bundle 1.2.0 --component agent
  expect_status 'v1 single core update success' 0 "$LAST_STATUS"
  expect_eq 'v1 to v2 format' proxycontrol-install-v2 "$(sed -n '1p' "$REGISTRY/agent.manifest")"
  . "$WORK/service.sh"
  qui_record_cores "$REGISTRY/agent.manifest" agent
  expect_eq 'legacy core carried forward' xray "${record_cores[*]}"
  expect_eq 'legacy digest carried forward' "$legacy_digest" "${record_digests[*]}"
}

# Each scenario runs in its own subshell with errexit on, so an unexpected failure ends
# that scenario only; the reasons it collected are reported together.
passes=0
failed=0

run_scenario() {
  local name=$1 body=$2 status=0 reasons=''
  : > "$failure_file"
  ( set -e; "$body" ) || status=$?
  if [[ -s $failure_file ]]; then
    reasons=$(tr '\n' ' ' < "$failure_file")
  fi
  if ((status != 0)); then
    reasons="${reasons}场景异常结束（退出状态 ${status}）"
  fi
  if [[ -n $reasons ]]; then
    printf 'FAIL %s: %s\n' "$name" "$reasons"
    printf '     临时目录：%s\n' "$TOP"
    failed=$((failed + 1))
  else
    printf 'PASS %s\n' "$name"
    passes=$((passes + 1))
  fi
}

write_fake_service_library "$TOP/harness/service.sh"
sed -n '/^# Release versions shared/,$p' "$repo_dir/deploy/linux/service.sh" >> "$TOP/harness/service.sh"
write_fake_install "$TOP/harness/install.sh"

printf 'update-test: root %s, arch %s, update.sh %s\n' \
  "$TOP" "$arch" "$update_script"

run_scenario '22 multi-core rollback and boot recovery' scenario_22
run_scenario '23 v1 single core to v2 migration' scenario_23
run_scenario '1 success' scenario_1
run_scenario '2 install fails after replacing' scenario_2
run_scenario '3 unhealthy new version' scenario_3
run_scenario '4 power loss at prepared' scenario_4
run_scenario '5 power loss at started' scenario_5
run_scenario '6 SIGINT during install' scenario_6
run_scenario '7 SIGHUP during install' scenario_7
run_scenario '8 SIGTERM before the snapshot' scenario_8
run_scenario '9 pending journal picked up by the next run' scenario_9
run_scenario '10 idempotent recovery' scenario_10
run_scenario '11 concurrent run refused' scenario_11
run_scenario '12 downgrade with --database' scenario_12
run_scenario '12b downgrade with --database, unhealthy' scenario_12b
run_scenario '13 --database refused for an upgrade' scenario_13
run_scenario '14 pre-flight newer without --database' scenario_14
run_scenario '15 pre-flight unsupported' scenario_15
run_scenario '16 backups written atomically' scenario_16
run_scenario '17a OpenRC success' scenario_17a
run_scenario '17b OpenRC unhealthy new version' scenario_17b
run_scenario '17c OpenRC power loss at prepared' scenario_17c
run_scenario '18a agent-only success' scenario_18a
run_scenario '18b agent-only power loss at prepared' scenario_18b
run_scenario '19 RC selected component success/rollback/recovery' scenario_19
run_scenario '20 RC downgrade precedence and component refusals' scenario_20
run_scenario '21 component up-to-date scope and default RC order' scenario_21

printf '\nupdate-test: %s passed, %s failed\n' "$passes" "$failed"
if ((failed > 0)); then exit 1; fi
exit 0
