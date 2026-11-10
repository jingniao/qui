#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
repo=$(cd -- "$script_dir/../../.." && pwd -P)

passes=0
failed=0
check() {
  local name=$1 expected=$2 status=0
  shift 2
  "$@" || status=$?
  if [[ $status == "$expected" ]]; then
    printf 'ok - %s\n' "$name"
    passes=$((passes + 1))
  else
    printf 'not ok - %s (status %s, want %s)\n' "$name" "$status" "$expected"
    failed=$((failed + 1))
  fi
}

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT

keyfile="$tmp/key"
pubfile="$tmp/pub"
go run "$repo/cmd/release" keygen --private-key "$keyfile" --public-key "$pubfile" >/dev/null

dist="$tmp/dist"
mkdir -p "$dist/proxy-control-9.8.7-linux-amd64/bin"
printf '#!/bin/sh\necho agent\n' > "$dist/proxy-control-9.8.7-linux-amd64/bin/proxy-agent"
chmod 0755 "$dist/proxy-control-9.8.7-linux-amd64/bin/proxy-agent"
: > "$dist/SHA256SUMS"

# 1. Skip on pre-release
skip_out=$(bash "$repo/scripts/sign-agent-release.sh" "1.5.0-rc.1" "$dist" "$keyfile")
check "skip on pre-release" 0 test "$skip_out" = "skip: not a formal release"

# 2. Formal release signs and produces files
bash "$repo/scripts/sign-agent-release.sh" "9.8.7" "$dist" "$keyfile"
bin_file="$dist/proxy-agent-9.8.7-linux-amd64.bin"
manifest_file="$dist/proxy-agent-9.8.7-linux-amd64.manifest.json"

check "generates .bin" 0 test -f "$bin_file"
check "generates .manifest.json" 0 test -f "$manifest_file"

# 3. Sequence in manifest
check "sequence in manifest is 9008007" 0 grep -q '"sequence": 9008007' "$manifest_file"

# 4. Verification passes
check "verify passes" 0 go run "$repo/cmd/release" verify --public-key "$pubfile" --manifest "$manifest_file" --artifact "$bin_file" --component proxy-agent --os linux --arch amd64

# 5. Checksum in SHA256SUMS
check "bin in SHA256SUMS" 0 grep -q "proxy-agent-9.8.7-linux-amd64.bin" "$dist/SHA256SUMS"
check "manifest in SHA256SUMS" 0 grep -q "proxy-agent-9.8.7-linux-amd64.manifest.json" "$dist/SHA256SUMS"

printf 'sign-agent-release-test: %d passed, %d failed\n' "$passes" "$failed"
if ((failed)); then exit 1; fi
