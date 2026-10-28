#!/usr/bin/env bash
# Build unmodified upstream in an isolated checkout; never use the panel module.
set -euo pipefail
version=1.14.4
tag=v$version
commit=c005f35300ce76427dc285481ec5695f5b179cd2
toolchain=go1.26.10
revision=1
for tool in git go tar gzip sha256sum readelf; do
  command -v "$tool" >/dev/null || { printf 'Required build tool unavailable: %s\n' "$tool" >&2; exit 1; }
done
output=${OUTPUT_DIRECTORY:?Set OUTPUT_DIRECTORY to an absolute output directory}
[[ $output == /* ]] || { echo 'OUTPUT_DIRECTORY must be absolute.' >&2; exit 1; }
mkdir -p -- "$output"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
git clone --quiet --depth 1 --branch "$tag" https://github.com/SagerNet/sing-box.git "$work/source"
cd -- "$work/source"
[[ $(git rev-parse HEAD) == "$commit" && -z $(git status --porcelain) ]]
export GOTOOLCHAIN=$toolchain CGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 GOFLAGS= GOWORK=off
[[ $(go version) == "go version $toolchain "* ]]
tags=$(tr -d '\r\n' < release/DEFAULT_BUILD_TAGS_OTHERS),with_v2ray_api
ldflags="-X 'github.com/sagernet/sing-box/constant.Version=$version' $(cat release/LDFLAGS) -s -w -buildid="
epoch=$(git log -1 --format=%ct)
for arch in amd64 arm64; do
  name=sing-box-$version-qui.$revision-linux-$arch
  mkdir -p -- "$work/$name"
  binary=$work/$name/sing-box
  GOOS=linux GOARCH=$arch go build -p 1 -buildvcs=true -trimpath -tags "$tags" -ldflags "$ldflags" -o "$binary" ./cmd/sing-box
  [[ -z $(git status --porcelain) ]]
  go version -m "$binary" | grep -q 'vcs.modified=false'
  # Inspect ELF metadata without executing a cross-compiled binary.
  readelf -l "$binary" > "$work/program-headers"
  readelf -d "$binary" > "$work/dynamic-section"
  if grep -q INTERP "$work/program-headers" || grep -q NEEDED "$work/dynamic-section"; then
    echo 'Expected a static ELF without PT_INTERP or DT_NEEDED.' >&2
    exit 1
  fi
  cp LICENSE "$work/$name/LICENSE"
  digest=$(sha256sum "$binary"); digest=${digest%% *}
  printf 'tag=%s\ncommit=%s\ngo=%s\nCGO_ENABLED=0\nGOOS=linux\nGOARCH=%s\nGOAMD64=v1\nGOARM64=v8.0\ntags=%s\nflags=-p 1 -buildvcs=true -trimpath\nldflags=%s\nbinary_sha256=%s\n' \
    "$tag" "$commit" "$toolchain" "$arch" "$tags" "$ldflags" "$digest" > "$output/$name.provenance.txt"
  tar -c -C "$work" --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner "$name" | gzip -n > "$output/$name.tar.gz"
done
