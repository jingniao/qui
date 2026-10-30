#!/usr/bin/env bash
# Local panel node-CA helper for a stopped panel.
#
# The panel owns exactly one node CA and loads or creates it in
# node_identity_directory. Rotating it is a local file operation: keep a verified
# copy of the current CA material, remove ca.crt and ca.key, start the panel so
# it creates a new CA, then disable and re-enroll every node.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
fail(){ printf 'Panel CA operation refused: %s\n' "$*" >&2; exit 1; }
usage(){
  cat <<'USAGE'
Usage: panel-ca.sh --inspect [--identity-directory DIR]
       panel-ca.sh --rotate BACKUP_DIRECTORY [--identity-directory DIR]

--inspect prints the current CA material, the private configuration key and the
panel service state. It changes nothing.

--rotate copies ca.crt, ca.key and configuration.key into BACKUP_DIRECTORY with
mode 0600, verifies every copy by SHA256, and then removes ca.crt and ca.key so
that the next panel start creates a new node CA. The panel must be stopped, and
the backup directory must not exist yet. Every node certificate is refused
afterwards, so every node must come back as a new node record: create the node
again in the panel, clear the node's local identity and enroll it with a fresh
one-time code (see docs/operations.md section 4).

Default identity directory: /var/lib/proxy-panel/identity
USAGE
}
action=${1:-}; shift || true
backup=''
if [[ $action == --rotate ]]; then
  (($# >= 1)) || fail 'Use --rotate BACKUP_DIRECTORY.'
  backup=$1; shift
fi
identity=/var/lib/proxy-panel/identity
while (($#)); do
  case $1 in
    --identity-directory) (($# >= 2)) || fail 'Missing value for --identity-directory.'; identity=$2; shift 2;;
    --help) usage; exit 0;;
    *) fail "Unknown option $1";;
  esac
done
case $action in --inspect|--rotate) ;; *) usage >&2; exit 2;; esac
[[ $identity == /* ]] || fail 'Identity directory must be an absolute path.'
[[ $(uname -s) == Linux && $EUID -eq 0 ]] || fail 'Run locally as root on Linux.'
for tool in systemctl stat sha256sum install cmp rm sed flock getent id; do command -v "$tool" >/dev/null || fail "Missing command: $tool"; done
[[ -d $identity && ! -L $identity ]] || fail 'Missing panel identity directory.'
[[ $(stat -c '%a' "$identity") == 700 ]] || fail 'Panel identity directory must have mode 0700.'
# The packaged install creates the directory as the panel service account; a
# hand-made install may own it as root. Nothing else may own it.
panel_account=proxy-panel
identity_owner=$(stat -c '%u' "$identity")
if [[ $identity_owner != 0 ]]; then
  getent passwd "$panel_account" >/dev/null || fail "Panel identity directory must belong to root or $panel_account."
  [[ $identity_owner == "$(id -u "$panel_account")" ]] || fail "Panel identity directory must belong to root or $panel_account."
fi
registry=/var/lib/proxycontrol-install
[[ -d $registry && ! -L $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail 'No private installation registry.'
[[ ! -L $registry/operation.lock ]] || fail 'Unsafe operation lock.'
exec 9>"$registry/operation.lock"
flock -n 9 || fail 'Another installation operation is running.'
record=$registry/panel.manifest
[[ -f $record && ! -L $record && $(stat -c '%u:%a' "$record") == 0:600 && $(sed -n '7p' "$record") == installed ]] || fail 'No completed Panel installation.'
qui_record_cores "$record" panel || fail 'Unknown installation record.'

report(){
  local file=$1 digest
  if [[ ! -e $file && ! -L $file ]]; then printf '%s: absent\n' "${file##*/}"; return; fi
  [[ -f $file && ! -L $file ]] || fail "Not a regular file: $file"
  digest=$(sha256sum -- "$file"); digest=${digest%% *}
  printf '%s: %s %s %s\n' "${file##*/}" "$(stat -c '%u:%g %a' "$file")" "$(stat -c '%s' "$file")" "$digest"
}
printf 'identity directory: %s\n' "$identity"
printf 'panel service: %s\n' "$(systemctl show --property=ActiveState --value proxy-panel.service)"
report "$identity/ca.crt"
report "$identity/ca.key"
report "$identity/configuration.key"
if [[ $action == --inspect ]]; then
  printf 'Inspection only; no material was changed.\n'
  exit 0
fi
[[ $backup == /* ]] || fail 'Backup directory must be an absolute path.'
[[ ! -e $backup && ! -L $backup ]] || fail 'Backup directory already exists; keep the earlier copy.'
case $backup in "$identity"|"$identity"/*) fail 'Backup must stay outside the live identity directory.';; esac
service_state=$(systemctl show --property=ActiveState --value proxy-panel.service)
[[ $service_state == inactive || $service_state == failed ]] || fail 'Stop proxy-panel.service before rotating the node CA.'
for material in ca.crt ca.key configuration.key; do
  [[ -f $identity/$material && ! -L $identity/$material ]] || fail "Missing $material in the identity directory."
  [[ $(stat -c '%u' "$identity/$material") == "$identity_owner" ]] || fail "$material must belong to the identity directory owner."
done
install -d -o root -g root -m 0700 -- "$backup"
for material in ca.crt ca.key configuration.key; do
  install -o root -g root -m 0600 -- "$identity/$material" "$backup/$material"
  sync -f -- "$backup/$material"
  cmp -s -- "$identity/$material" "$backup/$material" || fail "Backup copy differs for $material."
done
sync -f -- "$backup"
rm -- "$identity/ca.crt" "$identity/ca.key"
sync -f -- "$identity"
printf 'Previous node CA material retained in %s.\n' "$backup"
printf 'Start proxy-panel.service to create a new node CA. Every node certificate is refused afterwards:\n'
printf 'create each node again in the panel, clear its local identity and enroll it with a fresh one-time code.\n'
printf 'Keep the backup until every node is back; the old CA cannot be restored afterwards.\n'
