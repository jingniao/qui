#!/usr/bin/env bash
# Fixed local enrollment operation. Does not start a core or a system service.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
fail(){ printf 'Enrollment refused: %s\n' "$*" >&2; exit 1; }
if [[ ${1:-} == --help ]]; then printf 'Usage: enroll.sh --token-file FILE\nUses fixed installed Agent/config as proxy-agent, then exits. Original token file is retained.\n'; exit 0; fi
[[ $# == 2 && $1 == --token-file ]] || fail 'Use --token-file FILE.'
[[ $(uname -s) == Linux && $EUID == 0 ]] || fail 'Run locally as root on Linux.'
for tool in systemctl runuser stat install mktemp rm sha256sum sed flock; do command -v "$tool" >/dev/null || fail "Missing command: $tool"; done
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
[[ $(sed -n '1p' "$record") == proxycontrol-install-v1 && $(sed -n '2p' "$record") == agent ]] || fail 'Unknown installation record format.'
binary=/usr/local/bin/proxy-agent
[[ -f $binary && ! -L $binary && $(stat -c '%u' "$binary") == 0 ]] || fail 'Installed Agent must be root-owned.'
digest=$(sha256sum -- "$binary");digest=${digest%% *}
[[ $digest == "$(sed -n '3p' "$record")" ]] || fail 'Installed launcher differs from original installation.'
systemctl is-active --quiet proxy-agent.service && fail 'Stop the Agent service before enrollment.'
[[ -d /var/lib/proxy-agent && ! -L /var/lib/proxy-agent && -f /etc/proxy-agent/config.yaml && ! -L /etc/proxy-agent/config.yaml ]] || fail 'Original configuration/data paths required.'
temporary=$(mktemp /var/lib/proxy-agent/.enrollment.XXXXXXXX)
trap 'rm -f -- "$temporary"' EXIT
install -o proxy-agent -g proxy-agent -m 0600 -- "$token" "$temporary"
runuser -u proxy-agent -- "$binary" --config /etc/proxy-agent/config.yaml --enroll --token-file "$temporary"
printf 'Node enrolled. No proxy core/service started. The source token file is retained; remove it securely after recording successful enrollment.\n'
