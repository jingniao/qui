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
    openrc) rc-service "$name" start 9>&-;;
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
    openrc) rc-service "$name" restart 9>&-;;
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
