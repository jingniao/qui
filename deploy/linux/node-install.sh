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
# or that still runs the Agent service, is refused.
#
# The host may run systemd (Debian, Ubuntu) or OpenRC (Alpine, including an LXC
# container without systemd): the init system is detected before anything is
# downloaded and the archive's deploy/linux/service.sh then starts the service the
# way that init system expects. --bundle and --sums install from a local release
# archive instead of GitHub, for a node that has no route to it.
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
用法：node-install.sh --version X.Y.Z[-rc.N] --panel HOST --ca-sha256 HEX --token TOKEN [选项]

  --version X.Y.Z[-rc.N]       面板发布版本，必须与面板版本一致
  --panel HOST          面板地址：域名或 IPv4
  --ca-sha256 HEX       面板证书信任锚的 SHA-256（64 位小写十六进制，面板显示）
  --token TOKEN         一次性节点注册码
  --http-port N         面板 HTTPS 端口（默认 443）
  --grpc-port N         面板 gRPC 端口（默认 9443）
  --core xray|sing-box  同时安装的核心（默认 xray）
  --core-source qui|official  sing-box 来源（默认 qui，同版本发布）
  --bundle FILE --sums FILE
                        离线安装：用本地发布归档与其 SHA256SUMS，不访问 GitHub
                        （两个参数必须同时给出）

脚本从 https://github.com/$repository/releases 下载与面板同版本的归档，用归档里
的 deploy/linux/install.sh 安装 Agent 与核心，按公网证书或固定 SHA-256 建立对面板
证书的信任，再用归档里的 deploy/linux/enroll.sh 登记并启动 Agent。注册码只能用
一次且会过期；节点登记成功前可以重跑本脚本（登记成功后会被拒绝），升级用 qui-update。
支持 systemd（Debian、Ubuntu）与 OpenRC（Alpine，含没有 systemd 的 LXC 容器）。
USAGE
}

version=''
panel=''
http_port=443
grpc_port=9443
ca_sha256=''
token=''
core=xray
core_source=qui
bundle_file=''
sums_file=''
while (($#)); do
  case $1 in
    --version|--panel|--http-port|--grpc-port|--ca-sha256|--token|--core|--bundle|--sums|--core-source)
      (($# >= 2)) || fail "缺少 $1 的值。"
      case $1 in
        --version) version=$2;; --panel) panel=$2;; --http-port) http_port=$2;;
        --grpc-port) grpc_port=$2;; --ca-sha256) ca_sha256=${2,,};; --token) token=$2;;
        --core) core=$2;; --bundle) bundle_file=$2;; --sums) sums_file=$2;;
        --core-source) core_source=$2;;
      esac
      shift 2;;
    --help) usage; exit 0;;
    *) fail "未知参数：$1（用 --help 查看用法）";;
  esac
done

[[ $core_source == qui || $core_source == official ]] || fail 'Invalid core source.'
[[ $version =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$ ]] || fail '版本号必须是 X.Y.Z 或 X.Y.Z-rc.N（N 为无前导零的正整数）。'
[[ $panel =~ ^[A-Za-z0-9.-]+$ ]] || fail '面板地址只能是域名或 IPv4（字母、数字、点、连字符，不含端口）。'
[[ $http_port =~ ^[0-9]{1,5}$ && $http_port -ge 1 && $http_port -le 65535 ]] || fail 'HTTPS 端口必须在 1-65535 之间。'
[[ $grpc_port =~ ^[0-9]{1,5}$ && $grpc_port -ge 1 && $grpc_port -le 65535 ]] || fail 'gRPC 端口必须在 1-65535 之间。'
[[ $ca_sha256 =~ ^[0-9a-f]{64}$ ]] || fail '面板证书 SHA-256 必须是 64 位小写十六进制。'
[[ $token =~ ^[A-Za-z0-9_-]{16,128}$ ]] || fail '注册码必须是 16-128 位 URL 安全字符。'
[[ $core == xray || $core == sing-box ]] || fail '核心只能是 xray 或 sing-box。'
# An offline installation supplies both files: the archive is checked against the
# supplied SHA256SUMS exactly like a download, so one path has no weaker trust.
if [[ -n $bundle_file || -n $sums_file ]]; then
  [[ -n $bundle_file && -n $sums_file ]] || fail '--bundle 与 --sums 必须同时给出（离线安装需要发布归档和它的 SHA256SUMS）。'
  for offline in "$bundle_file" "$sums_file"; do
    [[ -f $offline && ! -L $offline ]] || fail "离线安装的文件必须是普通文件（不能是目录或符号链接）：$offline"
  done
fi

[[ $(uname -s) == Linux && $EUID -eq 0 ]] || fail '请以 root 身份在 Linux 上运行。'
# A stock Alpine host has none of bash, curl, ca-certificates or GNU tar, but this
# script is bash, the archive is fetched with curl and unpacked with GNU tar, so
# they are installed before anything is checked. busybox tar is not enough:
# --no-same-owner is a GNU option. On any other host this is a no-op because apk
# does not exist there.
if command -v apk >/dev/null 2>&1; then
  if ! command -v curl >/dev/null 2>&1 || ! tar --version 2>/dev/null | grep -q GNU; then
    apk add --no-cache bash curl ca-certificates tar \
      || fail '无法安装 bash、curl、ca-certificates 与 tar：请先执行 apk add --no-cache bash curl ca-certificates tar，再重跑本脚本。'
  fi
fi
# The init system decides which service tool is required and which command starts
# the Agent at the end. It is detected here, before anything is downloaded, so an
# unsupported host fails before it touches the network.
if [[ -d /run/systemd/system ]]; then
  init_system=systemd
elif [[ -d /run/openrc ]] && { [[ -x /sbin/openrc-run ]] || command -v openrc-run >/dev/null 2>&1; }; then
  init_system=openrc
else
  fail '不支持的初始化系统：需要 systemd 或 OpenRC。'
fi
required_tools=(curl tar sha256sum install mktemp stat chown chmod sed rm uname)
if [[ $init_system == systemd ]]; then
  required_tools+=(systemctl)
else
  required_tools+=(rc-service rc-update)
fi
for tool in "${required_tools[@]}"; do
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
# OpenRC has no is-active; the runscript's status is the equivalent verdict.
if [[ $init_system == systemd ]]; then
  if systemctl is-active --quiet proxy-agent.service; then agent_running=true; else agent_running=false; fi
else
  if rc-service proxy-agent status >/dev/null 2>&1; then agent_running=true; else agent_running=false; fi
fi
if $agent_running; then
  fail '请先停止 proxy-agent 服务（systemctl stop proxy-agent.service 或 rc-service proxy-agent stop）再重跑本脚本。'
fi

case $(uname -m) in
  x86_64) architecture=amd64;;
  aarch64|arm64) architecture=arm64;;
  *) fail "不支持的架构：$(uname -m)";;
esac

# The official sing-box release ships a glibc build and a musl build per
# architecture, and only the second one starts on Alpine: the plain archive is
# linked against the glibc loader, so the kernel answers "not found" for a binary
# that is present. install.sh names the archive the same way from its own copy of
# this detection, and both scripts run on the same host. The loader is looked up
# in the directory given as the first argument (default /lib), so the regression
# test (deploy/linux/tests/core-asset-test.sh) can hand it a directory of its own.
host_libc() {
  local directory=${1:-/lib} loader
  for loader in "$directory"/ld-musl-*.so.1; do
    if [[ -e $loader ]]; then printf 'musl'; return; fi
  done
  printf 'glibc'
}
# The digest of the sing-box archive install.sh will download for this host:
# every architecture and libc combination has its own official GitHub asset
# digest of sing-box-1.14.3, and the musl archive is a different file from the
# glibc one.
singbox_digest() {
  case $1:$2 in
    amd64:glibc) printf 'e2bdf179c15a3652955dc44867e33ca0965c9c9b91b34267dcc5a5d639a5feee';;
    arm64:glibc) printf '29dae24d76bea3bc62d07dd3e60cfc12003de7debbd6b768df9be9c68360174e';;
    amd64:musl) printf '41fc81fd041134d4d7d850a928d7d2cde17a7d6563953f406b22d32a68bfab1f';;
    arm64:musl) printf '12d6d67a4edf0dd54590865b1f4f79cd09ae043a02d648e9d1b5fccc8446ee07';;
    *) fail "不支持的架构与 libc 组合：$1:$2";;
  esac
}

# The core release is pinned here and install.sh verifies the archive before it
# installs anything. Xray publishes a .dgst file next to its asset and install.sh
# reads it itself; sing-box publishes no such file, so its asset digest is pinned
# per architecture and libc (official GitHub asset digests of sing-box-1.14.3).
case $core in
  xray)
    core_version=26.3.27
    core_digest='';;
  sing-box)
    if [[ $core_source == qui ]]; then
      core_version=1.14.4
      core_digest=''
    else
      core_version=1.14.3
      libc=$(host_libc)
      core_digest=$(singbox_digest "$architecture" "$libc")
    fi;;
esac

directory=''
cleanup() { if [[ -n $directory ]]; then rm -rf -- "$directory"; fi; }
trap cleanup EXIT
directory=$(mktemp -d) || fail '无法创建临时目录。'

# --no-insecure overrides a -k that a curl wrapper or alias may put in front of
# the arguments (one Alpine LXC image ships /usr/bin/curl as `curl.orig -k "$@"`),
# so a download is never accepted without a verified certificate.
download() {
  curl --no-insecure -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 5 --output "$2" -- "$1" \
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
if [[ -n $bundle_file ]]; then
  # Offline installation: the two local files are copied under the names the rest
  # of the script uses, so the digest check below is the same code for both paths
  # and the archive is verified against its SHA256SUMS either way.
  install -o root -g root -m 0600 -- "$bundle_file" "$directory/$bundle.tar.gz" || fail '无法复制离线归档。'
  install -o root -g root -m 0600 -- "$sums_file" "$directory/SHA256SUMS" || fail '无法复制离线 SHA256SUMS。'
  printf '使用离线归档 %s 与其 SHA256SUMS。\n' "${bundle_file##*/}"
else
  printf '下载 %s ...\n' "$bundle.tar.gz"
  download "$base/$bundle.tar.gz" "$directory/$bundle.tar.gz"
  download "$base/SHA256SUMS" "$directory/SHA256SUMS"
fi
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
[[ $core_source == qui || $core_source == official ]] || fail 'Invalid core source.'
core_arguments=(--core "$core" --core-version "$core_version" --core-source "$core_source" --qui-version "$version")
if [[ $core == sing-box && $core_source == qui && -n $bundle_file ]]; then
  core_arguments+=(--core-bundle "$(dirname -- "$bundle_file")/sing-box-$core_version-qui.1-linux-$architecture.tar.gz" --core-sums "$sums_file")
fi
if [[ -n $core_digest ]]; then core_arguments+=(--core-sha256 "$core_digest"); fi
printf '\n安装 Agent 与核心 %s %s ...\n' "$core" "$core_version"
"$source_dir/deploy/linux/install.sh" --node --binary "$binary" --sha256 "$digest" "${core_arguments[@]}" \
  || fail 'Agent 安装失败（原因见上面 install.sh 的输出）；install.sh 可能已经替换了部分文件，本机尚未登记，修正问题后可以直接重跑本脚本（install.sh 原地升级）。'

config=/etc/proxy-agent/config.yaml
ca_file=/etc/proxy-agent/tls/ca.crt
authority=$panel
[[ $http_port == 443 ]] || authority=$panel:$http_port
ca_url=https://$authority/install/ca.pem
fetched=$directory/panel-ca.pem

printf '\n获取面板证书信任锚：%s\n' "$ca_url"
# The panel certificate counts as publicly trusted only when it verifies against
# the very bundle that would become the node CA: --cacert names that bundle and
# --no-insecure overrides a -k that a curl wrapper may add, so a host whose curl
# skips verification falls through to the pinned SHA-256 instead of installing a
# bundle the Agent's gRPC handshake would then reject.
system_bundle=''
for candidate in /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt; do
  if [[ -f $candidate && ! -L $candidate ]]; then system_bundle=$candidate; break; fi
done
if [[ -n $system_bundle ]] && curl --no-insecure --cacert "$system_bundle" -fs --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 3 --output "$fetched" -- "$ca_url"; then
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

# A run whose enrollment failed leaves its retry marker (enrollment-attempt.json)
# and the private key it generated. The Agent binds both to that token and panel
# and refuses to reuse the key for another binding, so the fresh registration a
# rerun brings would be refused for good. Nothing was enrolled with that key - no
# certificate and no enrollment result exist, or this run was already refused
# above or here - so the two files are moved aside, never deleted, and the Agent
# generates a new key. An enrollment result means the panel may have issued a
# certificate; that state is left for the operator.
data_directory=/var/lib/proxy-agent
[[ ! -e $data_directory/enrollment-result.json && ! -L $data_directory/enrollment-result.json ]] \
  || fail '本机存在未完成确认的登记结果（enrollment-result.json）；处理方法见 docs/operations.md 第 5 节。'
if [[ -e $data_directory/enrollment-attempt.json || -L $data_directory/enrollment-attempt.json ]]; then
  retired=$(mktemp -d /var/lib/proxy-agent-enrollment-retired.XXXXXXXX) || fail '无法创建旧登记尝试的保留目录。'
  mv -- "$data_directory/enrollment-attempt.json" "$retired/" || fail '无法移走旧的登记尝试。'
  if [[ -e $data_directory/identity/client.key || -L $data_directory/identity/client.key ]]; then
    mv -- "$data_directory/identity/client.key" "$retired/" || fail '无法移走旧登记尝试的私钥。'
  fi
  printf '上一次未完成的登记尝试（标记与私钥）已移到 %s，未删除；确认新登记成功后可删除该目录。\n' "$retired"
fi

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

# The init-system library comes from the archive that was just verified, so the
# same helper that decided the service file starts the service here; nothing is
# started by install.sh itself.
[[ -f $source_dir/deploy/linux/service.sh && ! -L $source_dir/deploy/linux/service.sh ]] || fail "归档里缺少 deploy/linux/service.sh。"
# shellcheck source=service.sh
. "$source_dir/deploy/linux/service.sh"
init_system=$(detect_init) || fail '不支持的初始化系统：需要 systemd 或 OpenRC。'
service_enable proxy-agent && service_start proxy-agent \
  || fail "无法启动 proxy-agent；日志：$(service_log_hint proxy-agent)"
# install.sh also installs/enables the fixed tuning helper (systemd path/oneshot
# or OpenRC inotifyd watcher); it must be armed before the first Agent command.
if [[ $init_system == systemd ]]; then
  systemctl start proxy-agent-tuning.path || fail '无法启动调优 path 单元。'
else
  service_is_active proxy-agent-tuning || service_start proxy-agent-tuning || fail '无法启动调优 watcher。'
fi
printf 'proxy-agent 服务状态：%s\n' "$(service_state proxy-agent)"
printf '\n节点已安装并登记：面板 %s，核心 %s %s。\n' "$panel" "$core" "$core_version"
printf '注册码文件已删除；私钥只保存在本机 /var/lib/proxy-agent/identity。\n'
printf '在面板里为该节点配置入站并发布配置版本后才会代理流量。\n'
printf '以后升级用 qui-update（面板或节点主机），不要重跑本脚本。\n'
