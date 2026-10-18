#!/usr/bin/env bash
# Regenerate the protobuf Go sources in this repository.
#
# protoc and the Go plugins are development tools that are not part of the
# repository. Install them first, or place a compiler in .tools/protoc (with the
# plugins in .tools/gopath/bin), then run this script.
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
cd -- "$root"
compiler=protoc
includes=()
if [[ -x .tools/protoc/bin/protoc ]]; then
  compiler=$root/.tools/protoc/bin/protoc
  includes=(-I "$root/.tools/protoc/include")
  export PATH="$root/.tools/gopath/bin:$PATH"
fi
command -v "$compiler" >/dev/null || { printf 'protoc is not available; install it and retry.\n' >&2; exit 1; }
for plugin in protoc-gen-go protoc-gen-go-grpc; do
  command -v "$plugin" >/dev/null || { printf '%s is not on PATH; install it and retry.\n' "$plugin" >&2; exit 1; }
done
"$compiler" -I proto ${includes[@]+"${includes[@]}"} \
  --go_out=. --go_opt=module=proxycontrol \
  --go-grpc_out=. --go-grpc_opt=module=proxycontrol \
  proto/agent.proto proto/xray_stats.proto
printf 'Protobuf sources regenerated.\n'
