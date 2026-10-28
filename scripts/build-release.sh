#!/usr/bin/env bash
# Build the Linux release archives: one tar.gz per architecture with the panel
# and agent binaries (the production web build is embedded in the panel), the
# deploy scripts, the systemd units, the OpenRC init scripts and the example
# configurations, plus a SHA256SUMS file next to them. A node archive carries both
# init-system definitions, because the same archive installs on Debian, Ubuntu and
# Alpine; deploy/linux/install.sh and node-install.sh pick the right one on the
# target host.
#
# Environment:
#   VERSION=1.0.0            version recorded in both binaries
#   OUTPUT_DIRECTORY=DIR     defaults to <repository>/dist
#   SKIP_NPM_CI=1            skip `npm ci` when the locked web dependencies are
#                            already installed (offline builds)
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
cd -- "$root"
version=${VERSION:-1.0.0}
output=${OUTPUT_DIRECTORY:-$root/dist}
architectures=(amd64 arm64)

. "$root/deploy/linux/service.sh"
qui_valid_release_version "$version" || { printf 'Invalid version: %s\n' "$version" >&2; exit 1; }
[[ $output == /* ]] || { printf 'OUTPUT_DIRECTORY must be an absolute path.\n' >&2; exit 1; }
command -v go >/dev/null || { printf 'go is not on PATH.\n' >&2; exit 1; }
command -v npm >/dev/null || { printf 'npm is not on PATH.\n' >&2; exit 1; }
command -v tar >/dev/null || { printf 'tar is not on PATH.\n' >&2; exit 1; }

if [[ ${SKIP_NPM_CI:-} == 1 ]]; then
  printf 'Skipping npm ci (SKIP_NPM_CI=1); using the installed web dependencies.\n'
else
  npm --prefix web ci
fi
npm --prefix web run build
[[ -f web/dist/index.html ]] || { printf 'The web build produced no web/dist/index.html.\n' >&2; exit 1; }

install -d -m 0755 -- "$output"
# Archives are byte-reproducible: members are sorted, every timestamp is the
# source revision's commit time (SOURCE_DATE_EPOCH wins when set) and every
# member is owned by root, while gzip -n keeps no timestamp of its own.
epoch=${SOURCE_DATE_EPOCH:-}
if [[ -z $epoch ]]; then
  epoch=$(git -C "$root" log -1 --format=%ct 2>/dev/null || true)
fi
[[ $epoch =~ ^[0-9]+$ ]] || { printf 'Cannot determine a reproducible archive timestamp.\n' >&2; exit 1; }
printf 'Archiving with mtime @%s.\n' "$epoch"
for architecture in "${architectures[@]}"; do
  bundle=proxy-control-$version-linux-$architecture
  stage=$output/$bundle
  if [[ -e $stage ]]; then rm -rf -- "$stage"; fi
  install -d -m 0755 -- "$stage/bin" "$stage/deploy"
  printf '%s\n' "$version" > "$stage/VERSION"
  for component in panel agent; do
    CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" go build -p 1 -trimpath \
      -ldflags "-s -w -X main.version=$version" \
      -o "$stage/bin/proxy-$component" "./cmd/$component"
  done
  for directory in linux systemd openrc examples; do
    cp -R -- "deploy/$directory" "$stage/deploy/$directory"
  done
  tar --create --file - -C "$output" \
    --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
    -- "$bundle" | gzip -n > "$output/$bundle.tar.gz"
  rm -rf -- "$stage"
  printf 'Built %s\n' "$output/$bundle.tar.gz"
done

OUTPUT_DIRECTORY="$output" bash "$root/scripts/build-singbox.sh"
cd -- "$output"
: > SHA256SUMS
for archive in proxy-control-*.tar.gz sing-box-*-qui.*-linux-*.tar.gz sing-box-*-qui.*-linux-*.provenance.txt; do
  digest=$(sha256sum -- "$archive"); digest=${digest%% *}
  printf '%s  %s\n' "$digest" "$archive" >> SHA256SUMS
done
printf 'Wrote %s/SHA256SUMS\n' "$output"
cat -- SHA256SUMS
