#!/usr/bin/env bash
# Called by TestTuningProbeLifecycle. Run the real installer activation block or
# updater recovery in a newly created tree. Record init jobs, and execute the
# packaged OpenRC watcher with a file-only helper, stopping before inotifyd.
set -euo pipefail
scenario=$1
fixture=$(cd -- "$2" && pwd -P)
repo=$(cd -- "$(dirname -- "$0")/../../.." && pwd -P)
export QUI_UPDATE_TEST_ROOT=$fixture/host
mkdir -- "$QUI_UPDATE_TEST_ROOT"
. "$repo/deploy/linux/update.sh"
. "$repo/deploy/linux/service.sh"
snapshot=$registry/rollback.fixture
data=$root/var/lib/proxy-agent
systemd_dir=$root/etc/systemd/system
run_dir=$root/run/proxy-agent-tuning
mkdir -p "$snapshot/files" "$systemd_dir" "$data/tuning" "$run_dir" "$root/etc/init.d"
cp "$fixture/old-probe.json" "$data/tuning/probe.json"
printf 'retained failed profile\n' > "$data/tuning/desired.json"
component=agent
init_system=systemd
mode=interactive
stage=$fixture/stage
service_mode=0644
tuning_service=$systemd_dir/proxy-agent-tuning.service
tuning_path=$systemd_dir/proxy-agent-tuning.path
tuning_probe=$systemd_dir/proxy-agent-tuning-probe.service
tuning_probe_reset=$systemd_dir/proxy-agent-tuning-probe-reset.service
touch "$tuning_service" "$tuning_path" "$tuning_probe" "$tuning_probe_reset"
calls=$fixture/jobs
: > "$calls"
service_is_active() { return 1; }
service_reload_definitions() { :; }
service_stop() { :; }
service_state() { printf 'inactive\n'; }
service_start() {
  [[ $1 == proxy-agent-tuning ]]
  sh "$fixture/watch"
}
publish() { :; }
install() {
  # Only the root-owned runtime directory is created with install in this fixture.
  [[ $1 == -d && ${!#} == "$run_dir" ]]
  mkdir -p -- "$run_dir"
}
runuser() {
  [[ $* == "-u proxy-agent -- rm -f -- $data/tuning/probe.json" ]] || return 1
  rm -f -- "$data/tuning/probe.json"
}
systemctl() {
  printf '%s\n' "$*" >> "$calls"
  if [[ -n ${TUNING_TEST_PROBE_UNIT:-} ]]; then
    local arg args=()
    for arg in "$@"; do
      case $arg in
        start|stop|--no-block) args+=("$arg");;
        proxy-agent-tuning-probe.service) args+=("$TUNING_TEST_PROBE_UNIT");;
        proxy-agent-tuning.path) args+=("$TUNING_TEST_PATH_UNIT");;
        proxy-agent-tuning.service) args+=("$TUNING_TEST_APPLY_UNIT");;
        *) printf 'Unexpected native job argument %s\n' "$arg" >&2; return 1;;
      esac
    done
    command systemctl "${args[@]}"
    return
  fi
  [[ $1 != is-failed ]] || return 1
  if [[ $scenario == install-failure && $* == *start*probe.service* ]]; then return 1; fi
}

recover_fixture() {
  cat > "$snapshot/journal" <<EOF
proxycontrol-update-journal-v1
component=agent
previous=previous-helper
target=candidate-helper
was_active=false
backup=
database_source=
phase=prepared
EOF
  recover_snapshot "$snapshot" "$mode"
}

case $scenario in
  install-failure)
    # The installer is executable only as a whole-machine root installer. Run its
    # unmodified tuning publication branch with staged files and init operations
    # bound to this fixture instead.
    sed -n '/^# Tuning uses the same publish/,/^# Enabled, never started/p' "$repo/deploy/linux/install.sh" > "$fixture/activate"
    . "$fixture/activate"
    grep -q 'start proxy-agent-tuning-probe.service' "$calls"
    ;;
  legacy-rollback|boot)
    mkdir -p "$snapshot/files$systemd_dir"
    cp -p "$tuning_path" "$tuning_service" "$snapshot/files$systemd_dir/"
    if [[ $scenario == boot ]]; then mode=boot; cp -p "$tuning_probe" "$tuning_probe_reset" "$snapshot/files$systemd_dir/"; fi
    recover_fixture
    if [[ $mode == boot ]]; then
      if grep -E '(stop|start).*proxy-agent-tuning' "$calls" | grep -v -- '--no-block'; then
        printf 'boot recovery waited on a later unit\n' >&2; exit 1
      fi
      grep -q -- '--no-block.*proxy-agent-tuning-probe.service' "$calls"
      grep -q -- '--no-block.*proxy-agent-tuning.path' "$calls"
    else
      [[ ! -e $tuning_probe && ! -e $tuning_probe_reset ]]
      grep -q '^start proxy-agent-tuning.path$' "$calls"
    fi
    ;;
  openrc-restart-failure)
    # Run the package's start_pre. Do not start a helper process: the service
    # manager may fail to launch it after a successful previous run.
    sed -e "s|/run/proxy-agent-tuning|$run_dir|g" -e "s|/var/lib/proxy-agent/tuning|$data/tuning|g" \
      -e "s|/proc/sys/kernel/cap_last_cap|$fixture/cap_last_cap|g" "$repo/deploy/openrc/proxy-agent-tuning" > "$fixture/init"
    printf '40\n' > "$fixture/cap_last_cap"
    checkpath() { :; }
    (. "$fixture/init"; start_pre)
    # The supervisor can respawn the same watcher without calling start_pre.
    # Observe the pending state from inside the file-only helper on both tries.
    cat > "$fixture/helper" <<EOF
#!/bin/sh
test "\$(cat '$run_dir/probe-finished')" = pending || exit 3
test -e "$fixture/allow-probe"
EOF
    chmod +x "$fixture/helper"
    sed -e "s|^DIR=.*|DIR=$data/tuning|" -e "s|^HELPER=.*|HELPER=$fixture/helper|" \
      -e "s|^RUN=.*|RUN=$run_dir|" -e 's|exec /sbin/inotifyd .*|exit 0 ;;|' "$repo/deploy/openrc/tuning-watch" > "$fixture/watch"
    touch "$run_dir/skip-replay"
    sh "$fixture/watch"
    [[ $(cat "$run_dir/probe-finished") == failed ]]
    touch "$run_dir/skip-replay" "$fixture/allow-probe"
    sh "$fixture/watch"
    [[ $(cat "$run_dir/probe-finished") == ready ]]
    ;;
  openrc-rollback|openrc-boot)
    init_system=openrc
    : > "$root/etc/init.d/proxy-agent-tuning"
    mkdir -p "$snapshot/files$root/etc/init.d"
    cp -p "$root/etc/init.d/proxy-agent-tuning" "$snapshot/files$root/etc/init.d/"
    printf '#!/bin/sh\nprintf "%%s\\n" "$1" >> "%s"\n' "$calls" > "$fixture/helper"
    chmod +x "$fixture/helper"
    sed -e "s|^DIR=.*|DIR=$data/tuning|" -e "s|^HELPER=.*|HELPER=$fixture/helper|" \
      -e "s|^RUN=.*|RUN=$run_dir|" -e 's|exec /sbin/inotifyd .*|exit 0 ;;|' "$repo/deploy/openrc/tuning-watch" > "$fixture/watch"
    if [[ $scenario == openrc-boot ]]; then mode=boot; fi
    recover_fixture
    if [[ $mode == boot ]]; then
      [[ ! -s $calls ]]
      sh "$fixture/watch" # the ordinary boot runlevel now starts the watcher
      [[ $(cat "$calls") == $'--probe-tuning\n--apply-tuning' ]]
    else
      [[ $(cat "$calls") == '--probe-tuning' ]] || { cat "$calls"; exit 1; }
    fi
    ;;
  *) exit 2 ;;
esac
[[ ! -e $data/tuning/probe.json ]] || { printf 'a same-boot probe survived activation/recovery\n' >&2; exit 1; }
