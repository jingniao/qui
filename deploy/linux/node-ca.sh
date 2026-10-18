#!/usr/bin/env bash
# Local node CA-trust helper for a stopped agent.
#
# A node trusts the panel CA that its configuration names in tls.client_ca. After
# a panel CA rotation the operator installs the new CA here and enrolls the node
# again, so this script only performs that local file operation.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
fail(){ printf 'Node CA operation refused: %s\n' "$*" >&2; exit 1; }
usage(){
  cat <<'USAGE'
Usage: node-ca.sh --inspect [--ca-path FILE] [--data-directory DIR]
       node-ca.sh --trust CA_FILE [--ca-path FILE] [--data-directory DIR]

--inspect prints the node identity material, the trusted control CA and the
agent service state. It changes nothing.

--trust validates CA_FILE with openssl, installs it as the trusted control CA
with mode 0644 root:proxy-agent, and keeps the previous file as FILE.previous.
The agent must be stopped. Enroll the node again afterwards.

Defaults: --ca-path /etc/proxy-agent/tls/ca.crt, --data-directory /var/lib/proxy-agent
USAGE
}
action=${1:-}; shift || true
source=''
if [[ $action == --trust ]]; then
  (($# >= 1)) || fail 'Use --trust CA_FILE.'
  source=$1; shift
fi
ca_path=/etc/proxy-agent/tls/ca.crt
data=/var/lib/proxy-agent
while (($#)); do
  case $1 in
    --ca-path) (($# >= 2)) || fail 'Missing value for --ca-path.'; ca_path=$2; shift 2;;
    --data-directory) (($# >= 2)) || fail 'Missing value for --data-directory.'; data=$2; shift 2;;
    --help) usage; exit 0;;
    *) fail "Unknown option $1";;
  esac
done
case $action in --inspect|--trust) ;; *) usage >&2; exit 2;; esac
[[ $ca_path == /* && $data == /* ]] || fail 'Use absolute paths.'
[[ $(uname -s) == Linux && $EUID -eq 0 ]] || fail 'Run locally as root on Linux.'
for tool in systemctl stat sha256sum install cmp rm sed flock; do command -v "$tool" >/dev/null || fail "Missing command: $tool"; done
[[ -d $data && ! -L $data ]] || fail 'Missing agent data directory.'
registry=/var/lib/proxycontrol-install
[[ -d $registry && ! -L $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail 'No private installation registry.'
[[ ! -L $registry/operation.lock ]] || fail 'Unsafe operation lock.'
exec 9>"$registry/operation.lock"
flock -n 9 || fail 'Another local installation operation is running.'
record=$registry/agent.manifest
[[ -f $record && ! -L $record && $(stat -c '%u:%a' "$record") == 0:600 && $(sed -n '7p' "$record") == installed ]] || fail 'No completed Agent installation.'
[[ $(sed -n '1p' "$record") == proxycontrol-install-v1 && $(sed -n '2p' "$record") == agent ]] || fail 'Unknown installation record.'
account=proxy-agent
[[ $(id -u "$account") != 0 ]] || fail 'Service account cannot be root.'
service_state=$(systemctl show --property=ActiveState --value proxy-agent.service)

report(){
  local file=$1 digest
  if [[ ! -e $file && ! -L $file ]]; then printf '%s: absent\n' "${file##*/}"; return; fi
  [[ -f $file && ! -L $file ]] || fail "Not a regular file: $file"
  digest=$(sha256sum -- "$file"); digest=${digest%% *}
  printf '%s: %s %s %s\n' "${file##*/}" "$(stat -c '%u:%g %a' "$file")" "$(stat -c '%s' "$file")" "$digest"
}
printf 'data directory: %s\n' "$data"
printf 'agent service: %s\n' "$service_state"
report "$data/identity/client.crt"
report "$data/identity/client.key"
report "$ca_path"
if [[ $action == --inspect ]]; then
  printf 'Inspection only; no material was changed.\n'
  exit 0
fi
[[ $service_state == inactive || $service_state == failed ]] || fail 'Stop proxy-agent.service before installing a trusted CA.'
[[ -f $source && ! -L $source ]] || fail 'The new CA must be a regular local file.'
[[ $(stat -c '%s' "$source") -le 65536 ]] || fail 'The new CA file is larger than 64 KiB.'
grep -q -- '-----BEGIN CERTIFICATE-----' "$source" || fail 'The new CA file carries no PEM certificate.'
grep -q -- '-----END CERTIFICATE-----' "$source" || fail 'The new CA file carries no complete PEM certificate.'
if grep -q -- 'PRIVATE KEY' "$source"; then fail 'Refuse a file that carries private key material.'; fi
command -v openssl >/dev/null || fail 'openssl is required to validate the new CA.'
openssl x509 -in "$source" -noout -checkend 0 >/dev/null 2>&1 || fail 'The new CA certificate is not currently valid.'
openssl x509 -in "$source" -noout -text 2>/dev/null | grep -q 'CA:TRUE' || fail 'The new certificate is not a CA certificate.'
install -d -o root -g "$account" -m 0750 "${ca_path%/*}"
if [[ -e $ca_path ]]; then
  [[ -f $ca_path && ! -L $ca_path ]] || fail 'The current trusted CA is not a regular file.'
  install -o root -g root -m 0600 -- "$ca_path" "$ca_path.previous"
  sync -f -- "$ca_path.previous"
  printf 'Previous trusted CA retained as %s.previous\n' "$ca_path"
fi
install -o root -g "$account" -m 0644 -- "$source" "$ca_path.new"
sync -f -- "$ca_path.new"
mv -T -- "$ca_path.new" "$ca_path"
sync -f -- "${ca_path%/*}"
printf 'Trusted control CA installed at %s.\n' "$ca_path"
printf 'Start proxy-agent.service and enroll the node again with deploy/linux/enroll.sh --token-file FILE.\n'
