#!/usr/bin/env bash
# Fixed local enrollment operation. Does not start a core or a service.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
fail(){ printf 'Enrollment refused: %s\n' "$*" >&2; exit 1; }
if [[ ${1:-} == --help ]]; then printf 'Usage: enroll.sh --token-file FILE\nUses fixed installed Agent/config as proxy-agent, then exits. Original token file is retained.\n'; exit 0; fi
[[ $# == 2 && $1 == --token-file ]] || fail 'Use --token-file FILE.'
[[ $(uname -s) == Linux && $EUID == 0 ]] || fail 'Run locally as root on Linux.'
# The init-system helpers (deploy/linux/service.sh) are sourced, never executed:
# this script has to ask the host which init system runs it instead of assuming
# systemd, because the same Agent also runs on Alpine. The release archive carries
# a copy next to this script and install.sh publishes one to
# /usr/local/lib/proxycontrol/service.sh; the sibling wins, and anything but a
# root-owned regular file is refused rather than read as root.
service_library=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/service.sh
if [[ ! -f $service_library ]]; then service_library=/usr/local/lib/proxycontrol/service.sh; fi
[[ -f $service_library && ! -L $service_library && $(stat -c '%u' "$service_library" 2>/dev/null) == 0 ]] \
  || fail "Missing the init-system library service.sh: $service_library"
# shellcheck source=service.sh
. "$service_library"
init_system=$(detect_init) || fail 'Neither systemd nor OpenRC is running on this host.'
required_tools=(runuser stat install mktemp rm rmdir chown sha256sum sed flock)
if [[ $init_system == systemd ]]; then
  required_tools+=(systemctl)
else
  required_tools+=(rc-service)
fi
for tool in "${required_tools[@]}"; do command -v "$tool" >/dev/null || fail "Missing command: $tool"; done
token=$2
[[ -f $token && ! -L $token && $(stat -c '%s' "$token") -le 512 ]] || fail 'Token must be a bounded regular file.'
token_mode=$(stat -c '%a' "$token")
[[ ( $token_mode == 600 || $token_mode == 400 ) && $(stat -c '%h' "$token") == 1 ]] || fail 'Token requires owner-only mode 0600/0400 and a single link.'
registry=/var/lib/proxycontrol-install
[[ -d $registry && ! -L $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail 'No private installation registry.'
[[ ! -L $registry/operation.lock ]] || fail 'Unsafe operation lock.'
exec 9>"$registry/operation.lock"
flock -n 9 || fail 'Another installation operation is running.'
record=$registry/agent.manifest
[[ -f $record && ! -L $record && $(stat -c '%u:%a' "$record") == 0:600 && $(sed -n '7p' "$record") == installed ]] || fail 'No completed original Agent installation.'
qui_record_cores "$record" agent || fail 'Unknown installation record format.'
binary=/usr/local/bin/proxy-agent
[[ -f $binary && ! -L $binary && $(stat -c '%u' "$binary") == 0 ]] || fail 'Installed Agent must be root-owned.'
digest=$(sha256sum -- "$binary");digest=${digest%% *}
[[ $digest == "$(sed -n '3p' "$record")" ]] || fail 'Installed launcher differs from original installation.'
service_is_active proxy-agent && fail 'Stop the Agent service before enrollment.'
[[ -d /var/lib/proxy-agent && ! -L /var/lib/proxy-agent && -f /etc/proxy-agent/config.yaml && ! -L /etc/proxy-agent/config.yaml ]] || fail 'Original configuration/data paths required.'
# The Agent refuses to enroll while its data directory holds anything it does
# not know, so the readable copy lives in a private directory of its own.
temporary_directory=$(mktemp -d /run/proxy-agent-enrollment.XXXXXXXX)
temporary=$temporary_directory/token
trap 'rm -f -- "$temporary"; rmdir -- "$temporary_directory" 2>/dev/null || true' EXIT
install -o proxy-agent -g proxy-agent -m 0600 -- "$token" "$temporary"
chown proxy-agent:proxy-agent -- "$temporary_directory"
runuser -u proxy-agent -- "$binary" --config /etc/proxy-agent/config.yaml --enroll --token-file "$temporary"
printf 'Node enrolled. No proxy core/service started. The source token file is retained; remove it securely after recording successful enrollment.\n'
