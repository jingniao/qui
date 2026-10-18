#!/usr/bin/env bash
# Removal preserves all identity, consumption, configuration and recovery data.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
fail() { printf 'Uninstall refused: %s\n' "$*" >&2; exit 1; }
if [[ ${1:-} == --help ]]; then
  printf 'Usage: uninstall.sh panel|agent\nStops and removes only recorded service/binaries. All data and config are retained.\n'; exit 0
fi
[[ $# == 1 && ( $1 == panel || $1 == agent ) ]] || fail 'Use uninstall.sh panel|agent.'
[[ $(uname -s) == Linux && $EUID == 0 ]] || fail 'Run locally as root on Linux.'
component=$1
name=proxy-$component
registry=/var/lib/proxycontrol-install
record=$registry/$component.manifest
removed=$registry/$component.removed
target=/usr/local/bin/$name
# The panel's terminal menu is removed with the program.
qui=/usr/local/bin/qui
# The updater serves both components, so it is removed only when no other
# component of this platform is installed on the host.
update=/usr/local/bin/qui-update
unit=/etc/systemd/system/$name.service
host_network_service=/etc/systemd/system/proxy-agent-host-network.service
host_network_path=/etc/systemd/system/proxy-agent-host-network.path
for tool in systemctl sha256sum stat flock sed install rm; do command -v "$tool" >/dev/null || fail "Missing command: $tool"; done
for path in /usr/local /usr/local/bin /usr/local/lib /usr/local/lib/proxy-agent /etc/systemd /etc/systemd/system /var/lib "$registry" /run/lock; do [[ ! -L $path ]] || fail "Symlinked destination: $path"; done
[[ -d $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail 'No protected installation registry.'
[[ ! -L $registry/operation.lock ]] || fail 'Symlinked installation lock.'
exec 9>"$registry/operation.lock"
flock -n 9 || fail 'Another installation operation is running.'
[[ -f $record && ! -L $record && $(stat -c '%u:%a' "$record") == 0:600 ]] || fail 'Missing protected installation record.'
[[ ! -e $removed && ! -L $removed ]] || fail 'Uninstall was already recorded; inspect retained files.'
[[ $(sed -n '1p' "$record") == proxycontrol-install-v1 && $(sed -n '2p' "$record") == "$component" && $(sed -n '7p' "$record") == installed ]] || fail 'Installation is incomplete or unknown.'
binary_digest=$(sed -n '3p' "$record")
unit_digest=$(sed -n '4p' "$record")
core=$(sed -n '5p' "$record")
core_digest=$(sed -n '6p' "$record")
# The enforced-source units are recorded after the completion marker. An
# installation made before that feature has no such lines, and its units are
# then left untouched.
host_network_service_digest=$(sed -n '8p' "$record")
host_network_path_digest=$(sed -n '9p' "$record")
host_network_units=false
if [[ $component == agent && -n $host_network_service_digest && -n $host_network_path_digest ]]; then
  host_network_units=true
fi
verify_owned() {
  local file=$1 expected=$2 actual
  [[ -f $file && ! -L $file && $(stat -c '%u' "$file") == 0 && $expected =~ ^[0-9a-f]{64}$ ]] || fail "Unrecognized owned file: $file"
  actual=$(sha256sum -- "$file"); actual=${actual%% *}
  [[ $actual == "$expected" ]] || fail "Modified file retained: $file"
}
verify_owned "$target" "$binary_digest"
verify_owned "$unit" "$unit_digest"
if $host_network_units; then
  verify_owned "$host_network_service" "$host_network_service_digest"
  verify_owned "$host_network_path" "$host_network_path_digest"
fi
core_target=''
if [[ -n $core ]]; then
  [[ $component == agent && ( $core == xray || $core == sing-box ) ]] || fail 'Invalid recorded core.'
  core_target=/usr/local/lib/proxy-agent/$core
  verify_owned "$core_target" "$core_digest"
fi
# A failed stop is not a clean handoff: never delete the program underneath it.
systemctl stop "$name.service"
[[ $(systemctl show "$name.service" -p ActiveState --value) == inactive ]] || fail 'Service did not stop cleanly.'
[[ $(systemctl show "$name.service" -p Result --value) == success ]] || fail 'Service stop failed; preserve programs and inspect accounting/recovery state.'
systemctl disable "$name.service"
if $host_network_units; then
  # The path unit may be armed and the oneshot unit is only run by it, so the
  # watcher is stopped first. A unit that is already inactive or was removed by
  # hand is not an error: the files and the table are what this script owns.
  systemctl disable --now "$host_network_path" || true
  systemctl disable "$host_network_service" || true
fi
install -o root -g root -m 0600 -- "$record" "$removed"
rm -- "$unit" "$target"
# /usr/local/bin/qui is not part of the recorded manifest, so it has no digest to
# verify; it belongs to the panel program and goes away with it.
if [[ $component == panel ]]; then rm -f -- "$qui"; fi
# /usr/local/bin/qui-update has no digest either and is shared by both
# components, so it stays while the other component is still installed.
other=panel
if [[ $component == panel ]]; then other=agent; fi
if [[ ! -e $registry/$other.manifest && ! -L $registry/$other.manifest ]]; then rm -f -- "$update"; fi
if $host_network_units; then rm -- "$host_network_service" "$host_network_path"; fi
[[ -z $core_target ]] || rm -- "$core_target"
systemctl daemon-reload
# The enforced-source table belongs to this Agent, so removing it restores the
# host's normal source selection. It may legitimately be absent.
if [[ $component == agent ]] && command -v nft >/dev/null && nft list table inet proxy_agent_source >/dev/null 2>&1; then
  nft delete table inet proxy_agent_source
fi
rm -- "$record"
printf '%s removed. Identity, quota ledgers, configuration, backups and service account remain.\n' "$name"
printf 'This does not revoke the node or release reserved allowances; close them explicitly in the panel.\n'
