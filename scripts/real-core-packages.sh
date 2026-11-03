#!/usr/bin/env bash
# Print the packages that hold real-core tests, one `./path` per line, so a
# real-core run can name them instead of the whole tree.
#
# A real-core test reaches its binary only through the environment: the tests
# themselves read os.Getenv("XRAY_BINARY"), "SINGBOX_BINARY", "MIHOMO_BINARY"
# (+ "MIHOMO_HOME" for the pinned geodata) or "GOST_BINARY", and no production
# file reads those names, so a package that never names one of them cannot start
# a real core and cannot be skipping for a missing core. The selection is
# therefore derived from the sources rather than maintained by hand: a new
# real-core test in a new package is picked up as soon as it names its variable.
#
# Usage:
#   scripts/real-core-packages.sh                      # the package list
#   bash scripts/ci-real-core-test.sh -p 4 -count=1 -timeout 60m \
#     $(scripts/real-core-packages.sh)
#
# It prints nothing and fails when no package qualifies, because an empty list
# would silently turn the real-core step into a no-op.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"

# Every variable the real-core tests use to find a core binary or its data.
# scripts/ci-real-core-test.sh fails the step when one of these skips, so the two
# files must stay in agreement.
pattern='XRAY_BINARY|SINGBOX_BINARY|MIHOMO_BINARY|MIHOMO_HOME|GOST_BINARY'

count=0
while read -r directory; do
  files=("$directory"/*_test.go)
  [[ -e ${files[0]} ]] || continue
  if grep -qE -- "$pattern" "${files[@]}"; then
    printf './%s\n' "${directory#"$root"/}"
    count=$((count + 1))
  fi
done < <(go list -f '{{.Dir}}' ./...)

if ((count == 0)); then
  printf 'No package references a core binary variable; refusing to run an empty real-core suite.\n' >&2
  exit 1
fi
