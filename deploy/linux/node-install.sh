#!/usr/bin/env bash
# Install, enroll and start a node agent on a fresh Linux server with one command.
#
# The command the panel shows in the register dialog runs this script on the new
# server. It downloads the release archive whose version equals the panel
# version from the public GitHub repository and verifies it against the
# SHA256SUMS published with it, so trust in the archive rests on that repository
# and not on this script. The archive's own deploy/linux/install.sh then installs
# the agent and the pinned official core, and deploy/linux/enroll.sh registers
# the node. That install.sh upgrades in place, so this script may be re-run until
# the node is enrolled: a first run whose enrollment failed (expired or used
# token, unreachable gRPC port, wrong --panel) is retried with a fresh
# registration, and the panel's gRPC port (9443 by default) has to be reachable
# from this host for enrollment to succeed. Only a host that is already enrolled,
# or that still runs proxy-agent.service, is refused.
#
# Trust in the panel certificate is established in one of two ways: when the
# certificate at /install/ca.pem verifies against the system CA bundle the bundle
# itself becomes the node CA (a publicly trusted certificate, for example Let's
# Encrypt, whose intermediates rotate), otherwise the certificate fetched over
# HTTPS has to match --ca-sha256 as shown by the panel. The enrollment token is
# single use and expires; it is visible in the shell history of this host until
# that history is cleared.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

repository=jingniao/qui

fail() { printf '安装被拒绝：%s\n' "$*" >&2; exit 1; }
usage() {
  cat <<USAGE
用法：node-install.sh --version X.Y.Z --panel HOST --ca-sha256 HEX --token TOKEN [选项]

  --version X.Y.Z       面板发布版本，必须与面板版本一致
  --panel HOST          面板地址：域名或 IPv4
  --ca-sha256 HEX       面板证书信任锚的 SHA-256（64 位小写十六进制，面板显示）
  --token TOKEN         一次性节点注册码
  --http-port N         面板 HTTPS 端口（默认 443）
  --grpc-port N         面板 gRPC 端口（默认 9443）
  --core xray|sing-box  同时安装的官方核心（默认 xray）

脚本从 https://github.com/$repository/releases 下载与面板同版本的归档，用归档里
的 deploy/linux/install.sh 安装 Agent 与核心，按公网证书或固定 SHA-256 建立对面板
证书的信任，再用归档里的 deploy/linux/enroll.sh 登记并启动 Agent。注册码只能用
一次且会过期；节点登记成功前可以重跑本脚本（登记成功后会被拒绝），升级用 qui-update。
USAGE
}

version=''
panel=''
http_port=443
grpc_port=9443
ca_sha256=''
token=''
core=xray
while (($#)); do
  case $1 in
    --version|--panel|--http-port|--grpc-port|--ca-sha256|--token|--core)
      (($# >= 2)) || fail "缺少 $1 的值。"
      case $1 in
        --version) version=$2;; --panel) panel=$2;; --http-port) http_port=$2;;
        --grpc-port) grpc_port=$2;; --ca-sha256) ca_sha256=${2,,};; --token) token=$2;;
        --core) core=$2;;
      esac
      shift 2;;
    --help) usage; exit 0;;
    *) fail "未知参数：$1（用 --help 查看用法）";;
  esac
done

[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail '版本号必须是 X.Y.Z 形式。'
[[ $panel =~ ^[A-Za-z0-9.-]+$ ]] || fail '面板地址只能是域名或 IPv4（字母、数字、点、连字符，不含端口）。'
[[ $http_port =~ ^[0-9]{1,5}$ && $http_port -ge 1 && $http_port -le 65535 ]] || fail 'HTTPS 端口必须在 1-65535 之间。'
[[ $grpc_port =~ ^[0-9]{1,5}$ && $grpc_port -ge 1 && $grpc_port -le 65535 ]] || fail 'gRPC 端口必须在 1-65535 之间。'
[[ $ca_sha256 =~ ^[0-9a-f]{64}$ ]] || fail '面板证书 SHA-256 必须是 64 位小写十六进制。'
[[ $token =~ ^[A-Za-z0-9_-]{16,128}$ ]] || fail '注册码必须是 16-128 位 URL 安全字符。'
[[ $core == xray || $core == sing-box ]] || fail '核心只能是 xray 或 sing-box。'

[[ $(uname -s) == Linux && $EUID -eq 0 ]] || fail '请以 root 身份在 Linux 上运行。'
for tool in systemctl curl tar sha256sum install mktemp stat chown chmod sed rm uname; do
  command -v "$tool" >/dev/null || fail "缺少命令：$tool"
done

# Only a host that is already enrolled is refused: enrollment is the only thing
# that writes the identity certificate, so a host whose first run installed the
# Agent but could not enroll (expired or used token, unreachable gRPC port, wrong
# --panel) runs this script again, and the archive's install.sh upgrades it in
# place. Re-enrolling a node that is already enrolled is described in the
# operations manual. A host that still runs the Agent service is refused as well,
# because install.sh and enroll.sh both require the service to be stopped.
identity_certificate=/var/lib/proxy-agent/identity/client.crt
if [[ -e $identity_certificate || -L $identity_certificate ]]; then
  fail '本机已安装并登记 Agent；升级用 qui-update，重新登记见 docs/operations.md 第 5 节'
fi
if systemctl is-active --quiet proxy-agent.service; then
  fail '请先停止 proxy-agent.service（systemctl stop proxy-agent.service）再重跑本脚本。'
fi

case $(uname -m) in
  x86_64) architecture=amd64;;
  aarch64|arm64) architecture=arm64;;
  *) fail "不支持的架构：$(uname -m)";;
esac

# The core release is pinned here and install.sh verifies the archive before it
# installs anything. Xray publishes a .dgst file next to its asset and install.sh
# reads it itself; sing-box publishes no such file, so its asset digest is pinned
# per architecture (official GitHub asset digests of sing-box-1.14.3).
case $core in
  xray)
    core_version=26.3.27
    core_digest='';;
  sing-box)
    core_version=1.14.3
    case $architecture in
      amd64) core_digest=e2bdf179c15a3652955dc44867e33ca0965c9c9b91b34267dcc5a5d639a5feee;;
      arm64) core_digest=29dae24d76bea3bc62d07dd3e60cfc12003de7debbd6b768df9be9c68360174e;;
    esac;;
esac

directory=''
cleanup() { if [[ -n $directory ]]; then rm -rf -- "$directory"; fi; }
trap cleanup EXIT
directory=$(mktemp -d) || fail '无法创建临时目录。'

download() {
  curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 5 --output "$2" -- "$1" \
    || fail "下载失败：$1"
}

# Print the digest SHA256SUMS lists for exactly this file name; returns 1 when
# there is no such line.
sums_digest() {
  local name=$1 line digest rest
  while IFS= read -r line; do
    digest=${line%%[[:space:]]*}
    [[ $digest =~ ^[0-9a-fA-F]{64}$ ]] || continue
    rest=${line#"$digest"}
    rest=${rest#"${rest%%[![:space:]]*}"}
    [[ $rest == "$name" ]] || continue
    printf '%s' "${digest,,}"
    return 0
  done < "$directory/SHA256SUMS"
  return 1
}

bundle=proxy-control-$version-linux-$architecture
base=https://github.com/$repository/releases/download/v$version
printf '下载 %s ...\n' "$bundle.tar.gz"
download "$base/$bundle.tar.gz" "$directory/$bundle.tar.gz"
download "$base/SHA256SUMS" "$directory/SHA256SUMS"
expected=$(sums_digest "$bundle.tar.gz") || fail "SHA256SUMS 里没有 $bundle.tar.gz 的条目。"
actual=$(sha256sum -- "$directory/$bundle.tar.gz"); actual=${actual%% *}
[[ $actual == "$expected" ]] || fail "归档 SHA256 与 SHA256SUMS 不一致：$bundle.tar.gz"

source_dir=$directory/$bundle
tar --no-same-owner -xzf "$directory/$bundle.tar.gz" -C "$directory" || fail '归档解压失败。'
[[ -d $source_dir && ! -L $source_dir ]] || fail "归档里缺少 $bundle/ 目录。"
for required in bin/proxy-agent deploy/linux/install.sh deploy/linux/enroll.sh; do
  [[ -f $source_dir/$required && ! -L $source_dir/$required ]] || fail "归档里缺少 $required。"
done

binary=$source_dir/bin/proxy-agent
digest=$(sha256sum -- "$binary"); digest=${digest%% *}
core_arguments=(--core "$core" --core-version "$core_version")
if [[ -n $core_digest ]]; then core_arguments+=(--core-sha256 "$core_digest"); fi
printf '\n安装 Agent 与核心 %s %s ...\n' "$core" "$core_version"
"$source_dir/deploy/linux/install.sh" --node --binary "$binary" --sha256 "$digest" "${core_arguments[@]}" \
  || fail 'Agent 安装失败，本机未被修改。'

config=/etc/proxy-agent/config.yaml
ca_file=/etc/proxy-agent/tls/ca.crt
authority=$panel
[[ $http_port == 443 ]] || authority=$panel:$http_port
ca_url=https://$authority/install/ca.pem
fetched=$directory/panel-ca.pem

printf '\n获取面板证书信任锚：%s\n' "$ca_url"
if curl -fsS --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 3 --output "$fetched" -- "$ca_url"; then
  system_bundle=''
  for candidate in /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt; do
    if [[ -f $candidate && ! -L $candidate ]]; then system_bundle=$candidate; break; fi
  done
  [[ -n $system_bundle ]] || fail '面板证书可由公网 CA 验证，但本机没有系统 CA 包（/etc/ssl/certs/ca-certificates.crt、/etc/pki/tls/certs/ca-bundle.crt）。'
  install -o root -g proxy-agent -m 0644 -- "$system_bundle" "$ca_file" || fail "无法安装 $ca_file。"
  printf '面板证书由公网 CA 签发：已把系统 CA 包 %s 安装为 %s。\n' "$system_bundle" "$ca_file"
else
  printf '面板证书无法用系统信任链验证（自签或私有 CA）：改用固定 SHA-256 校验。\n'
  curl -fsS -k --proto '=https' --proto-redir '=https' --max-redirs 3 --output "$fetched" -- "$ca_url" || fail "无法从 $ca_url 下载面板证书。"
  actual=$(sha256sum -- "$fetched"); actual=${actual%% *}
  [[ $actual == "$ca_sha256" ]] || fail "面板证书 SHA-256 与面板显示的不一致（期望 $ca_sha256，实际 $actual）：可能是中间人攻击，或 --panel/--http-port 指向了别的面板。"
  install -o root -g proxy-agent -m 0644 -- "$fetched" "$ca_file" || fail "无法安装 $ca_file。"
  printf '面板证书 SHA-256 与面板显示的一致：已安装 %s。\n' "$ca_file"
fi

# Print the raw value of the first top-level "key: value" line of a file, with a
# trailing carriage return removed; the same simple parsing qui.sh uses. Returns
# 1 when the key is absent.
read_value() {
  local file=$1 key=$2 line
  while IFS= read -r line || [[ -n $line ]]; do
    if [[ $line =~ ^[[:space:]]*$key[[:space:]]*:[[:space:]]*(.*)$ ]]; then
      printf '%s\n' "${BASH_REMATCH[1]%$'\r'}"
      return 0
    fi
  done < "$file"
  return 1
}

# sed -i replaces the file through a temporary one, so the owner and mode of the
# service configuration are recorded first and restored (and verified) after the
# rewrite: the Agent runs as proxy-agent and reads this file itself.
[[ -f $config && ! -L $config ]] || fail "缺少配置文件：$config（Agent 安装未完成）。"
metadata=$(stat -c '%u:%g:%a' "$config")
sed -i -e "s/^control_address:.*/control_address: $panel:$grpc_port/" -e "s/^server_name:.*/server_name: $panel/" "$config" \
  || fail "无法更新 $config。"
uid=${metadata%%:*}; rest=${metadata#*:}; gid=${rest%%:*}; mode=${rest#*:}
chown "$uid:$gid" -- "$config" || fail "无法恢复 $config 的属主。"
chmod "$mode" -- "$config" || fail "无法恢复 $config 的权限。"
[[ $(stat -c '%u:%g:%a' "$config") == "$metadata" ]] || fail "配置文件属主或权限在更新后发生变化：$config"
[[ $(read_value "$config" control_address) == "$panel:$grpc_port" ]] || fail "配置里的 control_address 不是 $panel:$grpc_port：$config"
[[ $(read_value "$config" server_name) == "$panel" ]] || fail "配置里的 server_name 不是 $panel：$config"
printf '已写入 control_address: %s 与 server_name: %s。\n' "$panel:$grpc_port" "$panel"

token_directory=$(mktemp -d) || fail '无法创建注册码临时目录。'
token_file=$token_directory/enrollment-token
printf '%s\n' "$token" > "$token_file" || fail '无法写入注册码文件。'
chmod 0600 -- "$token_file"
printf '\n登记节点 ...\n'
if ! "$source_dir/deploy/linux/enroll.sh" --token-file "$token_file"; then
  rm -f -- "$token_file"
  rmdir -- "$token_directory" 2>/dev/null || true
  fail "节点登记失败；注册码可能已过期或已被使用。请确认本机能访问面板的 gRPC 端口（$panel:$grpc_port，默认 9443），且 --panel 与 --grpc-port 填写正确，然后在面板里新建节点注册，用新的安装命令重跑本脚本（本机尚未登记，可以重跑）。"
fi
rm -f -- "$token_file"
rmdir -- "$token_directory" 2>/dev/null || true

systemctl enable --now proxy-agent.service || fail '无法启动 proxy-agent.service；日志：journalctl -u proxy-agent -n 50'
printf 'proxy-agent.service 状态：%s\n' "$(systemctl is-active proxy-agent.service 2>/dev/null || true)"
printf '\n节点已安装并登记：面板 %s，核心 %s %s。\n' "$panel" "$core" "$core_version"
printf '注册码文件已删除；私钥只保存在本机 /var/lib/proxy-agent/identity。\n'
printf '在面板里为该节点配置入站并发布配置版本后才会代理流量。\n'
printf '以后升级用 qui-update（面板或节点主机），不要重跑本脚本。\n'