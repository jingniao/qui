#!/usr/bin/env bash
# Run as root on Linux. Like panel-install-test, execute a path-rebased copy
# of the real script; all service/account/firewall operations are shell fakes.
set -euo pipefail
if [[ $(uname -s) != Linux ]]; then
  printf 'SKIP purge-menu-test: needs Linux (uname -s %s).\n' "$(uname -s)"
  exit 0
fi
[[ $EUID == 0 ]] || { printf 'purge-menu-test: needs root.\n'; exit 1; }
repo=$(cd -- "$(dirname -- "$0")/../../.." && pwd -P)
T=$(mktemp -d)
trap 'rm -rf -- "$T"' EXIT
check() { local label=$1; shift; "$@" || { printf 'FAIL - %s\n' "$label"; exit 1; }; printf 'ok - %s\n' "$label"; }
# Prefix every installation path, including paths embedded in quoted strings.
rebase() { sed -e "s|/usr/local|$R/usr/local|g" -e "s|/var/lib|$R/var/lib|g" -e "s|/etc/|$R/etc/|g" -e "s|/run/lock|$R/run/lock|g" "$1"; }
setup() {
  R=$T/root-$((++sequence)); export R
  mkdir -p "$R"/{usr/local/bin,usr/local/lib/proxycontrol,usr/local/lib/proxy-agent,etc/systemd/system,etc/init.d,etc/sysctl.d,var/lib/proxycontrol-install,run/lock}
  chmod 700 "$R/var/lib/proxycontrol-install"
  cp "$repo/deploy/linux/service.sh" "$R/service.sh"
  cat >> "$R/service.sh" <<'FAKES'
detect_init() { printf '%s\n' "$INIT"; }
systemctl() { case "$*" in *ActiveState*) echo inactive;; *Result*) echo success;; esac; }
service_state() { echo stopped; }
service_disable() { :; }
service_reload_definitions() { :; }
rc-service() { :; }
rc-update() { :; }
nft() { case $1 in list) [[ -f $R/$4 ]];; delete) rm "$R/$4";; esac; }
id() { [[ -f $R/account-$1 ]]; }
pgrep() { return 1; }
getent() { [[ -f $R/group-$2 ]]; }
userdel() { echo "userdel $*" >> "$R/calls"; rm "$R/account-$1"; }
groupdel() { echo "groupdel $*" >> "$R/calls"; rm "$R/group-$1"; }
ufw() {
  if [[ $1 == status ]]; then
    if [[ ${UFW_ACTIVE:-yes} != yes ]]; then echo 'Status: inactive'; return 0; fi
    echo 'Status: active'
    [[ ${RULES:-yes} != yes ]] || printf '22550/tcp ALLOW Anywhere\n18880/tcp ALLOW Anywhere\n'
  else echo "ufw $*" >> "$R/calls"; fi
}
firewall-cmd() {
  echo "firewall $*" >> "$R/calls"
  # Queries/deletes would succeed if the caller skipped the inactive-state gate.
  [[ $1 != --state ]]
}
FAKES
  rebase "$repo/deploy/linux/uninstall.sh" > "$R/uninstall.sh"
  for f in qui qui-update; do touch "$R/usr/local/bin/$f"; done
  touch "$R/usr/local/lib/proxycontrol/uninstall.sh"
  for c in "$@"; do
    mkdir -p "$R/etc/proxy-$c" "$R/var/lib/proxy-$c/tuning"
    printf 'http_address: :22550\ngrpc_address: :18880\n' > "$R/etc/proxy-$c/config.yaml"
    printf '#!/bin/sh\nexit 17\n' > "$R/usr/local/bin/proxy-$c"
    chmod 755 "$R/usr/local/bin/proxy-$c"
    unit=$R/etc/systemd/system/proxy-$c.service
    [[ $INIT != openrc ]] || unit=$R/etc/init.d/proxy-$c
    touch "$unit" "$R/account-proxy-$c" "$R/group-proxy-$c"
    bd=$(sha256sum "$R/usr/local/bin/proxy-$c"); ud=$(sha256sum "$unit")
    printf 'proxycontrol-install-v1\n%s\n%s\n%s\n\n\ninstalled\n\n\n%s\n' "$c" "${bd%% *}" "${ud%% *}" "$INIT" > "$R/var/lib/proxycontrol-install/$c.manifest"
    chmod 600 "$R/var/lib/proxycontrol-install/$c.manifest"
  done
  touch "$R/proxy_agent_source" "$R/proxy_agent_hop"
}
run() {
  status=0
  bash "$R/uninstall.sh" "$@" > "$R/output" 2>&1 || status=$?
}
# util-linux script provides a real PTY: the production -t guard is unchanged.
terminal() {
  status=0
  printf '%b' "$1" | script -q -e -c "bash '$R/uninstall.sh' $2 --purge" /dev/null > "$R/output" 2>&1 || status=$?
}
# Exercise the complete installer after purge, with only host account/service
# operations faked. Real ELF validation, staging, publication and records run.
install_panel() {
  local package=$R/package/deploy f binary_digest
  mkdir -p "$package/linux" "$package/systemd" "$package/examples"
  for f in install.sh qui.sh update.sh update-recovery.sh uninstall.sh panel-update-request.sh; do
    rebase "$repo/deploy/linux/$f" > "$package/linux/$f"
  done
  for f in proxy-panel.service proxy-panel-update.service proxy-panel-update.path proxycontrol-update-recovery.service; do
    rebase "$repo/deploy/systemd/$f" > "$package/systemd/$f"
  done
  rebase "$repo/deploy/examples/panel.yaml" > "$package/examples/panel.yaml"
  cp "$R/service.sh" "$package/linux/service.sh"
  cat >> "$package/linux/service.sh" <<'INSTALL_FAKES'
service_is_active() { return 1; }
getent() {
  case $1 in
    group) [[ -f $R/group-$2 ]];;
    passwd) [[ -f $R/account-$2 ]];;
    *) return 1;;
  esac
}
groupadd() { touch "$R/group-${@: -1}"; }
useradd() { touch "$R/account-${@: -1}"; }
id() {
  [[ -f $R/account-${@: -1} ]] || return 1
  case $1 in -u|-g) echo 12345;; -gn) echo "${@: -1}";; esac
}
# Numeric ownership needs no real service account in the host account database.
install() {
  local argument
  local -a arguments=()
  for argument in "$@"; do
    [[ $argument != proxy-panel ]] || argument=12345
    arguments+=("$argument")
  done
  command install "${arguments[@]}"
}
INSTALL_FAKES
  cp -L /bin/true "$R/panel-binary"
  binary_digest=$(sha256sum "$R/panel-binary"); binary_digest=${binary_digest%% *}
  status=0
  bash "$package/linux/install.sh" --panel --binary "$R/panel-binary" --sha256 "$binary_digest" \
    > "$R/install-output" 2>&1 || status=$?
}
sequence=0
for INIT in systemd openrc; do
  export INIT
  setup agent
  run agent
  check "$INIT ordinary uninstall" test "$status" = 0
  check 'source table removed' test ! -e "$R/proxy_agent_source"
  check 'hop table removed' test ! -e "$R/proxy_agent_hop"
  setup agent
  rm "$R/proxy_agent_source" "$R/proxy_agent_hop"
  run agent
  check 'absent tables accepted' test "$status" = 0
  setup agent
  run agent --purge
  check 'nonterminal purge refused' test "$status" != 0
  check 'nonterminal data retained' test -d "$R/var/lib/proxy-agent"
  command -v script >/dev/null || { printf 'SKIP PTY scenarios: util-linux script unavailable.\n'; continue; }
  for selection in panel agent all; do
    if [[ $selection == all ]]; then setup panel agent; else setup "$selection"; fi
    terminal 'y\npurge\n' "$selection"
    check "$INIT purge $selection" test "$status" = 0
    check 'registry removed' test ! -e "$R/var/lib/proxycontrol-install"
    check 'shared library removed' test ! -e "$R/usr/local/lib/proxycontrol"
    check 'shared updater removed' test ! -e "$R/usr/local/bin/qui-update"
    check 'shared menu removed' test ! -e "$R/usr/local/bin/qui"
    if [[ $selection != agent ]]; then
      check 'ufw HTTP rule removed' grep -qx 'ufw --force delete allow 22550/tcp' "$R/calls"
      check 'ufw gRPC rule removed' grep -qx 'ufw --force delete allow 18880/tcp' "$R/calls"
    fi
    for c in panel agent; do
      for p in "var/lib/proxy-$c" "etc/proxy-$c" "usr/local/bin/proxy-$c" "account-proxy-$c" "group-proxy-$c"; do
        check "$p removed" test ! -e "$R/$p"
      done
    done
  done
  setup panel
  RULES=no; export RULES
  terminal 'y\npurge\n' panel
  unset RULES
  check 'missing firewall rules accepted' test "$status" = 0
  check 'missing rules not deleted' bash -c '! grep -q "^ufw\|^firewall" "$1"' _ "$R/calls"
  setup panel
  UFW_ACTIVE=no; export UFW_ACTIVE
  terminal 'y\npurge\n' panel
  unset UFW_ACTIVE
  check "$INIT inactive firewalld purge succeeds" test "$status" = 0
  check 'inactive firewalld state checked' grep -qx 'firewall --state' "$R/calls"
  check 'inactive firewalld rules not queried, deleted or reloaded' \
    bash -c '! grep -qE "^firewall (--permanent|--reload)" "$1"' _ "$R/calls"
  check 'inactive ufw rules not deleted' bash -c '! grep -q "^ufw " "$1"' _ "$R/calls"
  if [[ $INIT == systemd ]]; then
    # The just-purged panel must behave like a fresh host, with no --reinstall.
    install_panel
    check 'install after purge without --reinstall succeeds' test "$status" = 0
    check 'install after purge restores binary' cmp "$R/panel-binary" "$R/usr/local/bin/proxy-panel"
    check 'install after purge restores executable mode' test -x "$R/usr/local/bin/proxy-panel"
    check 'install after purge restores config' test -f "$R/etc/proxy-panel/config.yaml"
    check 'install after purge restores service' test -f "$R/etc/systemd/system/proxy-panel.service"
    check 'install after purge restores manifest' grep -qx 'proxycontrol-install-v2' "$R/var/lib/proxycontrol-install/panel.manifest"
    check 'install after purge leaves no removed marker' test ! -e "$R/var/lib/proxycontrol-install/panel.removed"
  fi
  for answer in 'n\n' 'y\nno\n'; do
    setup agent
    terminal "$answer" agent
    check 'cancel exits zero' test "$status" = 0
    check 'cancel message' grep -q '已取消' "$R/output"
    check 'cancel binary retained' test -x "$R/usr/local/bin/proxy-agent"
    check 'cancel manifest retained' test -f "$R/var/lib/proxycontrol-install/agent.manifest"
  done
  setup panel agent
  terminal 'y\npurge\n' agent
  check 'single component purge succeeds' test "$status" = 0
  check 'shared menu retained' test -f "$R/usr/local/bin/qui"
  check 'other manifest retained' test -f "$R/var/lib/proxycontrol-install/panel.manifest"
  setup agent
  # Record tuning units so uninstall executes the failing real helper fixture.
  for suffix in service path; do
    if [[ $INIT == systemd ]]; then
      touch "$R/etc/systemd/system/proxy-agent-tuning.$suffix"
    elif [[ $suffix == service ]]; then touch "$R/etc/init.d/proxy-agent-tuning"
    else touch "$R/usr/local/lib/proxy-agent/tuning-watch"; fi
  done
  empty=$(printf '' | sha256sum); empty=${empty%% *}
  printf '%s\n%s\n' "$empty" "$empty" >> "$R/var/lib/proxycontrol-install/agent.manifest"
  terminal 'y\npurge\n' agent
  check 'tuning restore failure refused' test "$status" != 0
  check 'tuning failure data retained' test -d "$R/var/lib/proxy-agent/tuning"
  check 'tuning failure binary retained' test -x "$R/usr/local/bin/proxy-agent"
  setup agent
  mv "$R/var/lib/proxy-agent" "$R/saved"
  ln -s "$R/saved" "$R/var/lib/proxy-agent"
  terminal 'y\npurge\n' agent
  check 'symlink refused' test "$status" != 0
  check 'symlink target preserved' test -d "$R/saved/tuning"
done

# Run the complete menu including manifest detection and startup validation.
for selection in agent panel all; do
  INIT=systemd; export INIT
  if [[ $selection == all ]]; then setup panel agent; else setup "$selection"; fi
  cp "$R/service.sh" "$R/usr/local/lib/proxycontrol/service.sh"
  for c in panel agent; do
    if [[ -f $R/usr/local/bin/proxy-$c ]]; then
      printf '#!/bin/sh\necho fixture-version\n' > "$R/usr/local/bin/proxy-$c"
    fi
  done
  # passwd lookup is a boundary fact; group lookup remains the account fake.
  printf '\ngetent() { return 0; }\n' >> "$R/usr/local/lib/proxycontrol/service.sh"
  rebase "$repo/deploy/linux/qui.sh" > "$R/menu.sh"
  printf '0\n' | bash "$R/menu.sh" > "$R/menu-output"
  check "$selection menu shows version" grep -q fixture-version "$R/menu-output"
  if [[ $selection == agent ]]; then
    check 'agent compact menu' grep -q '^2. 卸载并清除全部数据' "$R/menu-output"
    check 'agent hides owner reset' bash -c '! grep -q "修改管理员" "$1"' _ "$R/menu-output"
    choice=2
  else
    check 'panel full menu' grep -q '^8. 卸载并清除全部数据' "$R/menu-output"
    choice=8
  fi
  rm "$R/usr/local/lib/proxycontrol/uninstall.sh"
  printf '%s\n0\n' "$choice" | bash "$R/menu.sh" > "$R/menu-output"
  check 'missing uninstaller recommends updater' grep -q '请先运行一次 qui-update' "$R/menu-output"
  cp "$R/uninstall.sh" "$R/usr/local/lib/proxycontrol/uninstall.sh"
  chmod 755 "$R/usr/local/lib/proxycontrol/uninstall.sh"
  # The menu version fixture above changed the binary; keep its install record accurate.
  for c in panel agent; do
    if [[ -f $R/usr/local/bin/proxy-$c ]]; then
      bd=$(sha256sum "$R/usr/local/bin/proxy-$c")
      sed -i "3s/.*/${bd%% *}/" "$R/var/lib/proxycontrol-install/$c.manifest"
    fi
  done
  for answer in 'n\n' 'y\nwrong\n'; do
    status=0
    back='\n'; [[ $selection != agent ]] || back=''
    printf '%b' "$choice\n${answer}${back}0\n" | script -q -e -c "bash '$R/menu.sh'" /dev/null > "$R/menu-output" 2>&1 || status=$?
    check "$selection menu cancel exits zero" test "$status" = 0
    check 'menu cancel message' grep -q '已取消' "$R/menu-output"
    check 'menu cancel returns to menu' test "$(grep -c fixture-version "$R/menu-output")" = 2
  done
  c=$selection; [[ $c != all ]] || c=panel
  mv "$R/var/lib/proxy-$c" "$R/saved"
  ln -s "$R/saved" "$R/var/lib/proxy-$c"
  status=0
  printf '%s\ny\npurge\n' "$choice" | script -q -e -c "bash '$R/menu.sh'" /dev/null > "$R/menu-output" 2>&1 || status=$?
  check 'menu preserves purge failure exit' test "$status" != 0
  check 'menu preserves original purge error' grep -q 'Uninstall refused:' "$R/menu-output"
  check 'failed purge preserves data' test -d "$R/saved/tuning"
  rm "$R/var/lib/proxy-$c"
  mv "$R/saved" "$R/var/lib/proxy-$c"
  status=0
  printf '%s\ny\npurge\n' "$choice" | script -q -e -c "bash '$R/menu.sh'" /dev/null > "$R/menu-output" 2>&1 || status=$?
  cat "$R/menu-output"
  check "$selection menu purge exits zero" test "$status" = 0
  check 'menu purge removes registry' test ! -e "$R/var/lib/proxycontrol-install"
  check 'successful purge does not redisplay menu' test "$(grep -c fixture-version "$R/menu-output")" = 1
  check 'successful purge does not wait for Enter' bash -c '! grep -q "按 Enter 返回菜单" "$1"' _ "$R/menu-output"
done
printf 'purge-menu-test: passed\n'
