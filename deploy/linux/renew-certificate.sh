#!/usr/bin/env bash
# Fixed stopped-node certificate renewal/confirmation; never starts a service.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
fail(){ printf 'Certificate operation refused: %s\n' "$*" >&2; exit 1; }
if [[ ${1:-} == --help ]]; then printf 'Usage: renew-certificate.sh --renew | --rotate-key\nUses the fixed installed Agent/config as proxy-agent on a stopped node. Renewal and rotation each confirm the new certificate.\n'; exit 0; fi
[[ $# == 1 ]] || fail 'Choose --renew or --rotate-key.'
case $1 in --renew) operation=--renew-node-certificate;; --rotate-key) operation=--rotate-node-key;; *) fail 'Choose --renew or --rotate-key.';; esac
[[ $(uname -s) == Linux && $EUID == 0 ]] || fail 'Run locally as root on Linux.'
# The init-system helpers (deploy/linux/service.sh) are sourced so the "is the
# Agent running" check works on both systemd and OpenRC; the sibling copy of an
# archive wins over the one install.sh publishes to /usr/local/lib/proxycontrol.
service_library=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/service.sh
if [[ ! -f $service_library ]]; then service_library=/usr/local/lib/proxycontrol/service.sh; fi
[[ -f $service_library && ! -L $service_library && $(stat -c '%u' "$service_library" 2>/dev/null) == 0 ]] \
  || fail "Missing the init-system library service.sh: $service_library"
# shellcheck source=service.sh
. "$service_library"
init_system=$(detect_init) || fail 'Neither systemd nor OpenRC is running on this host.'
required_tools=(runuser stat sha256sum sed flock)
if [[ $init_system == systemd ]]; then
  required_tools+=(systemctl)
else
  required_tools+=(rc-service)
fi
for tool in "${required_tools[@]}"; do command -v "$tool" >/dev/null || fail "Missing command: $tool"; done
registry=/var/lib/proxycontrol-install
[[ -d $registry && ! -L $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail 'No private installation registry.'
[[ ! -L $registry/operation.lock ]] || fail 'Unsafe operation lock.'
exec 9>"$registry/operation.lock"
flock -n 9 || fail 'Another installation operation is running.'
record=$registry/agent.manifest
[[ -f $record && ! -L $record && $(stat -c '%u:%a' "$record") == 0:600 && $(sed -n '7p' "$record") == installed ]] || fail 'No completed original Agent installation.'
[[ $(sed -n '1p' "$record") == proxycontrol-install-v1 && $(sed -n '2p' "$record") == agent ]] || fail 'Unknown installation record format.'
binary=/usr/local/bin/proxy-agent
[[ -f $binary && ! -L $binary && $(stat -c '%u' "$binary") == 0 ]] || fail 'Installed Agent must be root-owned.'
digest=$(sha256sum -- "$binary");digest=${digest%% *}
[[ $digest == "$(sed -n '3p' "$record")" ]] || fail 'Installed launcher differs from original installation.'
service_is_active proxy-agent && fail 'Stop the Agent service before certificate operations.'
[[ -d /var/lib/proxy-agent && ! -L /var/lib/proxy-agent && -f /etc/proxy-agent/config.yaml && ! -L /etc/proxy-agent/config.yaml ]] || fail 'Original configuration/data paths required.'
runuser -u proxy-agent -- "$binary" --config /etc/proxy-agent/config.yaml "$operation"
printf 'Node certificate confirmed. No proxy core or service started.\n'
