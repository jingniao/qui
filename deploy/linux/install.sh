#!/usr/bin/env bash
# Local installer and in-place upgrader for the panel or a node agent.
#
# It installs the binary, the service account, the fixed AGENTS.md section 30
# directories and the packaged systemd unit. For a node it also installs an
# official Xray or sing-box release after verifying its SHA256. Nothing is
# started, and an existing configuration, identity or data is never touched, so
# re-running the script upgrades the program in place.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

fail() { printf 'Installation refused: %s\n' "$*" >&2; exit 1; }
usage() {
  cat <<'USAGE'
Usage: install.sh --panel|--node --binary FILE --sha256 DIGEST [options]

  --binary FILE          proxy-panel or proxy-agent executable to install
  --sha256 DIGEST        lowercase SHA256 of that executable
  --config FILE          config template for a first install
                         (default: the packaged examples/<component>.yaml)
  --core xray|sing-box   install the official core release with --node
  --core-version VERSION release version to download, for example 26.3.27
  --core-url URL         official release asset URL
                         (default: the official GitHub release asset)
  --core-sha256 DIGEST   lowercase SHA256 of the downloaded release archive
                         (default for xray: the release .dgst file)
  --reinstall            install again after uninstall.sh removed the service

Re-running this script upgrades in place: the program and unit are replaced,
the service account, directories, configuration and data are left untouched.
No service is started. Managed agent and core upgrades go through the panel.
USAGE
}

mode=''; binary=''; digest=''; config=''; core=''; core_version=''; core_digest=''; core_url=''; reinstall=false
while (($#)); do
  case $1 in
    --panel|--node)
      [[ -z $mode ]] || fail 'Choose exactly one of --panel and --node.'
      mode=$1; shift;;
    --binary|--sha256|--config|--core|--core-version|--core-sha256|--core-url)
      (($# >= 2)) || fail "Missing value for $1"
      case $1 in
        --binary) binary=$2;; --sha256) digest=${2,,};; --config) config=$2;;
        --core) core=$2;; --core-version) core_version=$2;;
        --core-sha256) core_digest=${2,,};; --core-url) core_url=$2;;
      esac
      shift 2;;
    --reinstall) reinstall=true; shift;;
    --help) usage; exit 0;;
    *) fail "Unknown option $1";;
  esac
done
[[ -n $mode ]] || { usage >&2; exit 2; }
component=panel
if [[ $mode == --node ]]; then component=agent; fi
name=proxy-$component
account=$name
etc=/etc/$name
data=/var/lib/$name
target=/usr/local/bin/$name
# The panel package also ships the root-only terminal menu (deploy/linux/qui.sh)
# and the updater (deploy/linux/update.sh); both go onto panel and node hosts.
qui=/usr/local/bin/qui
update=/usr/local/bin/qui-update
unit=/etc/systemd/system/$name.service
host_network_service=/etc/systemd/system/proxy-agent-host-network.service
host_network_path=/etc/systemd/system/proxy-agent-host-network.path
registry=/var/lib/proxycontrol-install
record=$registry/$component.manifest
removed=$registry/$component.removed
core_target=''
[[ -z $core ]] || core_target=/usr/local/lib/proxy-agent/$core
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)

[[ $(uname -s) == Linux && $EUID -eq 0 ]] || fail 'Run locally as root on Linux.'
for tool in systemctl install sha256sum od stat getent useradd groupadd flock id tr sed chmod rm mktemp rmdir sync mv ln dirname find grep head mkdir; do
  command -v "$tool" >/dev/null || fail "Required command unavailable: $tool"
done
[[ -d /run/systemd/system ]] || fail 'systemd must be running.'
[[ -n $binary && $digest =~ ^[0-9a-f]{64}$ ]] || fail 'Provide the binary and its trusted lowercase SHA256.'
[[ -f $root/systemd/$name.service && ! -L $root/systemd/$name.service ]] || fail 'Missing packaged systemd unit.'
if [[ $component == agent ]]; then
  for packaged in proxy-agent-host-network.service proxy-agent-host-network.path; do
    [[ -f $root/systemd/$packaged && ! -L $root/systemd/$packaged ]] || fail "Missing packaged systemd unit: $packaged"
  done
fi
if [[ -n $core || -n $core_version || -n $core_digest || -n $core_url ]]; then
  [[ $component == agent ]] || fail 'A core is only installed with --node.'
  [[ $core == xray || $core == sing-box ]] || fail 'Core must be xray or sing-box.'
  [[ -n $core_version ]] || fail '--core requires --core-version.'
  [[ $core_version =~ ^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$ ]] || fail 'Invalid core version.'
  if [[ -n $core_digest && ! $core_digest =~ ^[0-9a-f]{64}$ ]]; then fail 'Core archive SHA256 must be lowercase hexadecimal.'; fi
fi
[[ -z $config || -f $config && ! -L $config ]] || fail 'Config template must be a regular local file.'

# Refuse symlinked system destinations rather than traversing them as root.
for path in /usr/local /usr/local/bin /usr/local/lib /usr/local/lib/proxy-agent /etc/systemd /etc/systemd/system /var/lib /run/lock "$etc" "$data" "$data/host-network" "$registry"; do
  [[ ! -L $path ]] || fail "Symlinked destination: $path"
done
install -d -o root -g root -m 0755 /usr/local/bin /usr/local/lib
if [[ -e $registry ]]; then
  [[ -d $registry && ! -L $registry && $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail 'Unsafe installation registry.'
fi
install -d -o root -g root -m 0700 "$registry"
[[ $(stat -c '%u:%a' "$registry") == 0:700 ]] || fail 'Installation registry must be private and root-owned.'
[[ ! -L $registry/operation.lock ]] || fail 'Symlinked installation lock.'
if [[ ! -e $registry/operation.lock ]]; then
  (set -o noclobber; : > "$registry/operation.lock") || fail 'Installation lock creation conflict.'
  chmod 0600 "$registry/operation.lock"; sync -f -- "$registry/operation.lock"
fi
[[ -f $registry/operation.lock && $(stat -c '%u:%a:%h' "$registry/operation.lock") == 0:600:1 ]] || fail 'Installation lock metadata differs.'
exec 9<>"$registry/operation.lock"
flock -n 9 || fail 'Another installation operation is running.'

# Replacing a program under a running service is not an upgrade: the operator
# stops the unit, installs, and starts it again.
systemctl is-active --quiet "$name.service" && fail "Stop $name.service before installing or upgrading."
if [[ -e $removed || -L $removed ]]; then
  $reinstall || fail 'The component was uninstalled; use --reinstall after inspecting the retained record.'
  [[ -f $removed && ! -L $removed && $(stat -c '%u:%a:%h' "$removed") == 0:600:1 ]] || fail 'Unsafe retained uninstall record.'
elif $reinstall; then
  fail '--reinstall requires a retained uninstall record.'
fi

verify_elf() {
  local file=$1 magic machine host kind entry
  [[ -f $file && ! -L $file ]] || fail "Expected a regular binary: $file"
  magic=$(od -An -tx1 -N6 -- "$file" | tr -d ' \n')
  [[ $magic == 7f454c460201 ]] || fail 'Require a 64-bit little-endian ELF executable.'
  kind=$(od -An -tx1 -j16 -N2 -- "$file" | tr -d ' \n')
  entry=$(od -An -tx1 -j24 -N8 -- "$file" | tr -d ' \n')
  [[ ( $kind == 0200 || $kind == 0300 ) && $entry != 0000000000000000 && ${#entry} == 16 ]] || fail 'ELF must have an executable entry point.'
  machine=$(od -An -tx1 -j18 -N2 -- "$file" | tr -d ' \n')
  host=$(uname -m)
  case $host:$machine in x86_64:3e00|aarch64:b700|arm64:b700) ;; *) fail "Binary architecture does not match $host";; esac
}
verify_binary() {
  local file=$1 expected=$2 actual
  actual=$(sha256sum -- "$file"); actual=${actual%% *}
  [[ $actual == "$expected" ]] || fail "SHA256 mismatch: $file"
  verify_elf "$file"
}

# The enforced outbound source needs nftables. The Agent runs nft only from the
# packaged helper unit, and only when the panel publishes a host source address,
# so the package is installed here instead of being required at boot. A node
# without it keeps running; the publish that needs it fails and is retried.
ensure_nft() {
  if command -v nft >/dev/null; then return; fi
  if command -v apt-get >/dev/null; then
    DEBIAN_FRONTEND=noninteractive apt-get install --yes --no-install-recommends nftables \
      || { DEBIAN_FRONTEND=noninteractive apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install --yes --no-install-recommends nftables; }
  elif command -v dnf >/dev/null; then
    dnf install --assumeyes nftables
  elif command -v yum >/dev/null; then
    yum install --assumeyes nftables
  else
    fail 'nft is required for the enforced outbound source; install the nftables package of this distribution and run this again.'
  fi
  command -v nft >/dev/null || fail 'The nftables package did not provide nft.'
}

host_architecture() {
  case $(uname -m) in x86_64) printf 'amd64';; aarch64|arm64) printf 'arm64';; *) fail 'Unsupported machine architecture.';; esac
}
core_asset() {
  case $(host_architecture):$1 in
    amd64:xray) printf 'Xray-linux-64.zip';;
    arm64:xray) printf 'Xray-linux-arm64-v8a.zip';;
    amd64:sing-box) printf 'sing-box-%s-linux-amd64.tar.gz' "$core_version";;
    arm64:sing-box) printf 'sing-box-%s-linux-arm64.tar.gz' "$core_version";;
  esac
}
core_prefix() {
  case $1 in
    xray) printf 'https://github.com/XTLS/Xray-core/releases/download/';;
    sing-box) printf 'https://github.com/SagerNet/sing-box/releases/download/';;
  esac
}
download() {
  local url=$1 destination=$2
  if command -v curl >/dev/null; then
    curl --fail --silent --show-error --location --max-redirs 3 --proto '=https' --tlsv1.2 --output "$destination" -- "$url"
  elif command -v wget >/dev/null; then
    wget --quiet --max-redirect=3 --output-document="$destination" -- "$url"
  else
    fail 'curl or wget is required to download a core release.'
  fi
  [[ -f $destination && ! -L $destination && -s $destination ]] || fail 'Core download produced no archive.'
}



# The expected archive digest comes from the operator or from the release
# digest file that Xray publishes next to the archive. Without one of them the
# release is not installed.
core_archive_digest() {
  local asset=$1 digest_file value
  if [[ -n $core_digest ]]; then printf '%s' "$core_digest"; return; fi
  [[ $core == xray ]] || fail 'Pass --core-sha256: the sing-box release publishes no digest file.'
  digest_file=$stage/asset.dgst
  download "$(core_prefix "$core")v$core_version/$asset.dgst" "$digest_file"
  value=$(grep -i -E '(sha2[-_]?256|sha256)[[:space:]]*=' "$digest_file" | grep -o -E '[0-9a-fA-F]{64}' | head -n 1)
  value=${value,,}
  [[ $value =~ ^[0-9a-f]{64}$ ]] || fail 'The Xray release digest file carries no SHA256 value.'
  printf '%s' "$value"
}

# Publish a staged file over its destination in one rename, so an interrupted
# install never leaves a half-written program, unit or record behind.
publish() {
  local source=$1 destination=$2 owner=$3 group=$4 mode=$5 temporary
  temporary=${destination%/*}/.${destination##*/}.installing.$$
  install -o "$owner" -g "$group" -m "$mode" -- "$source" "$temporary"
  sync -f -- "$temporary"
  mv -T -- "$temporary" "$destination"
  sync -f -- "${destination%/*}"
}

stage=$(mktemp -d "$registry/staging.XXXXXXXX")
keep_stage=false
cleanup() {
  if $keep_stage; then printf 'Installation material retained for inspection: %s\n' "${stage##*/}" >&2; return; fi
  [[ $stage == "$registry"/staging.* ]] || return 1
  rm -f -- "$stage/program" "$stage/unit" "$stage/qui" "$stage/qui-update" "$stage/config" "$stage/core" "$stage/archive" "$stage/asset.dgst" "$stage/installed-manifest" "$stage/host-network-service" "$stage/host-network-path"
  if [[ -d $stage/extract ]]; then rm -rf -- "$stage/extract"; fi
  rmdir -- "$stage"
}
trap cleanup EXIT
install -o root -g root -m 0600 -- "$binary" "$stage/program"
verify_binary "$stage/program" "$digest"
install -o root -g root -m 0600 -- "$root/systemd/$name.service" "$stage/unit"
unit_digest=$(sha256sum -- "$stage/unit"); unit_digest=${unit_digest%% *}
if [[ $component == panel ]]; then
  # The terminal menu is a plain script: it is staged and published like the
  # unit, and deliberately not part of the fixed installed-manifest format that
  # the other scripts read by line number.
  [[ -f $root/linux/qui.sh && ! -L $root/linux/qui.sh ]] || fail 'Missing packaged terminal menu script.'
  install -o root -g root -m 0600 -- "$root/linux/qui.sh" "$stage/qui"
fi
# The updater serves both components, so it is installed on panel and node hosts.
[[ -f $root/linux/update.sh && ! -L $root/linux/update.sh ]] || fail 'Missing packaged updater script.'
install -o root -g root -m 0600 -- "$root/linux/update.sh" "$stage/qui-update"
host_network_service_digest=''
host_network_path_digest=''
if [[ $component == agent ]]; then
  install -o root -g root -m 0600 -- "$root/systemd/proxy-agent-host-network.service" "$stage/host-network-service"
  install -o root -g root -m 0600 -- "$root/systemd/proxy-agent-host-network.path" "$stage/host-network-path"
  host_network_service_digest=$(sha256sum -- "$stage/host-network-service"); host_network_service_digest=${host_network_service_digest%% *}
  host_network_path_digest=$(sha256sum -- "$stage/host-network-path"); host_network_path_digest=${host_network_path_digest%% *}
  ensure_nft
fi

# A first install writes the packaged configuration template. An upgrade keeps
# the configuration that is already in place.
config_source=''
if [[ ! -e $etc/config.yaml ]]; then
  config_source=$config
  [[ -n $config_source ]] || config_source=$root/examples/$component.yaml
  [[ -f $config_source && ! -L $config_source ]] || fail 'Config template must be a regular local file.'
  install -o root -g root -m 0600 -- "$config_source" "$stage/config"
  if [[ $core == sing-box ]]; then
    sed -i 's/^core_name: xray$/core_name: sing-box/;s@^core_path: /usr/local/lib/proxy-agent/xray$@core_path: /usr/local/lib/proxy-agent/sing-box@' "$stage/config"
  fi
fi

core_binary_digest=''
if [[ -n $core ]]; then
  asset=$(core_asset "$core")
  url=$core_url
  if [[ -z $url ]]; then url=$(core_prefix "$core")v$core_version/$asset; fi
  prefix=$(core_prefix "$core")
  remainder=${url#"$prefix"}
  [[ $url == "$prefix"* && $remainder == */"$asset" && ${remainder%"/$asset"} != */* && -n ${remainder%"/$asset"} ]] || fail "Core URL must be an official $core release asset."
  expected=$(core_archive_digest "$asset")
  download "$url" "$stage/archive"
  actual=$(sha256sum -- "$stage/archive"); actual=${actual%% *}
  [[ $actual == "$expected" ]] || fail 'Core archive SHA256 mismatch.'
  mkdir -- "$stage/extract"
  case $asset in
    *.zip)
      command -v unzip >/dev/null || fail 'unzip is required to extract the Xray release archive.'
      unzip -q -o -d "$stage/extract" -- "$stage/archive";;
    *.tar.gz)
      command -v tar >/dev/null || fail 'tar is required to extract the sing-box release archive.'
      tar --extract --gzip --file "$stage/archive" --directory "$stage/extract" --no-same-owner --no-same-permissions;;
    *) fail 'Unsupported core archive container.';;
  esac
  [[ -z $(find "$stage/extract" -type l -print -quit) ]] || fail 'The core archive contains symbolic links.'
  mapfile -t candidates < <(find "$stage/extract" -type f -name "$core")
  [[ ${#candidates[@]} == 1 ]] || fail "The core archive must contain exactly one $core executable."
  install -o root -g root -m 0755 -- "${candidates[0]}" "$stage/core"
  verify_elf "$stage/core"
  core_binary_digest=$(sha256sum -- "$stage/core"); core_binary_digest=${core_binary_digest%% *}
fi

if ! getent group "$account" >/dev/null; then groupadd --system "$account"; fi
if ! getent passwd "$account" >/dev/null; then
  useradd --system --gid "$account" --home-dir "$data" --no-create-home --shell /usr/sbin/nologin "$account"
fi
[[ $(id -u "$account") != 0 && $(id -g "$account") != 0 && $(id -gn "$account") == "$account" ]] || fail 'Service account must be a dedicated non-root account.'
account_uid=$(id -u "$account"); account_gid=$(id -g "$account")
if [[ $component == panel ]]; then
  install -d -o "$account" -g "$account" -m 0750 "$etc" "$etc/tls"
else
  install -d -o root -g "$account" -m 0750 "$etc" "$etc/tls"
fi
install -d -o "$account" -g "$account" -m 0700 "$data"
if [[ $component == agent ]]; then
  # The helper reads its desired file and writes its status file here; it is the
  # only directory the helper unit can write to.
  install -d -o "$account" -g "$account" -m 0700 "$data/host-network"
fi

keep_stage=true
publish "$stage/program" "$target" root root 0755
publish "$stage/unit" "$unit" root root 0644
if [[ $component == panel ]]; then publish "$stage/qui" "$qui" root root 0755; fi
publish "$stage/qui-update" "$update" root root 0755
if [[ -n $host_network_service_digest ]]; then
  publish "$stage/host-network-service" "$host_network_service" root root 0644
  publish "$stage/host-network-path" "$host_network_path" root root 0644
fi
if [[ -n $config_source ]]; then publish "$stage/config" "$etc/config.yaml" "$account_uid" "$account_gid" 0600; fi
if [[ -n $core ]]; then
  install -d -o root -g root -m 0755 /usr/local/lib/proxy-agent
  publish "$stage/core" "$core_target" root root 0755
fi
systemctl daemon-reload
if [[ -n $host_network_service_digest ]]; then
  # Enabled and armed, while no proxy service is started by this script: the path
  # unit only watches the desired file the Agent publishes. Arming it here is what
  # lets the first publish of an installation that has not rebooted yet reach the
  # helper, and the oneshot unit reapplies the retained file at every boot.
  systemctl enable "$host_network_path" "$host_network_service"
  systemctl start "$host_network_path"
fi

# An agent upgrade without --core replaces the program and unit only, so the
# recorded core has to be carried forward: writing an empty core would make
# uninstall.sh leave the installed core behind. The old record is read by the
# same fixed line numbers every other script uses, and a record that is not
# complete or does not name a known core is not trusted and is replaced as
# before. $core itself stays empty here: setting it would trigger a core
# download and the first-install configuration rewrite.
core_record=$core
core_record_digest=$core_binary_digest
if [[ $component == agent && -z $core && -f $record && ! -L $record ]] \
  && [[ $(sed -n '1p' "$record") == proxycontrol-install-v1 ]] \
  && [[ $(sed -n '2p' "$record") == agent ]] \
  && [[ $(sed -n '7p' "$record") == installed ]]; then
  previous_core=$(sed -n '5p' "$record")
  previous_core_digest=$(sed -n '6p' "$record")
  if [[ ( $previous_core == xray || $previous_core == sing-box ) && $previous_core_digest =~ ^[0-9a-f]{64}$ ]]; then
    core_record=$previous_core
    core_record_digest=$previous_core_digest
  fi
fi

printf 'proxycontrol-install-v1\n%s\n%s\n%s\n%s\n%s\ninstalled\n' "$component" "$digest" "$unit_digest" "$core_record" "$core_record_digest" > "$stage/installed-manifest"
# The enforced-source units are recorded after the completion marker so the fixed
# line numbers every other script reads stay unchanged. A record written before
# this feature has no such lines, and uninstall.sh then leaves those units alone.
if [[ -n $host_network_service_digest ]]; then
  printf '%s\n%s\n' "$host_network_service_digest" "$host_network_path_digest" >> "$stage/installed-manifest"
fi
chmod 0600 "$stage/installed-manifest"
publish "$stage/installed-manifest" "$record" root root 0600
if $reinstall; then rm -- "$removed"; sync -f -- "$registry"; fi
verify_binary "$target" "$digest"
keep_stage=false

printf '%s installed at %s with unit %s.\n' "$name" "$target" "$unit"
if [[ -n $core ]]; then printf 'Core %s %s installed at %s.\n' "$core" "$core_version" "$core_target"; fi
printf 'Existing configuration, identity and data were left untouched and nothing was started.\n'
printf 'Next: review %s/config.yaml, then systemctl enable --now %s.service.\n' "$etc" "$name"
if [[ $component == panel ]]; then printf 'Run qui for the terminal menu.\n'; fi
if [[ -n $host_network_service_digest ]]; then
  printf 'The enforced-source units %s and %s are enabled and armed; they apply only what the panel publishes.\n' "${host_network_service##*/}" "${host_network_path##*/}"
fi
if [[ $component == agent ]]; then printf 'Then enroll the node with deploy/linux/enroll.sh --token-file FILE.\n'; fi
printf 'Managed agent and core upgrades are deployed from the panel.\n'
printf 'Run qui-update to update from GitHub.\n'

