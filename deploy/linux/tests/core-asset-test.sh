#!/usr/bin/env bash
# Regression test for the core release archive the installers choose per host:
# deploy/linux/install.sh (core_asset) and deploy/linux/node-install.sh (the
# pinned sing-box digests).
#
# The defect it pins: both asked for sing-box-<版本>-linux-<架构>.tar.gz on every
# host. That archive is linked against the glibc loader, so on Alpine (musl) the
# kernel answers "not found" for the core that had just been installed, and the
# official -musl archive, which has its own digest, was never fetched.
#
# The helpers are extracted from the two scripts with sed, so the test reads the
# release under test instead of a copy of it: host_libc (both scripts), and
# core_asset plus singbox_digest. uname is stubbed and the musl loader lookup
# points at a temporary directory, so the test needs no root, no Linux kernel and
# no network:
#
#   bash deploy/linux/tests/core-asset-test.sh
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
install_sh=$script_dir/../install.sh
node_install_sh=$script_dir/../node-install.sh

passes=0
failed=0

check() {
  local name=$1 actual=$2 expected=$3
  if [[ $actual == "$expected" ]]; then
    printf 'ok - %s\n' "$name"
    passes=$((passes + 1))
  else
    printf 'not ok - %s (got %s, want %s)\n' "$name" "$actual" "$expected"
    failed=$((failed + 1))
  fi
}

# Print one top-level function. Every function in these scripts ends with a "}"
# in the first column, and no block inside one does.
extract() {
  local script=$1 name=$2 body
  body=$(sed -n "/^$name() {\$/,/^}\$/p" "$script")
  if [[ -z $body ]]; then
    printf 'core-asset-test: no %s() in %s\n' "$name" "$script" >&2
    exit 1
  fi
  printf '%s\n' "$body"
}

# The extracted helpers refuse what they cannot name; no case under test gets
# there.
fail() { printf 'core-asset-test: fail() reached: %s\n' "$*" >&2; return 1; }

T=$(mktemp -d) || exit 1
trap 'rm -rf -- "$T"' EXIT
mkdir -p -- "$T/musl" "$T/glibc"
# What Alpine has and what Debian has, so the lookup is exercised both ways.
: > "$T/musl/ld-musl-x86_64.so.1"
: > "$T/glibc/ld-linux-x86-64.so.2"

eval "$(extract "$install_sh" host_libc)"
eval "$(extract "$install_sh" host_architecture)"
eval "$(extract "$install_sh" core_asset)"
# The second copy, renamed, so both detections can be compared in one shell.
node_libc_source=$(extract "$node_install_sh" host_libc)
eval "${node_libc_source/host_libc/host_libc_node}"
eval "$(extract "$node_install_sh" singbox_digest)"

check 'install.sh: a musl loader is musl' "$(host_libc "$T/musl")" musl
check 'install.sh: no musl loader is glibc' "$(host_libc "$T/glibc")" glibc
check 'node-install.sh: a musl loader is musl' "$(host_libc_node "$T/musl")" musl
check 'node-install.sh: no musl loader is glibc' "$(host_libc_node "$T/glibc")" glibc

# host_architecture reads uname; the libc is named explicitly, so the asset names
# below do not depend on the host this runs on.
machine=x86_64
uname() { printf '%s\n' "$machine"; }
core_version=1.14.3
version=$core_version

check 'sing-box keeps its glibc archive on amd64' "$(core_asset sing-box glibc)" "sing-box-$version-linux-amd64.tar.gz"
check 'sing-box takes the -musl archive on amd64' "$(core_asset sing-box musl)" "sing-box-$version-linux-amd64-musl.tar.gz"
check 'xray keeps its archive on amd64' "$(core_asset xray glibc)" 'Xray-linux-64.zip'
check 'xray keeps its archive on musl amd64' "$(core_asset xray musl)" 'Xray-linux-64.zip'

machine=aarch64
check 'sing-box keeps its glibc archive on arm64' "$(core_asset sing-box glibc)" "sing-box-$version-linux-arm64.tar.gz"
check 'sing-box takes the -musl archive on arm64' "$(core_asset sing-box musl)" "sing-box-$version-linux-arm64-musl.tar.gz"
check 'xray keeps its archive on arm64' "$(core_asset xray glibc)" 'Xray-linux-arm64-v8a.zip'
check 'xray keeps its archive on musl arm64' "$(core_asset xray musl)" 'Xray-linux-arm64-v8a.zip'

# node-install.sh hands install.sh the digest of exactly the archive install.sh
# names for this host: one pinned digest per architecture and libc.
check 'node-install.sh pins the glibc amd64 digest' "$(singbox_digest amd64 glibc)" e2bdf179c15a3652955dc44867e33ca0965c9c9b91b34267dcc5a5d639a5feee
check 'node-install.sh pins the glibc arm64 digest' "$(singbox_digest arm64 glibc)" 29dae24d76bea3bc62d07dd3e60cfc12003de7debbd6b768df9be9c68360174e
check 'node-install.sh pins the musl amd64 digest' "$(singbox_digest amd64 musl)" 41fc81fd041134d4d7d850a928d7d2cde17a7d6563953f406b22d32a68bfab1f
check 'node-install.sh pins the musl arm64 digest' "$(singbox_digest arm64 musl)" 12d6d67a4edf0dd54590865b1f4f79cd09ae043a02d648e9d1b5fccc8446ee07
mapfile -t pinned < <(printf '%s\n' "$(singbox_digest amd64 glibc)" "$(singbox_digest arm64 glibc)" "$(singbox_digest amd64 musl)" "$(singbox_digest arm64 musl)" | sort -u)
check 'the four pinned archives have four distinct digests' "${#pinned[@]}" 4

printf '\ncore-asset-test: %s/%s passed\n' "$passes" "$((passes + failed))"
if ((failed > 0)); then exit 1; fi
exit 0
