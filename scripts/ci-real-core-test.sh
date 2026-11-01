#!/usr/bin/env bash
# Run `go test -v` for a real-core CI step and fail when a test skipped because
# a proxy core, its data or a usable local address was missing. The cores job
# sets every core variable, so such a skip means the step did not test what it
# claims; any other skip is listed but allowed.
#
# Usage: scripts/ci-real-core-test.sh <go test arguments...>
set -uo pipefail
log=$(mktemp)
trap 'rm -f -- "$log"' EXIT
go test -v "$@" >"$log" 2>&1
status=$?
if ((status != 0)); then
  cat -- "$log"
  exit "$status"
fi
grep -E '^(ok|FAIL|\?)[[:space:]]' -- "$log"
# A -run pattern that matches nothing passes silently, e.g. after a rename.
if grep -q '\[no tests to run\]' -- "$log"; then
  printf 'A package ran no tests for this -run pattern.\n' >&2
  exit 1
fi
# A skip prints its reason on the line before "--- SKIP".
skips=$(awk '/^=== |--- (PASS|FAIL): / {reason = ""} /_test\.go:[0-9]+: / {reason = $0; next} /--- SKIP: / {print $0 " <-" reason}' "$log")
missing=$(printf '%s\n' "$skips" | grep -E 'XRAY_BINARY|SINGBOX_BINARY|MIHOMO_BINARY|MIHOMO_HOME|non-loopback|cannot listen' || true)
printf 'Passed real-core tests: %s\n' "$(grep -cE -- '--- PASS: TestReal' "$log")"
if [[ -n $skips ]]; then
  printf 'Skipped tests:\n%s\n' "$skips"
fi
if [[ -n $missing ]]; then
  printf 'Real-core tests skipped for a missing core, core data or local address:\n%s\n' "$missing" >&2
  exit 1
fi
