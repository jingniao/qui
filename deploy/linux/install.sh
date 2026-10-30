#!/usr/bin/env bash
# Local installer and in-place upgrader for the panel or a node agent.
#
# It installs the binary, the service account, the fixed AGENTS.md section 30
# directories and the packaged service definition: the systemd unit on a systemd
# host, the OpenRC init scripts on an Alpine host. Only --node is supported on
# OpenRC; the panel needs systemd. For a node it also installs an official Xray
# and/or sing-box releases after verifying SHA256. The main service is not
# started. Agent core settings are normalized to the recorded set; identity,
# data and other configuration settings are retained.
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
  --core xray|sing-box   repeatable; default xray on a new node
  --core-source qui|official  sing-box source (default: qui; Xray stays official)
  --qui-version VERSION same qui release as the Agent (required for qui source)
  --core-bundle FILE --core-sums FILE  local qui core archive and SHA256SUMS
  --core-version VERSION version for the preceding --core (pinned default)
  --core-url URL         asset URL for the preceding --core
                         (default: the official GitHub release asset)
  --core-sha256 DIGEST   archive SHA256 for the preceding --core
                         (default for xray: the release .dgst file)
  --reinstall            install again after uninstall.sh removed the service

Re-running this script upgrades in place: the program and unit are replaced,
the service account, directories and data are retained. Agent core settings
are normalized; other configuration is retained. No main service is started. Managed agent and core upgrades go through the panel.
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
cores=(); declare -A core_versions=() core_digests=() core_urls=()
core_source=qui; qui_version=''; core_bundle=''; core_sums=''

qui_release_version() {
  local value=$qui_version bundle_version
  # Release bundles carry their version so the default source follows the Agent.
  if [[ -f $root/../VERSION ]]; then
    bundle_version=$(cat -- "$root/../VERSION")
    [[ -z $value || $value == "$bundle_version" ]] || fail 'The qui core must come from the Agent bundle release.'
    value=$bundle_version
  fi
  qui_valid_release_version "$value" || fail 'Provide --qui-version X.Y.Z or X.Y.Z-rc.N matching the Agent release.'
  printf '%s' "$value"
}
while (($#)); do
  case $1 in
    --panel|--node)
      [[ -z $mode ]] || fail 'Choose exactly one of --panel and --node.'
      mode=$1; shift;;
    --binary|--sha256|--config|--core|--core-version|--core-sha256|--core-url|--core-source|--qui-version|--core-bundle|--core-sums)
      (($# >= 2)) || fail "Missing value for $1"
      case $1 in
        --binary) binary=$2;; --sha256) digest=${2,,};; --config) config=$2;;
        --core)
          [[ $2 == xray || $2 == sing-box ]] || fail 'Core must be xray or sing-box.'
          [[ " ${cores[*]} " != *" $2 "* ]] || fail 'Duplicate --core.'
          core=$2; cores+=("$core");;
        --core-version) [[ -n $core ]] || fail 'Put --core before its options.'; core_versions[$core]=$2;;
        --core-sha256) [[ -n $core ]] || fail 'Put --core before its options.'; core_digests[$core]=${2,,};;
        --core-url) [[ -n $core ]] || fail 'Put --core before its options.'; core_urls[$core]=$2;;
        --core-source) core_source=$2;; --qui-version) qui_version=$2;;
        --core-bundle) core_bundle=$2;; --core-sums) core_sums=$2;;
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

# VERSION has the same grammar whether installing the panel or the Agent.
if [[ -f $root/../VERSION ]]; then
  qui_valid_release_version "$(cat -- "$root/../VERSION")" || fail 'Invalid bundle VERSION (expected X.Y.Z or X.Y.Z-rc.N).'
fi

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
  tuning_files=(proxy-agent-tuning.service proxy-agent-tuning.path)
  tuning_service=/etc/systemd/system/proxy-agent-tuning.service
  tuning_path=/etc/systemd/system/proxy-agent-tuning.path
  recovery_service=/etc/systemd/system/proxycontrol-update-recovery.service
else
  service_directory=$root/openrc
  unit=/etc/init.d/$name
  service_mode=0755
  host_network_files=(proxy-agent-host-network host-network-watch)
  host_network_service=/etc/init.d/proxy-agent-host-network
  host_network_path=/usr/local/lib/proxy-agent/host-network-watch
  tuning_files=(proxy-agent-tuning tuning-watch)
  tuning_service=/etc/init.d/proxy-agent-tuning
  tuning_path=/usr/local/lib/proxy-agent/tuning-watch
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
required_tools=(install sha256sum od stat getent useradd groupadd flock id tr sed chmod rm mktemp rmdir sync mv ln dirname find grep head mkdir awk cp)
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
  for packaged in "${host_network_files[@]}" "${tuning_files[@]}"; do
    [[ -f $service_directory/$packaged && ! -L $service_directory/$packaged ]] || fail "Missing packaged service file: $packaged"
  done
fi
host_architecture() {
  case $(uname -m) in x86_64) printf 'amd64';; aarch64|arm64) printf 'arm64';; *) fail 'Unsupported machine architecture.';; esac
}
host_libc() {
  local directory=${1:-/lib} loader
  for loader in "$directory"/ld-musl-*.so.1; do
    if [[ -e $loader ]]; then printf 'musl'; return; fi
  done
  printf 'glibc'
}
singbox_digest() {
  case $1:$2 in
    amd64:glibc) printf 'e2bdf179c15a3652955dc44867e33ca0965c9c9b91b34267dcc5a5d639a5feee';;
    arm64:glibc) printf '29dae24d76bea3bc62d07dd3e60cfc12003de7debbd6b768df9be9c68360174e';;
    amd64:musl) printf '41fc81fd041134d4d7d850a928d7d2cde17a7d6563953f406b22d32a68bfab1f';;
    arm64:musl) printf '12d6d67a4edf0dd54590865b1f4f79cd09ae043a02d648e9d1b5fccc8446ee07';;
    *) fail "不支持的架构与 libc 组合：$1:$2";;
  esac
}
select_core() {
  core=$1
  core_version=${core_versions[$core]:-}
  core_digest=${core_digests[$core]:-}
  core_url=${core_urls[$core]:-}
  if [[ $core == xray ]]; then core_version=${core_version:-26.3.27}
  elif [[ $core_source == qui ]]; then core_version=${core_version:-1.14.4}
  else
    core_version=${core_version:-1.14.3}
    if [[ -z $core_digest && $core_version == 1.14.3 ]]; then core_digest=$(singbox_digest "$(host_architecture)" "$(host_libc)"); fi
  fi
  core_target=/usr/local/lib/proxy-agent/$core
}
[[ $core_source == qui || $core_source == official ]] || fail 'Invalid core source.'
# Defaults apply only to first installs; an upgrade without --core carries the
# recorded set without downloading or changing core binaries.
if [[ $component == agent && ${#cores[@]} == 0 && ! -e $record ]]; then cores=(xray); fi
if [[ $component != agent && ${#cores[@]} != 0 ]]; then fail 'A core is only installed with --node.'; fi
for core in "${cores[@]}"; do
  select_core "$core"
  if [[ $core == sing-box && $core_source == qui ]]; then
    qui_version=$(qui_release_version)
    [[ -z $core_url && -z $core_digest ]] || fail 'The qui source requires release SHA256SUMS, not --core-url/--core-sha256.'
    [[ $core_version == 1.14.4 ]] || fail 'This installer pins qui sing-box 1.14.4-qui.1.'
    [[ -z $core_bundle && -z $core_sums || -f $core_bundle && ! -L $core_bundle && -f $core_sums && ! -L $core_sums ]] || fail 'Provide both local core files.'
  fi
  [[ $core_version =~ ^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$ ]] || fail 'Invalid core version.'
  if [[ -n $core_digest && ! $core_digest =~ ^[0-9a-f]{64}$ ]]; then fail 'Core archive SHA256 must be lowercase hexadecimal.'; fi
done
[[ -z $config || -f $config && ! -L $config ]] || fail 'Config template must be a regular local file.'

# Refuse symlinked system destinations rather than traversing them as root.
for path in /usr/local /usr/local/bin /usr/local/lib /usr/local/lib/proxy-agent /usr/local/lib/proxycontrol /var/lib /run/lock /etc/sysctl.d "$etc" "$data" "$data/host-network" "$data/tuning" "$registry"; do
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

record_cores=(); record_digests=()
if [[ -e $record || -L $record ]]; then
  [[ -f $record && ! -L $record && $(stat -c '%u:%a:%h' "$record") == 0:600:1 ]] || fail 'Unsafe installation record.'
  qui_record_cores "$record" "$component" || fail 'Invalid installation record.'
fi
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

# The official sing-box release ships a glibc build and a musl build per
# architecture, and only the second one starts on Alpine: the plain archive is
# linked against the glibc loader, so the kernel answers "not found" for a binary
# that is present. The loader is looked up in the directory given as the first
# argument (default /lib), so the regression test
# (deploy/linux/tests/core-asset-test.sh) can hand it a directory of its own.
# The Xray release is one static binary per architecture, whatever the libc.
# sing-box depends on it, so its asset carries the -musl suffix on a musl host;
# the caller may name the libc as the second argument, which the test does, and
# the installer leaves it to host_libc.
core_asset() {
  local architecture libc
  architecture=$(host_architecture)
  libc=${2:-$(host_libc)}
  if [[ $1 == sing-box && ${core_source:-qui} == qui ]]; then
    printf 'sing-box-%s-qui.1-linux-%s.tar.gz' "$core_version" "$architecture"
    return
  fi
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
  if [[ $1 == sing-box && ${core_source:-qui} == qui ]]; then
    printf 'https://github.com/jingniao/qui/releases/download/'
    return
  fi
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
  if [[ $core == sing-box && $core_source == qui ]]; then
    digest_file=$stage/SHA256SUMS
    if [[ -n $core_sums ]]; then cp -- "$core_sums" "$digest_file"
    else download "$(core_prefix "$core")v$qui_version/SHA256SUMS" "$digest_file"; fi
    value=$(awk -v asset="$asset" '$2 == asset && NF == 2 {n++; digest=$1} END {if(n==1) print digest}' "$digest_file")
    [[ $value =~ ^[0-9a-f]{64}$ ]] || fail 'Missing or ambiguous qui core SHA256SUMS entry.'
    printf '%s' "$value"
    return
  fi
  if [[ -n $core_digest ]]; then printf '%s' "$core_digest"; return; fi
  [[ $core == xray ]] || fail 'Pass --core-sha256: the sing-box release publishes no digest file.'
  digest_file=$stage/asset.dgst
  download "$(core_prefix "$core")v$core_version/$asset.dgst" "$digest_file"
  value=$(grep -i -E '(sha2[-_]?256|sha256)[[:space:]]*=' "$digest_file" | grep -o -E '[0-9a-fA-F]{64}' | head -n 1)
  value=${value,,}
  [[ $value =~ ^[0-9a-f]{64}$ ]] || fail 'The Xray release digest file carries no SHA256 value.'
  printf '%s' "$value"
}

verify_core_archive() {
  local file=$1 expected=$2 actual
  actual=$(sha256sum -- "$file"); actual=${actual%% *}
  [[ $actual == "$expected" ]] || fail 'Core archive SHA256 mismatch.'
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
  rm -f -- "$stage/program" "$stage/unit" "$stage/qui" "$stage/qui-update" "$stage/config" "$stage/config.new" "$stage/core" "$stage/core-xray" "$stage/core-sing-box" "$stage/SHA256SUMS" "$stage/archive" "$stage/asset.dgst" "$stage/installed-manifest" "$stage/host-network-service" "$stage/host-network-path" "$stage/tuning-service" "$stage/tuning-path" "$stage/service-library" "$stage/update-recovery" "$stage/update-recovery-service" "$stage/machine-id"
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
tuning_service_digest=''
tuning_path_digest=''
if [[ $component == agent ]]; then
  install -o root -g root -m 0600 -- "$service_directory/${tuning_files[0]}" "$stage/tuning-service"
  install -o root -g root -m 0600 -- "$service_directory/${tuning_files[1]}" "$stage/tuning-path"
  tuning_service_digest=$(sha256sum -- "$stage/tuning-service"); tuning_service_digest=${tuning_service_digest%% *}
  tuning_path_digest=$(sha256sum -- "$stage/tuning-path"); tuning_path_digest=${tuning_path_digest%% *}
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

# A first install uses the packaged configuration template. An upgrade retains
# existing settings, normalizing only the Agent core settings below.
config_source=''
if [[ ! -e $etc/config.yaml ]]; then
  config_source=$config
  [[ -n $config_source ]] || config_source=$root/examples/$component.yaml
  [[ -f $config_source && ! -L $config_source ]] || fail 'Config template must be a regular local file.'
  install -o root -g root -m 0600 -- "$config_source" "$stage/config"
fi

core_binary_digest=''
for core in "${cores[@]}"; do
  select_core "$core"
  asset=$(core_asset "$core")
  url=$core_url
  release_version=$core_version
  if [[ $core == sing-box && $core_source == qui ]]; then release_version=$qui_version; fi
  if [[ -z $url ]]; then url=$(core_prefix "$core")v$release_version/$asset; fi
  prefix=$(core_prefix "$core")
  remainder=${url#"$prefix"}
  [[ $url == "$prefix"* && $remainder == */"$asset" && ${remainder%"/$asset"} != */* && -n ${remainder%"/$asset"} ]] || fail "Core URL must be an official $core release asset."
  expected=$(core_archive_digest "$asset")
  if [[ -n $core_bundle && $core == sing-box && $core_source == qui ]]; then cp -- "$core_bundle" "$stage/archive"
  else download "$url" "$stage/archive"; fi
  verify_core_archive "$stage/archive" "$expected"
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
  mv -- "$stage/core" "$stage/core-$core"
  if [[ " ${record_cores[*]} " == *" $core "* ]]; then
    for i in "${!record_cores[@]}"; do
      if [[ ${record_cores[i]} == "$core" ]]; then record_digests[i]=$core_binary_digest; fi
    done
  else record_cores+=("$core"); record_digests+=("$core_binary_digest"); fi
  rm -rf -- "$stage/extract"
done

# An upgrade without core selection preserves existing configuration verbatim.
# When installing cores, preserve other settings. The scalar pair keeps old Agents
# usable during a rollback; cores is authoritative for the multi-core Agent.
if [[ $component == agent && ${#record_cores[@]} -gt 0 && ( ${#cores[@]} -gt 0 || -n $config_source ) ]]; then
  if [[ -z $config_source ]]; then
    [[ -f $etc/config.yaml && ! -L $etc/config.yaml ]] || fail 'Unsafe Agent configuration.'
    config_source=$etc/config.yaml
    install -o root -g root -m 0600 -- "$config_source" "$stage/config"
  fi
  qui_agent_core_config "$stage/config" "$stage/config.new"
  mv -- "$stage/config.new" "$stage/config"
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
  install -d -o "$account" -g "$account" -m 0700 "$data/tuning"
  install -d -o root -g root -m 0755 /etc/sysctl.d
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
for core in "${cores[@]}"; do
  install -d -o root -g root -m 0755 /usr/local/lib/proxy-agent
  publish "$stage/core-$core" "/usr/local/lib/proxy-agent/$core" root root 0755
done
service_reload_definitions
# Tuning uses the same publish/rollback bookkeeping as the enforced-source
# helper. Stage verified package files before publishing; record on lines 11/12.
if [[ $component == agent ]]; then
  publish "$stage/tuning-service" "$tuning_service" root root "$service_mode"
  publish "$stage/tuning-path" "$tuning_path" root root "$service_mode"
  service_reload_definitions
  if [[ $init_system == systemd ]]; then
    systemctl enable proxy-agent-tuning.path proxy-agent-tuning.service
    systemctl start proxy-agent-tuning.path
    if systemctl is-failed --quiet proxy-agent-tuning.service; then
      systemctl reset-failed proxy-agent-tuning.service
      systemctl start --no-block proxy-agent-tuning.service
    fi
  else
    service_enable proxy-agent-tuning
    if service_is_active proxy-agent-tuning; then service_restart proxy-agent-tuning; else service_start proxy-agent-tuning; fi
  fi
fi
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

qui_write_record "$stage/installed-manifest"
chmod 0600 "$stage/installed-manifest"
publish "$stage/installed-manifest" "$record" root root 0600
if $reinstall; then rm -- "$removed"; sync -f -- "$registry"; fi
verify_binary "$target" "$digest"
publishing_started=false

printf '%s installed at %s with unit %s.\n' "$name" "$target" "$unit"
for core in "${cores[@]}"; do printf 'Core %s installed at /usr/local/lib/proxy-agent/%s.\n' "$core" "$core"; done
printf 'Identity and data were retained. Review the Agent core settings before starting.\n'
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
# Enrollment is the only thing that writes the identity certificate, so a node
# that already has one is not told to enroll again; node-install.sh treats the
# same file (and a replaced symlink) as enrolled.
if [[ $component == agent && ! -e $data/identity/client.crt && ! -L $data/identity/client.crt ]]; then
  printf 'Then enroll the node with deploy/linux/enroll.sh --token-file FILE.\n'
fi
printf 'Managed agent and core upgrades are deployed from the panel.\n'
printf 'An interrupted qui-update is finished or rolled back at boot by proxycontrol-update-recovery.\n'
printf 'Run qui-update to update from GitHub.\n'
