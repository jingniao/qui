#!/usr/bin/env bash
# Local installer and in-place upgrader for the panel or a node agent.
#
# It installs the binary, the service account, the fixed AGENTS.md section 30
# directories and the packaged service definition: the systemd unit on a systemd
# host, the OpenRC init scripts on an Alpine host. Only --node is supported on
# OpenRC; the panel needs systemd. For a node it also installs an official Xray
# or sing-box release after verifying its SHA256. Nothing is started, and an
# existing configuration, identity or data is never touched, so re-running the
# script upgrades the program in place.
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

# The init-system helpers (deploy/linux/service.sh) are sourced, never executed.
# The release archive carries a copy next to this script, and install.sh also
# publishes one to /usr/local/lib/proxycontrol/service.sh for the scripts that
# are not run from an archive (the installed qui-update, or an uninstall.sh
# extracted on its own). The sibling wins; anything but a root-owned regular
# file is refused rather than read as root.
service_library=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/service.sh
if [[ ! -f $service_library ]]; then service_library=/usr/local/lib/proxycontrol/service.sh; fi
[[ -f $service_library && ! -L $service_library && $(stat -c '%u' "$service_library" 2>/dev/null) == 0 ]] \
  || fail "Missing the init-system library service.sh: $service_library"
# shellcheck source=service.sh
. "$service_library"

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
# The init-system library is installed once per host, like the updater, so the
# scripts that do not run from an archive can source it.
library=/usr/local/lib/proxycontrol/service.sh
# The boot-time hook that finishes an update qui-update could not finish is
# installed once per host as well, and by both components: a host runs at most one
# of them and the recovery must run before whichever one starts.
recovery=/usr/local/lib/proxycontrol/update-recovery
registry=/var/lib/proxycontrol-install
record=$registry/$component.manifest
removed=$registry/$component.removed
core_target=''
[[ -z $core ]] || core_target=/usr/local/lib/proxy-agent/$core
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)

[[ $(uname -s) == Linux && $EUID -eq 0 ]] || fail 'Run locally as root on Linux.'
init_system=$(detect_init) || fail 'Neither systemd nor OpenRC is running on this host.'
# The panel owns its own service, its database directory and the terminal menu,
# all of which are written for systemd; a node agent runs on either init system.
if [[ $component == panel && $init_system != systemd ]]; then
  fail 'The panel requires systemd; on OpenRC only --node is supported.'
fi
# The service definitions follow the init system: systemd takes units from
# deploy/systemd, OpenRC takes init scripts from deploy/openrc. The enforced
# outbound source keeps both variable names on each: on systemd it is a oneshot
# unit plus a path unit, on OpenRC an init script plus the inotify watcher that
# script supervises.
if [[ $init_system == systemd ]]; then
  service_directory=$root/systemd
  unit=/etc/systemd/system/$name.service
  service_mode=0644
  host_network_files=(proxy-agent-host-network.service proxy-agent-host-network.path)
  host_network_service=/etc/systemd/system/proxy-agent-host-network.service
  host_network_path=/etc/systemd/system/proxy-agent-host-network.path
  recovery_service=/etc/systemd/system/proxycontrol-update-recovery.service
else
  service_directory=$root/openrc
  unit=/etc/init.d/$name
  service_mode=0755
  host_network_files=(proxy-agent-host-network host-network-watch)
  host_network_service=/etc/init.d/proxy-agent-host-network
  host_network_path=/usr/local/lib/proxy-agent/host-network-watch
  recovery_service=/etc/init.d/proxycontrol-update-recovery
fi
if [[ $init_system == openrc ]] && command -v apk >/dev/null; then
  # A stock Alpine host has neither bash (the interpreter of this script) nor
  # curl, ca-certificates, GNU coreutils/findutils/tar, shadow (useradd and
  # groupadd), runuser or nftables. They are installed here, before the tool
  # check, so the rest of the script needs no second dialect; repeating the
  # command on a host that already has them is a no-op.
  apk add --no-cache bash curl ca-certificates coreutils findutils grep sed tar unzip shadow runuser nftables \
    || fail 'Installing the required Alpine packages failed: run "apk add --no-cache bash curl ca-certificates coreutils findutils grep sed tar unzip shadow runuser nftables" and retry.'
fi
required_tools=(install sha256sum od stat getent useradd groupadd flock id tr sed chmod rm mktemp rmdir sync mv ln dirname find grep head mkdir)
if [[ $init_system == systemd ]]; then
  required_tools+=(systemctl)
else
  required_tools+=(rc-service rc-update openrc-run)
fi
for tool in "${required_tools[@]}"; do
  command -v "$tool" >/dev/null || fail "Required command unavailable: $tool"
done
if [[ $init_system == openrc ]]; then
  # supervise-daemon learned the capabilities option in OpenRC 0.45. An older
  # release cannot give the Agent CAP_NET_BIND_SERVICE, so a core could not bind
  # a port below 1024 and the node would publish nothing that works.
  openrc_version=$(openrc --version 2>/dev/null || true)
  openrc_version=$(printf '%s\n' "$openrc_version" | grep -o -E '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n 1 || true)
  [[ $openrc_version =~ ^([0-9]+)\.([0-9]+)(\.[0-9]+)?$ ]] \
    || fail 'Cannot determine the OpenRC version (openrc --version).'
  if ((BASH_REMATCH[1] == 0 && BASH_REMATCH[2] < 45)); then
    fail "OpenRC 0.45 or newer is required for supervise-daemon capabilities (found $openrc_version)."
  fi
fi
[[ -n $binary && $digest =~ ^[0-9a-f]{64}$ ]] || fail 'Provide the binary and its trusted lowercase SHA256.'
packaged_service=$service_directory/${unit##*/}
[[ -f $packaged_service && ! -L $packaged_service ]] || fail "Missing packaged service file: ${unit##*/}"
# The recovery service definition comes from the same directory as the unit: the
# systemd unit on a systemd host, the init script on an Alpine host.
packaged_recovery_service=$service_directory/${recovery_service##*/}
[[ -f $packaged_recovery_service && ! -L $packaged_recovery_service ]] || fail "Missing packaged service file: ${recovery_service##*/}"
if [[ $component == agent ]]; then
  for packaged in "${host_network_files[@]}"; do
    [[ -f $service_directory/$packaged && ! -L $service_directory/$packaged ]] || fail "Missing packaged service file: $packaged"
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
for path in /usr/local /usr/local/bin /usr/local/lib /usr/local/lib/proxy-agent /usr/local/lib/proxycontrol /var/lib /run/lock "$etc" "$data" "$data/host-network" "$registry"; do
  [[ ! -L $path ]] || fail "Symlinked destination: $path"
done
if [[ $init_system == systemd ]]; then
  for path in /etc/systemd /etc/systemd/system; do
    [[ ! -L $path ]] || fail "Symlinked destination: $path"
  done
else
  [[ ! -L /etc/init.d ]] || fail 'Symlinked destination: /etc/init.d'
fi
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
# stops the service, installs, and starts it again.
service_is_active "$name" && fail "Stop $name before installing or upgrading (systemctl stop $name.service, or rc-service $name stop)."
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

# Install the package that provides a command this script needs on a systemd host
# when the command is missing. The enforced outbound source needs nft: the Agent
# runs it only from the packaged helper unit, and only when the panel publishes a
# host source address, so the package is installed here instead of being required
# at boot. The Xray release is a zip, and a minimal Debian or Ubuntu install has no
# unzip. Alpine gets both from the apk line above.
ensure_command() {
  local command=$1 package=$2 purpose=$3
  if command -v "$command" >/dev/null; then return; fi
  if command -v apt-get >/dev/null; then
    DEBIAN_FRONTEND=noninteractive apt-get install --yes --no-install-recommends "$package" \
      || { DEBIAN_FRONTEND=noninteractive apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install --yes --no-install-recommends "$package"; }
  elif command -v dnf >/dev/null; then
    dnf install --assumeyes "$package"
  elif command -v yum >/dev/null; then
    yum install --assumeyes "$package"
  else
    fail "$command is required $purpose; install the $package package of this distribution and run this again."
  fi
  command -v "$command" >/dev/null || fail "The $package package did not provide $command."
}

# The Agent's core-lifetime check
# (internal/agent/core/lifetime_environment_linux.go) identifies a boot by the
# machine id, the kernel boot id and the pid namespace link, and only concludes
# that a core died uncleanly when all three are readable. Nothing creates
# /etc/machine-id without systemd or dbus, so a stock Alpine container has none,
# and without a valid one an unclean core exit can never be confirmed: the data
# plane stays blocked. A valid file is never overwritten, because it is the
# host's identity.
ensure_machine_id() {
  local path=/etc/machine-id source=/var/lib/dbus/machine-id value=''
  # systemd and dbus own this file on their hosts: it is always there and it is
  # the identity of the machine, so an install on systemd must not read, rewrite
  # or refuse it. Only a host without either (Alpine, or an LXC container of it)
  # can lack it, and only there does the Agent need it created.
  if [[ $init_system == systemd ]]; then return 0; fi
  if [[ -f $path ]]; then
    value=$(tr -d '\n' < "$path")
    [[ $value =~ ^[0-9a-fA-F]{32}$ ]] && return 0
  fi
  # Written through a rename, so a symlink at this path is refused instead of
  # being followed as root.
  [[ ! -L $path ]] || fail "$path is a symbolic link; replace it with a regular 32-hex-character machine id and retry."
  if [[ -f $source && ! -L $source ]]; then
    value=$(tr -d '\n' < "$source")
  fi
  if [[ ! $value =~ ^[0-9a-fA-F]{32}$ ]]; then
    value=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
  fi
  [[ $value =~ ^[0-9a-fA-F]{32}$ ]] || fail 'Cannot produce a 32-hex-character machine id.'
  printf '%s\n' "$value" > "$stage/machine-id"
  publish "$stage/machine-id" "$path" root root 0444
  printf 'Created %s (%s); the Agent needs it to confirm core lifetimes.\n' "$path" "$value"
}

host_architecture() {
  case $(uname -m) in x86_64) printf 'amd64';; aarch64|arm64) printf 'arm64';; *) fail 'Unsupported machine architecture.';; esac
}
# The official sing-box release ships a glibc build and a musl build per
# architecture, and only the second one starts on Alpine: the plain archive is
# linked against the glibc loader, so the kernel answers "not found" for a binary
# that is present. The loader is looked up in the directory given as the first
# argument (default /lib), so the regression test
# (deploy/linux/tests/core-asset-test.sh) can hand it a directory of its own.
host_libc() {
  local directory=${1:-/lib} loader
  for loader in "$directory"/ld-musl-*.so.1; do
    if [[ -e $loader ]]; then printf 'musl'; return; fi
  done
  printf 'glibc'
}
# The Xray release is one static binary per architecture, whatever the libc.
# sing-box depends on it, so its asset carries the -musl suffix on a musl host;
# the caller may name the libc as the second argument, which the test does, and
# the installer leaves it to host_libc.
core_asset() {
  local architecture libc
  architecture=$(host_architecture)
  libc=${2:-$(host_libc)}
  case $architecture:$1 in
    amd64:xray) printf 'Xray-linux-64.zip';;
    arm64:xray) printf 'Xray-linux-arm64-v8a.zip';;
    amd64:sing-box)
      if [[ $libc == musl ]]; then printf 'sing-box-%s-linux-amd64-musl.tar.gz' "$core_version"
      else printf 'sing-box-%s-linux-amd64.tar.gz' "$core_version"; fi;;
    arm64:sing-box)
      if [[ $libc == musl ]]; then printf 'sing-box-%s-linux-arm64-musl.tar.gz' "$core_version"
      else printf 'sing-box-%s-linux-arm64.tar.gz' "$core_version"; fi;;
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
    # --no-insecure overrides a -k that a curl wrapper or alias may add in front
    # of the arguments, so the release is never fetched without verified TLS.
    curl --no-insecure --fail --silent --show-error --location --max-redirs 3 --proto '=https' --tlsv1.2 --output "$destination" -- "$url"
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

# The temporary file the current publish() is writing. It is global because a failure
# inside publish has to remove it, and because cleanup() removes it after a signal as
# well: an install interrupted between the copy and the rename used to leave a
# full-size dot file next to the destination (in the field, a 20 MB
# /usr/local/bin/.proxy-agent.installing.<pid> after the rename had been refused by an
# immutable target).
publishing_temporary=''

# Publish a staged file over its destination in one rename, so an interrupted
# install never leaves a half-written program, unit or record behind.
publish() {
  local source=$1 destination=$2 owner=$3 group=$4 mode=$5 temporary
  temporary=${destination%/*}/.${destination##*/}.installing.$$
  publishing_temporary=$temporary
  if ! install -o "$owner" -g "$group" -m "$mode" -- "$source" "$temporary" \
    || ! sync -f -- "$temporary" \
    || ! mv -T -- "$temporary" "$destination"; then
    rm -f -- "$temporary"
    publishing_temporary=''
    return 1
  fi
  publishing_temporary=''
  sync -f -- "${destination%/*}"
}

stage=$(mktemp -d "$registry/staging.XXXXXXXX")
# True once the first publish() has replaced a file. From that point a failure leaves a
# mix of old and new files behind, so cleanup() reports that instead of keeping the
# staging directory: a retained directory is 20-40 MB per failed attempt and piles up
# in the registry, while the staged material is rebuilt by re-running the same
# install.sh anyway.
publishing_started=false
cleanup() {
  if [[ -n $publishing_temporary ]]; then rm -f -- "$publishing_temporary"; fi
  if $publishing_started; then
    printf 'Installation stopped after it had begun replacing files: the installed program and service files may be a mix of the old and the new version. Re-running the same install.sh completes the upgrade, and qui-update restores the previous version by itself.\n' >&2
  fi
  [[ $stage == "$registry"/staging.* ]] || return 1
  rm -f -- "$stage/program" "$stage/unit" "$stage/qui" "$stage/qui-update" "$stage/config" "$stage/core" "$stage/archive" "$stage/asset.dgst" "$stage/installed-manifest" "$stage/host-network-service" "$stage/host-network-path" "$stage/service-library" "$stage/update-recovery" "$stage/update-recovery-service" "$stage/machine-id"
  if [[ -d $stage/extract ]]; then rm -rf -- "$stage/extract"; fi
  rmdir -- "$stage"
}
trap cleanup EXIT
install -o root -g root -m 0600 -- "$binary" "$stage/program"
verify_binary "$stage/program" "$digest"
install -o root -g root -m 0600 -- "$packaged_service" "$stage/unit"
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
# The init-system library serves both components too and is installed once per
# host, because the installed qui-update and a uninstall.sh that was extracted on
# its own have no sibling to source it from. The copy this script itself resolved
# is what gets published, so an archive always carries its own version.
[[ -f $service_library && ! -L $service_library ]] || fail "Missing the init-system library: $service_library"
install -o root -g root -m 0600 -- "$service_library" "$stage/service-library"
# The boot-time recovery hook and its service definition serve both components and
# are staged like the library. They are deliberately not recorded in the install
# manifest, whose fixed line numbers every other script reads, and deliberately not
# part of the files qui-update snapshots: a rollback to an older version has to
# keep them, because the older qui-update still writes the journals this hook
# recovers, and without a journal they do nothing at all.
[[ -f $root/linux/update-recovery.sh && ! -L $root/linux/update-recovery.sh ]] || fail 'Missing packaged update recovery script.'
install -o root -g root -m 0600 -- "$root/linux/update-recovery.sh" "$stage/update-recovery"
install -o root -g root -m 0600 -- "$packaged_recovery_service" "$stage/update-recovery-service"
host_network_service_digest=''
host_network_path_digest=''
if [[ $component == agent ]]; then
  # On systemd these are the oneshot unit and the path unit that arms it; on
  # OpenRC the init script and the watcher that script supervises. The digests
  # are taken over the staged files whatever the init system, so uninstall.sh
  # verifies the same two record lines on both.
  install -o root -g root -m 0600 -- "$service_directory/${host_network_files[0]}" "$stage/host-network-service"
  install -o root -g root -m 0600 -- "$service_directory/${host_network_files[1]}" "$stage/host-network-path"
  host_network_service_digest=$(sha256sum -- "$stage/host-network-service"); host_network_service_digest=${host_network_service_digest%% *}
  host_network_path_digest=$(sha256sum -- "$stage/host-network-path"); host_network_path_digest=${host_network_path_digest%% *}
  if [[ $init_system == systemd ]]; then
    ensure_command nft nftables 'for the enforced outbound source'
  else
    # Alpine installs nftables from apk above; package managers are not driven
    # from here on OpenRC, but the helper still needs the binary.
    command -v nft >/dev/null || fail 'nft is required for the enforced outbound source: run "apk add --no-cache nftables" and retry.'
  fi
fi
# The staging directory exists by now and publish() is defined above it, so the
# Agent's machine id can be published before the program itself.
if [[ $component == agent ]]; then ensure_machine_id; fi

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
      if [[ $init_system == systemd ]]; then ensure_command unzip unzip 'to extract the Xray release archive'; fi
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
  # Debian and Ubuntu keep nologin in /usr/sbin, Alpine (shadow) in /sbin. The
  # first one that exists is used, so the account never gets a login shell.
  nologin=/usr/sbin/nologin
  [[ -f $nologin ]] || nologin=/sbin/nologin
  [[ -f $nologin ]] || fail 'Neither /usr/sbin/nologin nor /sbin/nologin exists; install the shadow/login package first.'
  useradd --system --gid "$account" --home-dir "$data" --no-create-home --shell "$nologin" "$account"
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

publishing_started=true
publish "$stage/program" "$target" root root 0755
publish "$stage/unit" "$unit" root root "$service_mode"
if [[ $component == panel ]]; then publish "$stage/qui" "$qui" root root 0755; fi
publish "$stage/qui-update" "$update" root root 0755
# /usr/local/lib/proxycontrol holds the init-system library and the boot-time
# update recovery hook; it is created here so an install that is not run from an
# archive can source the library and run the recovery later.
install -d -o root -g root -m 0755 /usr/local/lib/proxycontrol
publish "$stage/service-library" "$library" root root 0644
publish "$stage/update-recovery" "$recovery" root root 0755
# The recovery runs before every service of both components, so its definition is
# published together with them: the systemd unit (0644) or the OpenRC init script
# (0755), which is the mode the component's own definition gets as well.
publish "$stage/update-recovery-service" "$recovery_service" root root "$service_mode"
if [[ -n $host_network_service_digest ]]; then
  if [[ $init_system == systemd ]]; then
    publish "$stage/host-network-service" "$host_network_service" root root 0644
    publish "$stage/host-network-path" "$host_network_path" root root 0644
  else
    # The OpenRC watcher is a program, not a service definition, and it lives
    # next to the core in /usr/local/lib/proxy-agent. Today that directory is
    # only created when a core is installed, so it is created here as well.
    install -d -o root -g root -m 0755 /usr/local/lib/proxy-agent
    publish "$stage/host-network-service" "$host_network_service" root root 0755
    publish "$stage/host-network-path" "$host_network_path" root root 0755
  fi
fi
if [[ -n $config_source ]]; then publish "$stage/config" "$etc/config.yaml" "$account_uid" "$account_gid" 0600; fi
if [[ -n $core ]]; then
  install -d -o root -g root -m 0755 /usr/local/lib/proxy-agent
  publish "$stage/core" "$core_target" root root 0755
fi
service_reload_definitions
# Enabled, never started: when qui-update runs this install.sh, the journal of
# that very update is in place, and a recovery started now would try to undo the
# update in progress (it would wait for qui-update's lock and then give up). It
# only has work at boot; on every other boot it finds no journal.
service_enable proxycontrol-update-recovery
if [[ -n $host_network_service_digest ]]; then
  if [[ $init_system == systemd ]]; then
    # Enabled and armed, while no proxy service is started by this script: the path
    # unit only watches the desired file the Agent publishes. Arming it here is what
    # lets the first publish of an installation that has not rebooted yet reach the
    # helper, and the oneshot unit reapplies the retained file at every boot.
    # systemctl reads an absolute path as a mount unit for start and stop, so
    # every call names the units.
    systemctl enable "${host_network_path##*/}" "${host_network_service##*/}"
    systemctl start "${host_network_path##*/}"
    # An installation upgraded from a release without the start-limit fix can
    # have the oneshot unit in the failed state, so nothing has been applied
    # since the path unit hit its limit. Replaying the retained file once is what
    # a boot does (and what restarting the OpenRC watcher does) and the outcome
    # goes to status.json for the Agent. --no-block keeps a helper failure - a
    # rule the host rejects, for instance - from failing this install.
    if systemctl is-failed --quiet "${host_network_service##*/}"; then
      systemctl reset-failed "${host_network_service##*/}"
      systemctl start --no-block "${host_network_service##*/}"
    fi
  else
    # On OpenRC the watcher is one supervised service and there is no path unit:
    # it has to be in the default runlevel so a boot reapplies the retained rule,
    # and restarting it when it already runs is what makes an upgraded watcher
    # take effect. Both apply only what the panel publishes, as on systemd.
    service_enable proxy-agent-host-network
    if service_is_active proxy-agent-host-network; then
      service_restart proxy-agent-host-network
    else
      service_start proxy-agent-host-network
    fi
  fi
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
# Line 10 names the init system the service definitions were written for, so
# uninstall.sh can refuse to remove a service with the tooling of the other one.
# It is appended last to keep the fixed line numbers above, and a systemd record
# keeps the original file contents: no line, which reads as systemd.
if [[ $init_system == openrc ]]; then printf 'openrc\n' >> "$stage/installed-manifest"; fi
chmod 0600 "$stage/installed-manifest"
publish "$stage/installed-manifest" "$record" root root 0600
if $reinstall; then rm -- "$removed"; sync -f -- "$registry"; fi
verify_binary "$target" "$digest"
publishing_started=false

printf '%s installed at %s with unit %s.\n' "$name" "$target" "$unit"
if [[ -n $core ]]; then printf 'Core %s %s installed at %s.\n' "$core" "$core_version" "$core_target"; fi
printf 'Existing configuration, identity and data were left untouched and nothing was started.\n'
if [[ $init_system == systemd ]]; then
  printf 'Next: review %s/config.yaml, then systemctl enable --now %s.service.\n' "$etc" "$name"
else
  printf 'Next: review %s/config.yaml, then rc-update add %s default && rc-service %s start.\n' "$etc" "$name" "$name"
  printf 'The service logs through logger into /var/log/messages.\n'
fi
if [[ $component == panel ]]; then printf 'Run qui for the terminal menu.\n'; fi
if [[ -n $host_network_service_digest ]]; then
  if [[ $init_system == systemd ]]; then
    printf 'The enforced-source units %s and %s are enabled and armed; they apply only what the panel publishes.\n' "${host_network_service##*/}" "${host_network_path##*/}"
  else
    printf 'The enforced-source service %s and the watcher %s are enabled and started; they apply only what the panel publishes.\n' "${host_network_service##*/}" "${host_network_path##*/}"
  fi
fi
if [[ $component == agent ]]; then printf 'Then enroll the node with deploy/linux/enroll.sh --token-file FILE.\n'; fi
printf 'Managed agent and core upgrades are deployed from the panel.\n'
printf 'An interrupted qui-update is finished or rolled back at boot by proxycontrol-update-recovery.\n'
printf 'Run qui-update to update from GitHub.\n'
