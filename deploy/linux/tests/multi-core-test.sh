#!/usr/bin/env bash
# Rootless tests of actual installer parsing, record migration, config and snapshot
# inventory. Safe in Windows Git Bash: never executes an installer or service.
set -euo pipefail
repo=$(cd -- "$(dirname -- "$0")/../../.." && pwd -P)
. "$repo/deploy/linux/service.sh"
T=$(mktemp -d)
trap 'rm -rf -- "$T"' EXIT
passes=0
check() {
  local label=$1
  shift
  if "$@"; then printf 'ok - %s\n' "$label"; passes=$((passes+1))
  else printf 'FAIL - %s\n' "$label" >&2; exit 1; fi
}
fail() { printf '%s\n' "$*" >&2; exit 1; }
component=agent; digest=$(printf agent | sha256sum); digest=${digest%% *}
unit_digest=$digest; host_network_service_digest=$digest; host_network_path_digest=$digest
tuning_service_digest=$digest; tuning_path_digest=$digest; init_system=openrc
printf 'proxycontrol-install-v1\nagent\n%s\n%s\nxray\n%s\ninstalled\n%s\n%s\nopenrc\n%s\n%s\n' \
  "$digest" "$digest" "$digest" "$digest" "$digest" "$digest" "$digest" > "$T/v1"
check 'read K2a v1' qui_record_cores "$T/v1" agent
check 'v1 fixed core line' test "${record_cores[*]}" = xray
head -n 7 "$T/v1" > "$T/v1-short"
check 'read original seven-line v1' qui_record_cores "$T/v1-short" agent
qui_write_record "$T/v2"
check 'v1 to v2' qui_record_cores "$T/v2" agent
check 'v2 header' test "$(sed -n '1p' "$T/v2")" = proxycontrol-install-v2
check 'init line retained' test "$(sed -n '10p' "$T/v2")" = openrc
check 'tuning service line retained' test "$(sed -n '11p' "$T/v2")" = "$digest"
check 'tuning watcher line retained' test "$(sed -n '12p' "$T/v2")" = "$digest"
record_cores+=(sing-box); record_digests+=("$unit_digest")
qui_write_record "$T/multi"
check 'read both cores' qui_record_cores "$T/multi" agent
check 'core order retained' test "${record_cores[*]}" = 'xray sing-box'
check 'both core digests retained' test "${#record_digests[@]}" = 2
# The third core kind is an ordinary record entry: one line per core, its fixed
# installer path and its own binary digest.
record_cores+=(gost); record_digests+=("$tuning_service_digest")
qui_write_record "$T/three"
check 'read three cores' qui_record_cores "$T/three" agent
check 'three core order retained' test "${record_cores[*]}" = 'xray sing-box gost'
check 'gost record line' grep -qx "core gost /usr/local/lib/proxy-agent/gost $tuning_service_digest" "$T/three"
qui_record_cores "$T/multi" agent
for invalid in duplicate path name digest; do
  cp "$T/multi" "$T/invalid"
  case $invalid in
    duplicate) sed -n '13p' "$T/multi" >> "$T/invalid";;
    path) sed -i 's@/usr/local/lib/proxy-agent/xray@/tmp/xray@' "$T/invalid";;
    name) sed -i 's/core xray/core sinbox/' "$T/invalid";;
    digest) printf 'core xray /usr/local/lib/proxy-agent/xray bad\n' >> "$T/invalid";;
  esac
  check "reject $invalid record" bash -c '. "$1"; ! qui_record_cores "$2" agent' _ "$repo/deploy/linux/service.sh" "$T/invalid"
done
check 'panel rejects core entries' bash -c '. "$1"; ! qui_record_cores "$2" panel' _ "$repo/deploy/linux/service.sh" "$T/multi"
head -n 11 "$T/multi" > "$T/truncated"
check 'reject truncated v2 metadata' bash -c '. "$1"; ! qui_record_cores "$2" agent' _ "$repo/deploy/linux/service.sh" "$T/truncated"
component=panel; record_cores=(); record_digests=()
qui_write_record "$T/panel"
check 'panel v2 round trip without cores' qui_record_cores "$T/panel" panel
check 'panel has all twelve metadata lines' test "$(wc -l < "$T/panel" | tr -d ' ')" = 12
component=agent
qui_record_cores "$T/multi" agent
cat > "$T/config" <<'YAML'
control_address: panel:9443
"core_name" : sing-box
core_path : /old/sing-box
'cores':
  - name: sing-box
    path: /old/sing-box
tls:
  server_name: panel
reconcile_interval: 5s
YAML
qui_agent_core_config "$T/config" "$T/new"
check 'config first-core fallback' grep -qx 'core_name: xray' "$T/new"
check 'config both entries' test "$(grep -c '  - name:' "$T/new")" = 2
check 'config unrelated nested TLS preserved' grep -qx '  server_name: panel' "$T/new"
check 'config unrelated scalar preserved' grep -qx 'reconcile_interval: 5s' "$T/new"
qui_agent_core_config "$T/new" "$T/new2"
# Blank lines are harmless; assert only one authoritative block and scalar pair.
check 'config rewrite no duplicate cores' test "$(grep -c '^cores:' "$T/new2")" = 1
check 'config rewrite no duplicate fallback' test "$(grep -c '^core_path:' "$T/new2")" = 1

# Execute the real argument parser only, stopping before any host operation.
installer_args() (
  mode=''; binary=''; digest=''; config=''; core=''; cores=()
  core_source=qui; qui_version=''; core_bundle=''; core_sums=''; reinstall=false
  declare -A core_versions=() core_digests=() core_urls=()
  eval "$(sed -n '/^while ((\$#)); do/,/^done/p' "$repo/deploy/linux/install.sh")"
  printf '%s|%s|%s' "${cores[*]}" "${core_versions[xray]:-}" "${core_versions[sing-box]:-}"
)
check 'repeatable CLI with per-core versions' test "$(installer_args --node --core xray --core-version 26.3.27 --core sing-box --core-version 1.14.4)" = 'xray sing-box|26.3.27|1.14.4'
check 'repeatable CLI accepts the third core' test "$(installer_args --node --core xray --core-version 26.3.27 --core gost --core-version 3.3.2)" = 'xray gost|26.3.27|'
# Direct subshell call: fail exits the parser subshell without ending this test.
if installer_args --core xray --core xray >/dev/null 2>&1; then exit 1; fi
printf 'ok - duplicate CLI refused\n'; passes=$((passes+1))
# GOST only serves port forwards, so it can never be the first (primary) core.
if installer_args --node --core gost >/dev/null 2>&1; then exit 1; fi
if installer_args --node --core gost --core xray >/dev/null 2>&1; then exit 1; fi
printf 'ok - GOST as the first core refused\n'; passes=$((passes+1))

node_args() (
  cores=(); core_source=qui
  eval "$(sed -n '/^while ((\$#)); do/,/^done/p' "$repo/deploy/linux/node-install.sh")"
  eval "$(sed -n '/^if \[\[ ${#cores\[@\]} == 0 /,/^done/p' "$repo/deploy/linux/node-install.sh")"
  printf '%s' "${cores[*]}"
)
check 'one-click default is only Xray' test "$(node_args)" = xray
check 'one-click repeats core flags' test "$(node_args --core xray --core sing-box)" = 'xray sing-box'
check 'one-click accepts the third core' test "$(node_args --core xray --core sing-box --core gost)" = 'xray sing-box gost'
if node_args --core xray --core xray >/dev/null 2>&1; then exit 1; fi
printf 'ok - one-click duplicate CLI refused\n'; passes=$((passes+1))
# GOST only serves port forwards, so it can never be the first (primary) core.
if node_args --core gost >/dev/null 2>&1; then exit 1; fi
if node_args --core gost --core sing-box >/dev/null 2>&1; then exit 1; fi
printf 'ok - one-click GOST as the first core refused\n'; passes=$((passes+1))

# The Agent's certificate directory is a fixed part of the section 30.2 layout: an
# inbound may name certificate files from it, so the installer has to create it with
# the mode the Agent can read and root alone can write. Both installers state it, and
# neither removes it again (uninstall keeps the configuration next to it).
check 'installer creates the Agent certificate directory' grep -qF 'install -d -o root -g "$account" -m 0750 "$etc/certs"' "$repo/deploy/linux/install.sh"
check 'one-command installer creates the Agent certificate directory' grep -qF 'install -d -o root -g proxy-agent -m 0750 /etc/proxy-agent/certs' "$repo/deploy/linux/node-install.sh"

eval "$(sed -n '/^component_files() {/,/^}/p' "$repo/deploy/linux/update.sh")"
root=$T/root; program_directory=$root/usr/local/bin; registry=$root/var/lib/proxycontrol-install
for init_system in systemd openrc; do
  component_files agent > "$T/paths"
  for suffix in usr/local/lib/proxy-agent/xray usr/local/lib/proxy-agent/sing-box usr/local/lib/proxy-agent/gost etc/proxy-agent/config.yaml; do
    check "$init_system snapshot $suffix" grep -qx "$root/$suffix" "$T/paths"
  done
  check "$init_system tuning snapshot retained" grep -q 'proxy-agent-tuning' "$T/paths"
  component_files panel > "$T/panel-paths"
  if grep -q '/proxy-agent/' "$T/panel-paths"; then exit 1; fi
  for suffix in usr/local/lib/proxycontrol/panel-update-request etc/systemd/system/proxy-panel-update.service etc/systemd/system/proxy-panel-update.path; do
    check "$init_system panel snapshot $suffix" grep -qx "$root/$suffix" "$T/panel-paths"
  done
done
eval "$(sed -n '/^restore_snapshot() {/,/^}/p' "$repo/deploy/linux/update.sh")"
# Exercise the production restore loop on fixture paths. Service calls and fsync
# are stubbed; crash durability itself is reserved for Linux/root update-test.
service_reload_definitions() { return 0; }
service_is_active() { return 1; }
sync() { return 0; }
for init_system in systemd openrc; do
  snapshot=$T/snapshot-$init_system
  mkdir -p "$root/usr/local/lib/proxy-agent" "$root/etc/proxy-agent" "$registry"
  printf original-xray > "$root/usr/local/lib/proxy-agent/xray"
  printf original-sing-box > "$root/usr/local/lib/proxy-agent/sing-box"
  printf original-gost > "$root/usr/local/lib/proxy-agent/gost"
  cp "$T/three" "$registry/agent.manifest"
  cp "$T/new" "$root/etc/proxy-agent/config.yaml"
  while IFS= read -r path; do
    [[ -f $path ]] || continue
    mkdir -p "${snapshot}/files${path%/*}"
    cp -p "$path" "$snapshot/files$path"
  done < <(component_files agent)
  printf new-xray > "$root/usr/local/lib/proxy-agent/xray"
  printf new-sing-box > "$root/usr/local/lib/proxy-agent/sing-box"
  printf new-gost > "$root/usr/local/lib/proxy-agent/gost"
  printf partial-record > "$registry/agent.manifest"
  printf changed-config > "$root/etc/proxy-agent/config.yaml"
  check "$init_system production restore loop" restore_snapshot agent
  check "$init_system Xray restored" test "$(cat "$root/usr/local/lib/proxy-agent/xray")" = original-xray
  check "$init_system sing-box restored" test "$(cat "$root/usr/local/lib/proxy-agent/sing-box")" = original-sing-box
  check "$init_system gost restored" test "$(cat "$root/usr/local/lib/proxy-agent/gost")" = original-gost
  check "$init_system config restored" cmp -s "$T/new" "$root/etc/proxy-agent/config.yaml"
  check "$init_system record restored" cmp -s "$T/three" "$registry/agent.manifest"
  check "$init_system idempotent restore" restore_snapshot agent
done

# The panel's web-UI update bridge is part of the panel snapshot, and a bridge that a failed
# update published onto a host whose previous version had none has to be removed again
# rather than left behind at the new version. The restore loop is the same one the agent
# cases above use; only the file list differs.
systemctl_calls=$T/systemctl-calls
: > "$systemctl_calls"
systemctl() { printf '%s\n' "$*" >> "$systemctl_calls"; return 0; }
init_system=systemd
snapshot=$T/snapshot-panel
mkdir -p "$root/usr/local/bin" "$root/usr/local/lib/proxycontrol" "$root/etc/systemd/system" "$registry"
printf original-panel > "$root/usr/local/bin/proxy-panel"
printf original-menu > "$root/usr/local/bin/qui"
while IFS= read -r path; do
  [[ -f $path ]] || continue
  mkdir -p "${snapshot}/files${path%/*}"
  cp -p "$path" "$snapshot/files$path"
done < <(component_files panel)
printf new-panel > "$root/usr/local/bin/proxy-panel"
printf '#!/bin/sh\n' > "$root/usr/local/lib/proxycontrol/panel-update-request"
printf '# new service unit\n' > "$root/etc/systemd/system/proxy-panel-update.service"
printf '# new path unit\n' > "$root/etc/systemd/system/proxy-panel-update.path"
check 'panel production restore loop' restore_snapshot panel
check 'panel binary restored' test "$(cat "$root/usr/local/bin/proxy-panel")" = original-panel
check 'new update helper removed' test ! -e "$root/usr/local/lib/proxycontrol/panel-update-request"
check 'new update service unit removed' test ! -e "$root/etc/systemd/system/proxy-panel-update.service"
check 'new update path unit removed' test ! -e "$root/etc/systemd/system/proxy-panel-update.path"
check 'path unit of the rolled-back release disabled' grep -qx -- 'disable --now proxy-panel-update.path' "$systemctl_calls"

# A rollback while a web-triggered update is running must not stop the service that runs
# the rollback: the snapshot has the bridge, so the path unit is left alone.
snapshot=$T/snapshot-panel-bridge
printf '#!/bin/sh\n' > "$root/usr/local/lib/proxycontrol/panel-update-request"
printf '# service unit\n' > "$root/etc/systemd/system/proxy-panel-update.service"
printf '# path unit\n' > "$root/etc/systemd/system/proxy-panel-update.path"
while IFS= read -r path; do
  [[ -f $path ]] || continue
  mkdir -p "${snapshot}/files${path%/*}"
  cp -p "$path" "$snapshot/files$path"
done < <(component_files panel)
: > "$systemctl_calls"
check 'panel restore of a bridging release' restore_snapshot panel
if grep -qx -- 'disable --now proxy-panel-update.path' "$systemctl_calls"; then
  printf 'FAIL - the update path unit must not be disabled when the snapshot has it\n' >&2; exit 1
fi
printf 'ok - bridging release keeps its update path unit\n'; passes=$((passes+1))
printf 'multi-core-test: %s passed\n' "$passes"
