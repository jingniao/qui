#!/usr/bin/env bash
# Rootless version grammar/precedence regression; also runs in Windows Git Bash.
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
repo=$(cd -- "$script_dir/../../.." && pwd -P)
. "$repo/deploy/linux/service.sh"
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
# node-install runs standalone before downloading service.sh. Exercise its actual
# validator against the same table, rather than copying a second regex here.
node_validator=$(sed -n '/^\[\[ \$version =~ /p' "$repo/deploy/linux/node-install.sh")
node_valid() (
  version=$1
  fail() { return 1; }
  eval "$node_validator" 2>/dev/null
)
for version in 0.0.0 1.0.9 1.1.0-rc.1 1.1.0-rc.2 1.1.0-rc.10 1.1.0 999999999999999999999.0.0-rc.999999999999999999999; do
  check "valid $version" 0 qui_valid_release_version "$version"
  check "standalone valid $version" 0 node_valid "$version"
done
for version in '' dev v1.1.0 1.1 1.1.0.1 01.1.0 1.01.0 1.1.00 1.1.0-rc 1.1.0-rc.0 1.1.0-rc.01 1.1.0-rc.-1 1.1.0-rc.+1 1.1.0-RC.1 1.1.0-beta.1 1.1.0-rc.1.1 1.1.0+build 1.1.0-rc.1+build '1.1.0 ' $'1.1.0\n'; do
  check "invalid [$version]" 1 qui_valid_release_version "$version"
  check "standalone invalid [$version]" 1 node_valid "$version"
done
# All pairwise comparisons test equality and both directions, including RC 2/10,
# a stable release versus its RCs and numeric major/minor/patch boundaries.
ordered=(1.0.9 1.1.0-rc.1 1.1.0-rc.2 1.1.0-rc.10 1.1.0 1.1.1-rc.1 1.1.1 1.2.0 1.10.0 2.0.0 999999999999999999998.0.0 999999999999999999999.0.0-rc.999999999999999999998 999999999999999999999.0.0-rc.999999999999999999999)
for i in "${!ordered[@]}"; do
  for j in "${!ordered[@]}"; do
    expected=1
    if ((i < j)); then expected=0; fi
    check "${ordered[i]} < ${ordered[j]}" "$expected" qui_release_is_older "${ordered[i]}" "${ordered[j]}"
  done
done
check 'unknown installed version unordered' 1 qui_release_is_older 1.1.0 dev
check 'invalid target unordered' 1 qui_release_is_older 1.1.0-rc.01 1.1.0
# Test installer VERSION/--qui-version binding without privileged installation.
eval "$(sed -n '/^qui_release_version() {/,/^}/p' "$repo/deploy/linux/install.sh")"
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
mkdir -p -- "$temporary/deploy"
root=$temporary/deploy
fail() { return 1; }
qui_version=1.1.0-rc.1
printf '%s\n' "$qui_version" > "$temporary/VERSION"
installer_matches() { [[ $(qui_release_version) == 1.1.0-rc.1 ]]; }
check 'installer RC VERSION matches' 0 installer_matches
# The real fail exits; run a subshell so a mismatched VERSION cannot continue.
installer_mismatch() (
  fail() { exit 1; }
  qui_version=1.1.0-rc.2
  qui_release_version
)
check 'installer VERSION mismatch refused' 1 installer_mismatch
installer_invalid() (
  fail() { exit 1; }
  qui_version=''
  printf '1.1.0-rc.01\n' > "$temporary/VERSION"
  qui_release_version
)
check 'installer invalid bundle VERSION refused' 1 installer_invalid
# Exercise the updater's actual download/offline digest path with fixture bytes,
# stopping before tar or installation. The curl stub performs no network access.
eval "$(sed -n '/^sums_digest() {/,/^}/p' "$repo/deploy/linux/update.sh")"
eval "$(sed -n '/^download() {/,/^}/p' "$repo/deploy/linux/update.sh")"
fetch_code=$(sed -n '/^  if \[\[ -n \$bundle_file \]\]; then/,/^  source_dir=/p' "$repo/deploy/linux/update.sh")
fetch_fixture() (
  fail() { exit 1; }
  local mode=$1 version=1.1.0-rc.2 arch=amd64 repository=jingniao/qui
  local bundle base directory=$temporary/fetch bundle_file='' sums_file=''
  local expected actual source_dir
  # Read the actual asset/tag assignments instead of maintaining a test copy.
  eval "$(sed -n '/^  bundle=proxy-control-/p; /^  base=https:/p' "$repo/deploy/linux/update.sh")"
  mkdir -p -- "$directory"
  printf 'test fixture bytes\n' > "$temporary/$bundle.tar.gz"
  actual=$(sha256sum -- "$temporary/$bundle.tar.gz")
  printf '%s  %s\n' "${actual%% *}" "$bundle.tar.gz" > "$temporary/SHA256SUMS"
  curl() {
    local destination='' url=''
    while (($#)); do
      case $1 in --output) destination=$2; shift 2;; --) url=$2; break;; *) shift;; esac
    done
    printf '%s\n' "$url" >> "$temporary/urls"
    case $url in
      https://github.com/jingniao/qui/releases/download/v1.1.0-rc.2/proxy-control-1.1.0-rc.2-linux-amd64.tar.gz)
        cp -- "$temporary/$bundle.tar.gz" "$destination";;
      https://github.com/jingniao/qui/releases/download/v1.1.0-rc.2/SHA256SUMS)
        cp -- "$temporary/SHA256SUMS" "$destination";;
      *) return 1;;
    esac
  }
  if [[ $mode != online ]]; then
    bundle_file=$temporary/$bundle.tar.gz
    sums_file=$temporary/SHA256SUMS
  fi
  if [[ $mode == tampered ]]; then printf 'tampered\n' >> "$bundle_file"; fi
  eval "$fetch_code"
)
check 'RC online tag and asset URLs, digest verified (stub curl)' 0 fetch_fixture online
check 'RC offline digest verified' 0 fetch_fixture offline
check 'RC tampered offline bytes refused' 1 fetch_fixture tampered
printf '\nrelease-version-test: %s passed, %s failed\n' "$passes" "$failed"
((failed == 0))
