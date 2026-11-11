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
# registration, and the panel's gRPC port (18880 by default) has to be reachable
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
# HTTPS has to match --ca-sha256 as shown by the panel. Both ways check the fetched
# bytes against --ca-sha256, because behind a reverse proxy or Cloudflare the Web
# side serves the proxy's certificate while the Agent talks to the panel's gRPC
# port directly; the script therefore also probes that port with the anchor it just
# installed before it enrolls, and names the cause when the certificate there
# belongs to something else. The enrollment token is single use and expires; it is
# visible in the shell history of this host until that history is cleared.
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
  --http-port N         面板 HTTPS 端口（默认 22550）
  --grpc-port N         面板 gRPC 端口（默认 18880）
  --grpc-host HOST      Agent 连接的 gRPC 地址（默认与 --panel 相同；必须是
                        面板证书覆盖、且不经终止 TLS 的反向代理／CDN 的地址）
  --core xray|sing-box|gost  可重复，例如 --core xray --core sing-box（默认 xray）
  --core-source qui|official  sing-box 来源（默认 qui，同版本发布）
  --web-upgrade         安装完成后启用 Agent 网页升级（默认不启用；之后也可在本机运行 qui-update --component agent 启用）
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
grpc_host=''
http_port=22550
grpc_port=18880
ca_sha256=''
token=''
cores=(); core=''
core_source=qui
bundle_file=''
sums_file=''
web_upgrade=false
while (($#)); do
  case $1 in
    --web-upgrade) web_upgrade=true; shift;;
    --version|--panel|--grpc-host|--http-port|--grpc-port|--ca-sha256|--token|--core|--bundle|--sums|--core-source)
      (($# >= 2)) || fail "缺少 $1 的值。"
      case $1 in
        --version) version=$2;; --panel) panel=$2;; --grpc-host) grpc_host=$2;; --http-port) http_port=$2;;
        --grpc-port) grpc_port=$2;; --ca-sha256) ca_sha256=${2,,};; --token) token=$2;;
        --core) cores+=("$2");; --bundle) bundle_file=$2;; --sums) sums_file=$2;;
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
# The gRPC address defaults to the panel address; saving a different one is for a
# panel whose Web side is reached through another name. It is used verbatim as
# control_address and as the TLS server name, so it has to pass the same check.
[[ -n $grpc_host ]] || grpc_host=$panel
[[ $grpc_host =~ ^[A-Za-z0-9.-]+$ ]] || fail 'Agent gRPC 地址只能是域名或 IPv4（字母、数字、点、连字符，不含端口）。'
[[ $http_port =~ ^[0-9]{1,5}$ && $http_port -ge 1 && $http_port -le 65535 ]] || fail 'HTTPS 端口必须在 1-65535 之间。'
[[ $grpc_port =~ ^[0-9]{1,5}$ && $grpc_port -ge 1 && $grpc_port -le 65535 ]] || fail 'gRPC 端口必须在 1-65535 之间。'
# --http-port and --grpc-port are two listeners of the same panel, so one value
# cannot be both. A command that repeats one of them (a typo, or the habit of
# pasting the CDN port into both) used to fail much later, at the certificate
# download or inside enrollment, with a message that named neither flag.
((10#$http_port != 10#$grpc_port)) || fail '--http-port 与 --grpc-port 不能是同一个端口：--http-port 是面板 Web（HTTPS）端口，本脚本用它下载 /install/ca.pem 并登记，也就是面板 config.yaml 里 http_address 的端口（默认 22550）；--grpc-port 是 Agent 的 gRPC 端口（默认 18880）。同一块面板上这两个端口不同，请照面板注册对话框给出的命令原样执行。'
[[ $ca_sha256 =~ ^[0-9a-f]{64}$ ]] || fail '面板证书 SHA-256 必须是 64 位小写十六进制。'
[[ $token =~ ^[A-Za-z0-9_-]{16,128}$ ]] || fail '注册码必须是 16-128 位 URL 安全字符。'
if [[ ${#cores[@]} == 0 ]]; then cores=(xray); fi
seen=' '
for core in "${cores[@]}"; do
  [[ $core == xray || $core == sing-box || $core == gost ]] || fail 'Core must be xray, sing-box or gost.'
  [[ $seen != *" $core "* ]] || fail 'Duplicate --core.'
  # GOST only serves port forwards, so it can never be the first (primary) core.
  [[ $seen != ' ' || $core != gost ]] || fail 'GOST cannot be the first core; list --core xray or --core sing-box first.'
  seen+="$core "
done
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
core_arguments=(--core-source "$core_source" --qui-version "$version")
for core in "${cores[@]}"; do
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
    gost)
      # The reviewed official pin: install.sh owns the archive digest, so nothing is
      # passed here, and only amd64 is accepted whatever upstream publishes.
      [[ $architecture == amd64 ]] || fail '本机架构不支持 GOST：只接受 amd64。'
      core_version=3.3.2
      core_digest='';;
  esac
  core_arguments+=(--core "$core" --core-version "$core_version")
  if [[ $core == sing-box && $core_source == qui && -n $bundle_file ]]; then
    core_arguments+=(--core-bundle "$(dirname -- "$bundle_file")/sing-box-$core_version-qui.1-linux-$architecture.tar.gz" --core-sums "$sums_file")
  fi
  if [[ -n $core_digest ]]; then core_arguments+=(--core-sha256 "$core_digest"); fi
done
printf '\nInstalling Agent and cores: %s ...\n' "${cores[*]}"
"$source_dir/deploy/linux/install.sh" --node --binary "$binary" --sha256 "$digest" "${core_arguments[@]}" \
  || fail 'Agent 安装失败（原因见上面 install.sh 的输出）；install.sh 可能已经替换了部分文件，本机尚未登记，修正问题后可以直接重跑本脚本（install.sh 原地升级）。'

config=/etc/proxy-agent/config.yaml
ca_file=/etc/proxy-agent/tls/ca.crt
# The Agent's own certificate directory (v1.4.7 section 3): an inbound may name a
# certificate and key file from here instead of pasted material. install.sh
# created it above; repeating it keeps the one-command install self-contained and
# idempotent, because a re-run over an existing installation must not fail.
install -d -o root -g proxy-agent -m 0750 /etc/proxy-agent/certs \
  || fail '无法创建 /etc/proxy-agent/certs。'
# 443 is the implicit HTTPS port of a URL, not the default: the default 22550 and
# every other port has to be spelled out in the URL that fetches the CA.
authority=$panel
[[ $http_port == 443 ]] || authority=$panel:$http_port
ca_url=https://$authority/install/ca.pem
fetched=$directory/panel-ca.pem

printf '\n获取面板证书信任锚：%s\n' "$ca_url"
# Everything below can still fail on a host that is not enrolled yet: the trust
# anchor is not installed, or enrollment never ran. Rerunning this script with the
# same registration token works until it expires, so every failure here says so.
retry_note='本机尚未登记：注册码未过期时可以原样重跑本脚本。'

# Explain a failed GET /install/ca.pem from curl's HTTP status ("000" when the
# server never answered at all) and its exit code, so the operator reads which of
# the two panel ports or which name is wrong instead of only "download failed".
# Kept a pure function of those two arguments, because
# deploy/linux/tests/node-install-test.sh calls it with chosen values.
ca_download_diagnosis() {
  local http_status=$1 curl_status=$2
  case $http_status in
    415|426)
      printf '%s' "这个 HTTPS 端口回了 HTTP $http_status，这是 gRPC 监听对普通 HTTP 请求的答复：--http-port 指向的是 gRPC 端口，不是面板 Web 端口。--http-port 只能填面板 config.yaml 里 http_address 的端口（默认 22550，Web、API 与订阅都在这里），--grpc-port 才是 Agent 的 gRPC 端口（默认 18880）；同一块面板上这两个端口不会相同。"
      ;;
    404)
      printf '%s' "面板 Web 端口上的 $ca_url 返回 404：面板可能以开发模式运行（开发模式不提供 /install/ca.pem），或者 $http_port 端口上是别的 Web 服务／反向代理，并没有把 /install/ca.pem 转发给面板。请核对 --panel 与 --http-port（面板 config.yaml 的 http_address，默认 22550）。"
      ;;
    000)
      case $curl_status in
        6) printf '%s' "无法解析面板地址 $panel（DNS 查询失败）：请确认这个域名在本机能解析出来，或改用面板可直达的 IPv4。" ;;
        7) printf '%s' "无法连接 $authority（连接被拒绝或不可达）：请确认本机能访问 $panel 的这个 HTTPS 端口——防火墙是否放行、--http-port 是否为面板 Web 端口。" ;;
        28) printf '%s' "连接 $authority 超时：请确认本机到 $panel:$http_port 的网络与防火墙放行情况，以及 --http-port 是否正确。" ;;
        *) printf '%s' "无法与 $authority 建立 HTTPS 连接（curl 退出码 $curl_status）：请确认 $panel:$http_port 可达（DNS、防火墙、--http-port）。" ;;
      esac
      ;;
    *)
      printf '%s' "$ca_url 返回 HTTP $http_status（curl 退出码 $curl_status）：请核对 --panel 与 --http-port，并确认这个端口就是面板 Web（HTTPS）端口。"
      ;;
  esac
}

# True when the downloaded file itself chains to the system trust store. The
# connection test in install_panel_ca cannot answer this: behind Cloudflare or a
# TLS-terminating reverse proxy the edge certificate is publicly trusted even when
# the panel's own certificate — what /install/ca.pem returns and what the gRPC port
# presents — is self-signed. openssl verify reads the file, not the transport. A
# host without openssl falls through to pinning the fetched certificate, which is
# still a correct trust anchor for the Agent.
panel_ca_is_public() {
  local file=$1
  command -v openssl >/dev/null 2>&1 || return 1
  openssl verify -CAfile "$system_bundle" "$file" >/dev/null 2>&1
}

# Establish trust in the panel certificate and install it as $ca_file. Two ways,
# one check: the bytes served by /install/ca.pem have to hash to --ca-sha256 in
# both, because that digest is what the panel showed the operator, and a proxy or
# another web server on the same name would otherwise pass unnoticed.
install_panel_ca() {
  local actual http_status status
  # --cacert and --no-insecure make the download itself validated; a host whose
  # curl skips verification falls through to the pinned SHA-256 instead. What the
  # download proves is the transport, so the branch below still asks whether the
  # fetched certificate is public (panel_ca_is_public) before installing the whole
  # system bundle.
  if [[ -n $system_bundle ]] && curl --no-insecure --cacert "$system_bundle" -fs --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 3 --output "$fetched" -- "$ca_url"; then
    actual=$(sha256sum -- "$fetched"); actual=${actual%% *}
    [[ $actual == "$ca_sha256" ]] || fail "https://$authority/install/ca.pem 提供的证书能被系统信任链验证，但它的 SHA-256 是 $actual，与面板显示的 $ca_sha256 不一致：这个地址取到的不是本面板的证书，而是别的面板、别的 Web 服务，或一个返回自己证书的反向代理／CDN。请核对 --panel 与 --http-port；$retry_note"
    if panel_ca_is_public "$fetched"; then
      install -o root -g proxy-agent -m 0644 -- "$system_bundle" "$ca_file" || fail "无法安装 $ca_file。"
      printf '面板证书由公网 CA 签发并已核对 SHA-256：已把系统 CA 包 %s 安装为 %s。\n' "$system_bundle" "$ca_file"
    else
      # Public edge certificate, private panel certificate: the Agent connects to the
      # panel's gRPC port directly, so the pinned panel certificate is its anchor.
      install -o root -g proxy-agent -m 0644 -- "$fetched" "$ca_file" || fail "无法安装 $ca_file。"
      printf '面板证书无法用系统信任链验证（自签、私有 CA，或 Web 一侧是终止 TLS 的反向代理／CDN），但已按面板显示的 SHA-256 核对：已安装 %s。\n' "$ca_file"
    fi
    return 0
  fi
  printf '面板证书无法用系统信任链验证（自签、私有 CA，或 Web 一侧是终止 TLS 的反向代理／CDN）：改用固定 SHA-256 校验。\n'
  # -k is deliberate here: the peer certificate is the file being fetched, so it
  # cannot be trusted in advance and --ca-sha256 is the check. The HTTP status is
  # read apart from the exit code so that a failure can name its cause (gRPC port,
  # wrong web port, 404 from a development panel) instead of only "download
  # failed". --no-insecure still overrides a -k-adding wrapper.
  status=0
  http_status=$(curl --no-insecure -k -sS --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 3 --connect-timeout 15 --max-time 60 --output "$fetched" --write-out '%{http_code}' -- "$ca_url") || status=$?
  if ((status != 0)) || [[ $http_status != 200 ]]; then
    fail "无法从 $ca_url 下载面板证书：$(ca_download_diagnosis "$http_status" "$status") $retry_note"
  fi
  actual=$(sha256sum -- "$fetched"); actual=${actual%% *}
  [[ $actual == "$ca_sha256" ]] || fail "面板证书 SHA-256 与面板显示的不一致（期望 $ca_sha256，实际 $actual）：可能是中间人攻击，或 --panel/--http-port 指向了别的面板。 $retry_note"
  install -o root -g proxy-agent -m 0644 -- "$fetched" "$ca_file" || fail "无法安装 $ca_file。"
  printf '面板证书 SHA-256 与面板显示的一致：已安装 %s。\n' "$ca_file"
}

system_bundle=''
for candidate in /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt; do
  if [[ -f $candidate && ! -L $candidate ]]; then system_bundle=$candidate; break; fi
done
install_panel_ca

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
sed -i -e "s/^control_address:.*/control_address: $grpc_host:$grpc_port/" -e "s/^server_name:.*/server_name: $grpc_host/" "$config" \
  || fail "无法更新 $config。"
uid=${metadata%%:*}; rest=${metadata#*:}; gid=${rest%%:*}; mode=${rest#*:}
chown "$uid:$gid" -- "$config" || fail "无法恢复 $config 的属主。"
chmod "$mode" -- "$config" || fail "无法恢复 $config 的权限。"
[[ $(stat -c '%u:%g:%a' "$config") == "$metadata" ]] || fail "配置文件属主或权限在更新后发生变化：$config"
[[ $(read_value "$config" control_address) == "$grpc_host:$grpc_port" ]] || fail "配置里的 control_address 不是 $grpc_host:$grpc_port：$config"
[[ $(read_value "$config" server_name) == "$grpc_host" ]] || fail "配置里的 server_name 不是 $grpc_host：$config"
printf '已写入 control_address: %s 与 server_name: %s。\n' "$grpc_host:$grpc_port" "$grpc_host"

# The enrollment that follows is the Agent's first real gRPC handshake: config.yaml
# now names $grpc_host:$grpc_port and $ca_file is its trust anchor. Behind Cloudflare or
# a TLS-terminating reverse proxy the Web side served the proxy's certificate, so
# the branch above could look right while this port is the panel itself; enrollment
# then fails late inside enroll.sh with a generic message. Probing costs one
# connection and names the cause; nothing is enrolled yet either way.

# Does this host's curl speak HTTP/2? grpc-go answers over h2 only, and a curl built
# without nghttp2 rejects --http2 with exit code 2, which would read as an ambiguous
# result instead of a verdict.
curl_supports_http2() {
  curl -V 2>/dev/null | grep -q 'HTTP2'
}

# TCP-only reachability of host:port, for a curl without HTTP/2. bash cannot time
# out its own /dev/tcp redirection, so the attempt runs under timeout(1) when the
# host has it (coreutils and busybox both ship it); without it a killer process
# bounds the wait instead.
tcp_reachable() {
  local host=$1 port=$2 pid killer
  if command -v timeout >/dev/null 2>&1; then
    timeout 10 "${BASH:-bash}" -c 'exec 3<>"/dev/tcp/$1/$2"' _ "$host" "$port" 2>/dev/null
    return
  fi
  ( exec 3<>"/dev/tcp/$host/$port" ) 2>/dev/null &
  pid=$!
  ( sleep 10; kill -TERM "$pid" 2>/dev/null ) >/dev/null 2>&1 &
  killer=$!
  if wait "$pid" 2>/dev/null; then
    kill -TERM "$killer" 2>/dev/null || true
    return 0
  fi
  kill -TERM "$killer" 2>/dev/null || true
  return 1
}

# Read the preflight's curl exit code. 0 and 22 are a verified handshake: gRPC
# answers a plain GET with an HTTP error (415), which curl turns into 22 only with
# -f, so both mean the TLS handshake succeeded and the certificate covers the name.
# The certificate verdicts mean a trust anchor or a name that does not fit; the
# network ones mean nothing answers on that path.
grpc_preflight_verdict() {
  case $1 in
    0|22) printf 'ok' ;;
    51|58|60|77|90|91) printf 'untrusted' ;;
    6|7|28) printf 'unreachable' ;;
    *) printf 'unknown' ;;
  esac
}

# Probe https://$grpc_host:$grpc_port/ with the trust the Agent itself will use:
# --cacert "$ca_file" (the anchor just installed), no -k, no redirects, HTTP/2 when
# this curl has it. A failure here is cheap to fix and its cause is known; the same
# failure inside enroll.sh only produces the generic enrollment message. A curl
# without HTTP/2 can prove reachability only, so its result is never read as a
# certificate verdict.
check_grpc_endpoint() {
  local status=0 verdict tls_checked=true note
  if curl_supports_http2; then
    curl --no-insecure --cacert "$ca_file" --http2 -sS --output /dev/null \
      --proto '=https' --proto-redir '=https' --max-redirs 0 --connect-timeout 10 --max-time 10 \
      -- "https://$grpc_host:$grpc_port/" || status=$?
    verdict=$(grpc_preflight_verdict "$status")
  else
    tls_checked=false
    if tcp_reachable "$grpc_host" "$grpc_port"; then verdict=ok; else verdict=unreachable; fi
  fi
  case $verdict in
    ok)
      if $tls_checked; then
        printf 'Agent gRPC 端点的 TLS 握手验证通过：%s:%s 出示的证书由刚安装的信任锚签发并覆盖 %s。\n' "$grpc_host" "$grpc_port" "$grpc_host"
      else
        printf '本机 curl 没有 HTTP/2 支持：只确认了 %s:%s 的 TCP 端口可达，未校验 TLS 证书。\n' "$grpc_host" "$grpc_port"
      fi
      ;;
    untrusted)
      fail "Agent 的 gRPC 端点在 TLS 校验上失败（curl 退出码 $status）：https://$grpc_host:$grpc_port/ 出示的证书不被刚安装的信任锚验证，或者证书不覆盖 $grpc_host 这个名字。最常见的原因是面板在 Cloudflare 或一个终止 TLS 的反向代理后面——Web 一侧返回的是 CDN／代理的公网证书，而 Agent 直连 gRPC 看到的是面板自己的（多为私有 CA 签发的）证书，两者不是同一张证书，mTLS 的 gRPC 也无法经过 Cloudflare。请把 --grpc-host 换成直达面板、且被面板自己的证书覆盖的地址（DNS-only 的灰云域名或面板的 IPv4，默认与 --panel 相同），--http-port 填面板自己的 Web 端口而不是代理的 443，并在防火墙放行这两个端口；也可以在 系统管理 → 系统设置 → 基础设置 → 节点安装连接地址 里保存这些地址与端口，之后生成的安装命令都用它。$retry_note"
      ;;
    unreachable)
      if $tls_checked; then note="curl 退出码 $status"; else note='TCP 连接被拒绝或超时'; fi
      fail "无法连接面板的 gRPC 端口 $grpc_host:$grpc_port（$note）：本机到该端口没有通路——防火墙未放行、--grpc-host／--grpc-port 填错，或者这个主机名走的是不转发该端口的 CDN。登记要求 Agent 直连这个端口。$retry_note"
      ;;
    *)
      printf '警告：无法判断 Agent gRPC 端点 %s:%s 的状态（curl 退出码 %s），继续登记；若登记失败请核对 --grpc-host、--grpc-port 与面板证书。\n' "$grpc_host" "$grpc_port" "$status" >&2
      ;;
  esac
}
printf '\n检查 Agent gRPC 端点：https://%s:%s/\n' "$grpc_host" "$grpc_port"
check_grpc_endpoint

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
  fail "节点登记失败；注册码可能已过期或已被使用。请确认本机能访问面板的 gRPC 端口（$grpc_host:$grpc_port，默认 18880），且 --grpc-host 与 --grpc-port 填写正确，然后在面板里新建节点注册，用新的安装命令重跑本脚本（本机尚未登记，可以重跑）。"
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
if [[ $web_upgrade == true ]]; then
  agent_enable_web_upgrade || true
fi
printf '\n节点已安装并登记：面板 %s，核心 %s。\n' "$panel" "${cores[*]}"
if [[ $web_upgrade != true ]]; then
  printf '网页升级未启用；需要在面板网页上升级 Agent 时，在本机运行一次 qui-update --component agent。\n'
fi
printf '注册码文件已删除；私钥只保存在本机 /var/lib/proxy-agent/identity。\n'
printf '在面板里为该节点配置入站并发布配置版本后才会代理流量。\n'
printf '以后升级用 qui-update（面板或节点主机），不要重跑本脚本。\n'
