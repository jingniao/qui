#!/usr/bin/env bash
# Sign the agent binary for formal releases (X.Y.Z) and emit signed manifest and raw .bin.
set -euo pipefail

version=${1:?usage: sign-agent-release.sh <version> <dist-dir> <private-key-file>}
dist_dir=${2:?usage: sign-agent-release.sh <version> <dist-dir> <private-key-file>}
private_key_file=${3:?usage: sign-agent-release.sh <version> <dist-dir> <private-key-file>}

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)

if [[ ! $version =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  printf 'skip: not a formal release\n'
  exit 0
fi

major=${BASH_REMATCH[1]}
minor=${BASH_REMATCH[2]}
patch=${BASH_REMATCH[3]}
sequence=$((10#$major * 1000000 + 10#$minor * 1000 + 10#$patch))

architectures=(amd64 arm64)

for arch in "${architectures[@]}"; do
  src_bin="$dist_dir/proxy-control-$version-linux-$arch/bin/proxy-agent"
  tar_file="$dist_dir/proxy-control-$version-linux-$arch.tar.gz"
  dest_bin="$dist_dir/proxy-agent-$version-linux-$arch.bin"
  manifest="$dist_dir/proxy-agent-$version-linux-$arch.manifest.json"

  if [[ -f "$src_bin" ]]; then
    cp -f -- "$src_bin" "$dest_bin"
  elif [[ -f "$tar_file" ]]; then
    tar -xzf "$tar_file" -C "$dist_dir" "proxy-control-$version-linux-$arch/bin/proxy-agent"
    cp -f -- "$src_bin" "$dest_bin"
    rm -rf -- "$dist_dir/proxy-control-$version-linux-$arch"
  else
    continue
  fi

  chmod 0755 "$dest_bin"

  go run "$root/cmd/release" sign \
    --private-key "$private_key_file" \
    --artifact "$dest_bin" \
    --component proxy-agent \
    --version "$version" \
    --os linux \
    --arch "$arch" \
    --sequence "$sequence" \
    --valid-for 8760h \
    --out "$manifest"

  if [[ -f "$dist_dir/SHA256SUMS" ]]; then
    for f in "proxy-agent-$version-linux-$arch.bin" "proxy-agent-$version-linux-$arch.manifest.json"; do
      grep -F "  $f" "$dist_dir/SHA256SUMS" >/dev/null 2>&1 || (cd -- "$dist_dir" && sha256sum -- "$f" >> SHA256SUMS)
    done
  fi
done
