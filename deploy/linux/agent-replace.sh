#!/usr/bin/env bash
# Sourced by install.sh under the installation lock after staging every core.

verify_replacement_signature() {
  local manifest=$1 program=$2 version=$3 arch=$4 scratch=$5
  local digest size
  digest=$(sha256sum -- "$program"); digest=${digest%% *}
  size=$(stat -c '%s' -- "$program")
  # Ed25519 SubjectPublicKeyInfo for release.OfficialKeyPins[0].
  printf '%s' MCowBQYDK2VwAyEAv1GzEWqRzysqZRFBryZStMCw4KeAkF3Vq2metTUlxTE= |
    openssl base64 -d -A > "$scratch/release.der" || return 1
  jq -e --arg version "$version" --arg arch "$arch" --arg digest "$digest" --argjson size "$size" '
    .manifest | .schema_version == 1 and .component == "proxy-agent" and
    .version == $version and .os == "linux" and .architecture == $arch and
    .sha256 == $digest and .bytes == $size and .sequence >= 1 and
    ((.issued_at | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601) <= now + 300) and
    ((.expires_at | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601) > now)
  ' "$manifest" >/dev/null || return 1
  printf 'proxy-control executable release v1\n' > "$scratch/signed"
  jq -cj '.manifest | {schema_version,component,version,os,architecture,sequence,bytes,sha256,issued_at,expires_at}' "$manifest" >> "$scratch/signed" || return 1
  jq -rj '.signature' "$manifest" | openssl base64 -d -A > "$scratch/release.sig" || return 1
  openssl pkeyutl -verify -pubin -keyform DER -inkey "$scratch/release.der" -rawin -in "$scratch/signed" -sigfile "$scratch/release.sig"
}

replacement_paths() {
  local path config=/etc/proxy-agent/config.yaml directory=/var/lib/proxy-agent
  for path in "$config" /etc/proxy-agent/tls/ca.crt /etc/proxy-agent/certs "$directory" "$directory/identity" "$directory/spool"; do
    [[ $(realpath -e -- "$path") == "$path" ]] || fail "重装拒绝符号链接或缺少路径：$path"
  done
  [[ $(sed -n 's/^data_directory:[[:space:]]*//p' "$config") == "$directory" ]] || fail '重装只支持固定的 Agent 数据目录 /var/lib/proxy-agent。'
  # Reject bind mounts too, including mounts on the same device that -xdev
  # alone cannot detect. The literal path has no mountinfo escaping.
  local mounted
  mounted=$(awk '$5 == "/var/lib/proxy-agent" || index($5,"/var/lib/proxy-agent/")==1 {print $5; exit}' /proc/self/mountinfo) || fail '无法读取文件系统挂载点。'
  [[ -z $mounted ]] || fail 'Agent 数据目录内含有挂载点，不能清除。'
  local link
  link=$(find "$directory" -xdev -type l -print -quit) || fail '无法检查 Agent 数据目录。'
  [[ -z $link ]] || fail "Agent 数据目录内含有符号链接：$link"
}

stop_replacement_agent() {
  local pid tracer i ready=false
  pid=$(service_main_pid proxy-agent) || fail '需要运行中的旧 Agent，才能确认本次停止的最终采样。'
  if [[ $init_system == systemd ]]; then
    systemctl stop proxy-agent.service || fail '旧 Agent 停止失败。'
    [[ $(systemctl show --value -p ExecMainPID proxy-agent.service) == "$pid" &&
       $(systemctl show --value -p ExecMainCode proxy-agent.service) == 1 &&
       $(systemctl show --value -p ExecMainStatus proxy-agent.service) == 0 &&
       $(systemctl show --value -p Result proxy-agent.service) == success ]] || fail '旧 Agent 未确认正常退出，保留本机数据。'
  else
    # OpenRC returns the stop request's result, not the Agent exit status.
    # Observe only the exit (no syscalls, signals, arguments or payload).
    command -v strace >/dev/null || fail 'OpenRC 重装需预先安装 strace 以确认服务退出码；旧服务仍在运行。'
    strace -e trace=none -e signal=none -o "$replacement_work/exit" -p "$pid" &
    tracer=$!
    for ((i=0;i<100;i++)); do
      if [[ $(awk '/^TracerPid:/{print $2}' "/proc/$pid/status") == "$tracer" ]]; then ready=true; break; fi
      kill -0 "$tracer" 2>/dev/null || break
      sleep .05
    done
    if ! $ready; then
      kill "$tracer" 2>/dev/null || true
      wait "$tracer" 2>/dev/null || true
      fail '无法取得旧 Agent 退出状态，OpenRC 服务未停止。'
    fi
    if ! rc-service proxy-agent stop; then
      kill "$tracer" 2>/dev/null || true
      wait "$tracer" 2>/dev/null || true
      fail 'OpenRC 停止旧 Agent 失败。'
    fi
    wait "$tracer" && grep -Fxq '+++ exited with 0 +++' "$replacement_work/exit" || fail '旧 Agent 未确认正常退出，保留本机数据。'
  fi
  if service_is_active proxy-agent; then fail '旧 Agent 仍在运行。'; fi
}

replace_installed_agent() (
  set -euo pipefail
  local program=$1 token_file=$2 manifest=$3 version=$4 arch=$5
  local replacement_work config=/etc/proxy-agent/config.yaml data=/var/lib/proxy-agent
  replacement_paths
  [[ -f $data/identity/client.crt && -f $data/spool/queue.db ]] || fail '缺少旧 Agent 身份或队列。'
  replacement_work=$(mktemp -d /run/proxy-agent-replace.XXXXXXXX)
  trap 'rm -rf -- "$replacement_work"' EXIT
  verify_replacement_signature "$manifest" "$program" "$version" "$arch" "$replacement_work" || fail 'Agent 官方发布签名或二进制内容不匹配。'
  install -o root -g proxy-agent -m 0750 -- "$program" "$replacement_work/agent"
  install -o proxy-agent -g proxy-agent -m 0400 -- "$token_file" "$replacement_work/token"
  install -o root -g proxy-agent -m 0440 -- "${token_file%/*}/replacement-endpoint" "$replacement_work/replacement-endpoint"
  install -o root -g proxy-agent -m 0440 -- "${token_file%/*}/replacement-ca.crt" "$replacement_work/replacement-ca.crt"
  chown root:proxy-agent -- "$replacement_work"
  chmod 0750 -- "$replacement_work"
  runuser -u proxy-agent -- "$replacement_work/agent" --config "$config" --drain-telemetry --check-config || fail '原 Agent 配置与固定安装路径不匹配，服务仍在运行。'
  stop_replacement_agent
  replacement_paths
  # A partially successful upload advances the real delivery ledger. Keep the
  # pre-upload rows as well so a later failed ACK preserves all original data.
  local snapshot
  snapshot=$(mktemp -d "$data/replacement-original.XXXXXXXX")
  cp -a -- "$data/identity" "$data/spool" "$snapshot/" || fail '无法保留上传前的原始队列。'
  sync -f -- "$snapshot" || fail '上传前的原始队列未能写入磁盘。'
  local confirmed original pid reader writer
  # Keep both lifetime locks until the installer detaches the confirmed tree.
  coproc DRAIN { runuser -u proxy-agent -- "$replacement_work/agent" --config "$config" --drain-telemetry --token-file "$replacement_work/token"; }
  pid=$DRAIN_PID; reader=${DRAIN[0]}; writer=${DRAIN[1]}
  if ! IFS= read -r -t 320 confirmed <&"$reader" || [[ $confirmed != old-agent-drained ]]; then
    exec {writer}>&-
    wait "$pid" || true
    fail "最终上传未获面板确认，旧 Agent 数据和上传前队列 $snapshot 均保留。"
  fi
  if service_is_active proxy-agent; then fail '旧 Agent 已重新启动，保留本机数据。'; fi
  replacement_paths
  original=$(mktemp -d /var/lib/proxy-agent-replacing.XXXXXXXX)
  mv -T -- /var/lib/proxy-agent "$original/data" || fail '面板已确认，无法移出旧 Agent 目录。'
  printf 'detached\n' >&"$writer"
  exec {writer}>&-
  wait "$pid" || fail "旧 Agent 数据保留在 $original/data，释放持久状态失败。"
  [[ $original == /var/lib/proxy-agent-replacing.???????? && $(realpath -e -- "$original/data") == "$original/data" ]] || fail '旧 Agent 清理目录不安全。'
  rm -rf --one-file-system -- "$original/data" || fail "面板已确认，清理 $original/data 失败。"
  rmdir -- "$original"
  printf '面板已确认最终上传，旧 Agent 本机数据已清除。\n'
)
