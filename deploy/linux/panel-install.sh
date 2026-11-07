#!/usr/bin/env bash
# Install, inspect, update and remove the qui panel on the panel host with one
# command.
#
# The documented use is interactive:
#   wget -O panel-install.sh \
#     https://raw.githubusercontent.com/jingniao/qui/main/deploy/linux/panel-install.sh
#   bash panel-install.sh
# It asks for the version, the two ports, the public address and how to get the
# TLS certificate, prints a summary and installs only after a typed confirmation.
# Every question has a default and every answer can be given as a flag instead
# (--version, --http-port, --grpc-port, --address, --tls, --cert, --key, --yes,
# --update), so the same script also runs unattended.
#
# Trust in the release rests on the public GitHub repository: the archive and the
# SHA256SUMS published with it are downloaded and the archive is verified against
# that SHA256SUMS before anything is installed. The archive's own
# deploy/linux/install.sh installs the program, the service account, the fixed
# AGENTS.md section 30 directories and the systemd unit; this script then writes
# the ports and certificate paths of the packaged configuration template,
# validates the result with the installed program (--check-config), enables and
# starts proxy-panel.service and waits until it stays up. --bundle/--sums install
# from a local archive instead of GitHub, for a host with no route to it.
#
# On a host that already has the panel installed the same script is a small menu:
# update (through qui-update, which owns the database backup, the snapshot and
# the rollback), status, restart and uninstall. There --yes only reports what is
# installed and changes nothing, and --update runs that same update without the
# menu, for a scripted host. The terminal menu "qui" remains the place to change
# the Owner credentials or the port.
#
# Keepalive is the packaged unit: Restart=on-failure with RestartSec=5 plus the
# boot enable this script performs. No cron job, timer or watchdog daemon is
# installed. A host without systemd is refused, because that unit is the
# keepalive and the panel is installed as a service only.
set -euo pipefail
umask 077
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

repository=jingniao/qui
service=proxy-panel.service
account=proxy-panel
version_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$'

# Root of every absolute path below. It is empty on a real host; only the
# regression test (deploy/linux/tests/panel-install-test.sh) sets it, the same way
# QUI_UPDATE_TEST_ROOT works for qui-update. Only root can set the environment of
# a root shell, so it is trusted like $PATH.
root=${QUI_PANEL_INSTALL_TEST_ROOT:-}
registry=$root/var/lib/proxycontrol-install
manifest=$registry/panel.manifest
etc=$root/etc/proxy-panel
config=$etc/config.yaml
data=$root/var/lib/proxy-panel
program=$root/usr/local/bin/proxy-panel
updater=$root/usr/local/bin/qui-update
certificate=$etc/tls/server.crt
certificate_key=$etc/tls/server.key

fail() { printf 'panel-install：%s\n' "$*" >&2; exit 1; }

usage() {
  cat <<USAGE
用法：bash panel-install.sh [选项]

  不给选项时是交互式安装：每一项都有默认值，最后汇总确认后才动手。
  已经安装过面板的主机上，同一个脚本会显示一个菜单：
  1 更新（调用 qui-update，由它备份数据库、做快照并在失败时回滚）、
  2 查看状态、3 重启、4 卸载（需要输入 uninstall 确认）、0 退出。
  菜单里任何一步失败都只报错并回到菜单，不会结束脚本。
  修改管理员密码或端口请用 qui 菜单。

  --version X.Y.Z[-rc.N]  安装指定发布版本（默认：GitHub 上的最新正式发布）
  --http-port N           管理后台与 API 的 HTTPS 端口（默认 22550）
  --grpc-port N           节点 Agent 的 gRPC 端口（默认 18880）
  --address HOST          面板对外地址：域名、公网 IPv4 或 IPv6（默认自动探测，
                          IPv6 可写 [2001:db8::1] 形式；用于访问链接与自签名证书的 SAN）
  --tls self-signed|files 证书来源（默认 self-signed：用 openssl 现场生成 10 年
                          有效的自签名证书）
  --cert FILE --key FILE  --tls files 时的证书与私钥文件（必须是普通文件，
                          且证书与私钥匹配）
  --yes                   不提问也不确认，未给出的项全部取默认值（自动化用）；
                          已安装的主机上只打印当前安装状态，不改任何东西
  --update                已安装的主机上直接更新（不显示菜单），可配合 --version、
                          --bundle/--sums 与 --yes；未安装的主机上会报错退出
  --bundle FILE --sums FILE
                          离线安装：用本地发布归档与其 SHA256SUMS，不访问 GitHub
                          （两个参数必须同时给出，并且必须用 --version 指定版本）

脚本从 https://github.com/$repository/releases 下载归档与 SHA256SUMS，校验摘要后
调用归档里的 deploy/linux/install.sh 安装程序、服务账号、目录与 systemd 单元，
再写入端口与证书路径、校验配置、enable --now 启动并等待其稳定运行。
重跑本脚本不会覆盖已有的配置、证书与数据库；上次卸载留下的记录（panel.removed）
会检出并自动用 --reinstall 重装，配置与数据库照旧保留。升级一律走 qui-update。
使用文档见 docs/install-guide.md，细节见 docs/deployment.md。
USAGE
}

version=''
http_port=''
grpc_port=''
address=''
tls_mode=''
certificate_source=''
key_source=''
assume_yes=false
update_only=false
bundle_file=''
sums_file=''
# Answer of the last ask(), and the temporary download directory the EXIT trap removes.
answer=''
directory=''

while (($#)); do
  case $1 in
    --version|--http-port|--grpc-port|--address|--tls|--cert|--key|--bundle|--sums)
      (($# >= 2)) || fail "缺少 $1 的值。"
      case $1 in
        --version) version=$2;;
        --http-port) http_port=$2;;
        --grpc-port) grpc_port=$2;;
        --address) address=$2;;
        --tls) tls_mode=$2;;
        --cert) certificate_source=$2;;
        --key) key_source=$2;;
        --bundle) bundle_file=$2;;
        --sums) sums_file=$2;;
      esac
      shift 2;;
    --yes|-y) assume_yes=true; shift;;
    --update) update_only=true; shift;;
    --help|-h) usage; exit 0;;
    *) fail "未知参数：$1（用 --help 查看用法）";;
  esac
done

is_version() { [[ $1 =~ $version_pattern ]]; }
valid_port() { [[ $1 =~ ^[0-9]{1,5}$ ]] && (( 10#$1 >= 1 && 10#$1 <= 65535 )); }
regular_file() { [[ -f $1 && ! -L $1 ]]; }

# The address without the brackets of an https://[v6]:port/ URL. A domain and an
# IPv4 literal come back unchanged.
address_host() {
  local value=$1
  if [[ $value == \[*\] ]]; then value=${value#[}; value=${value%]}; fi
  printf '%s\n' "$value"
}

# An IPv6 literal is hexadecimal groups and colons with at least two colons, with or
# without the brackets of a URL; a value with a single colon is a host:port and not a
# literal. Only the syntax is checked here, openssl is the real gate for the
# certificate.
is_ipv6_literal() {
  local value
  value=$(address_host "$1")
  [[ $value =~ ^[0-9A-Fa-f:.]+$ && $value == *:*:* ]]
}

# What --address and the prompt accept: a domain, an IPv4 or an IPv6 literal, without
# a port.
address_allowed() {
  local value
  value=$(address_host "$1")
  [[ $value =~ ^[A-Za-z0-9.-]+$ ]] || is_ipv6_literal "$value"
}

[[ -z $version ]] || is_version "$version" \
  || fail '版本号必须是 X.Y.Z 或 X.Y.Z-rc.N（N 为无前导零的正整数）。'
[[ -z $http_port ]] || valid_port "$http_port" || fail 'HTTPS 端口必须在 1-65535 之间。'
[[ -z $grpc_port ]] || valid_port "$grpc_port" || fail 'gRPC 端口必须在 1-65535 之间。'
[[ -z $http_port || -z $grpc_port || $http_port != "$grpc_port" ]] \
  || fail 'HTTPS 端口与 gRPC 端口不能相同。'
case ${tls_mode:-self-signed} in
  self-signed|files) ;;
  *) fail "--tls 只接受 self-signed 或 files（收到 $tls_mode）。";;
esac
if [[ -n $address ]]; then
  address_allowed "$address" \
    || fail '地址只能是域名、IPv4 或 IPv6（域名可含字母、数字、点、连字符，IPv6 可带方括号，均不含端口）。'
  # Everything below uses the bare literal, so the SAN of an IPv6 address and its URL
  # brackets are written in exactly one place each.
  address=$(address_host "$address")
fi
if [[ -n $certificate_source || -n $key_source ]]; then
  [[ $tls_mode == files ]] || fail '--cert 与 --key 只能与 --tls files 一起使用。'
  [[ -n $certificate_source && -n $key_source ]] \
    || fail '--tls files 需要同时给出 --cert 与 --key。'
  regular_file "$certificate_source" || fail "证书必须是普通文件：$certificate_source"
  regular_file "$key_source" || fail "私钥必须是普通文件：$key_source"
fi
if [[ -n $bundle_file || -n $sums_file ]]; then
  [[ -n $bundle_file && -n $sums_file ]] \
    || fail '--bundle 与 --sums 必须同时给出（离线安装需要发布归档和它的 SHA256SUMS）。'
  regular_file "$bundle_file" || fail "离线归档必须是普通文件：$bundle_file"
  regular_file "$sums_file" || fail "离线 SHA256SUMS 必须是普通文件：$sums_file"
  [[ -n $version ]] || fail '离线安装（--bundle/--sums）必须用 --version 指定版本。'
fi

# The host gate. A test cannot drop privileges or stop systemd, so the three facts
# are overridable; on a real host the defaults ask the host itself.
require_host() {
  local host_system=${QUI_PANEL_INSTALL_TEST_UNAME:-$(uname -s)}
  local host_euid=${QUI_PANEL_INSTALL_TEST_EUID:-$EUID}
  local init_marker=${QUI_PANEL_INSTALL_TEST_INIT_MARKER:-/run/systemd/system}
  [[ $host_system == Linux ]] || fail "只支持 Linux，当前是 $host_system。"
  [[ $host_euid == 0 ]] || fail '请以 root 身份运行：sudo bash panel-install.sh'
  [[ -d $init_marker ]] \
    || fail '面板需要 systemd：本机没有运行 systemd（Debian/Ubuntu 默认是）。'
}


# Print the raw value of the first top-level "key: value" line of a file, with a
# trailing carriage return removed; the same simple parsing qui.sh and update.sh
# use. Returns 1 when the key is absent.
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

# The install record is what install.sh writes and what qui-update and uninstall.sh
# read. The program alone can be left behind by an uninstall that kept its data, so
# it does not decide which mode this run is in.
installed_panel() {
  [[ -f $manifest && ! -L $manifest ]]
}

# The panel certificate protects the web UI and the gRPC listener; the unit runs
# with ProtectHome and ProtectSystem=strict, so a certificate outside /etc/proxy-panel
# is not readable by the service and would only fail at start-up. Both choices are
# therefore installed to these two fixed paths, and the configuration names them. An
# IPv4 or IPv6 literal becomes an IP: entry, a domain a DNS: one; an IPv6 address is
# written without the brackets a URL needs.
san_for() {
  local host san
  host=$(address_host "$1")
  if [[ $host =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] || is_ipv6_literal "$host"; then
    san="IP:$host"
  else
    san="DNS:$host"
  fi
  printf '%s,DNS:localhost,IP:127.0.0.1' "$san"
}

# Write a self-signed certificate and its key into a directory. RSA 2048 and ten
# years, with the chosen address in the subject alternative names, so the browser
# warning can be accepted once and the certificate does not expire in a year.
generate_self_signed() {
  local destination=$1 host=$2 configuration=$1/openssl.cnf
  cat > "$configuration" <<EOF
[req]
distinguished_name = subject
prompt = no
x509_extensions = extensions
[subject]
CN = $host
[extensions]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = $(san_for "$host")
EOF
  openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 3650 \
    -config "$configuration" -keyout "$destination/server.key" -out "$destination/server.crt" \
    2>/dev/null || return 1
  chmod 0600 -- "$destination/server.key" "$destination/server.crt"
}

# Succeeds when the certificate and the private key belong together, whatever the
# key algorithm: both are asked for their public key and the two are compared.
certificate_pair_matches() {
  local crt=$1 key=$2 crt_material key_material
  crt_material=$(openssl x509 -in "$crt" -noout -pubkey 2>/dev/null) || return 1
  key_material=$(openssl pkey -in "$key" -pubout 2>/dev/null) || return 1
  [[ -n $crt_material && $crt_material == "$key_material" ]]
}

certificate_valid_now() {
  openssl x509 -in "$1" -noout -checkend 0 >/dev/null 2>&1
}

# Print the packaged template with the four lines this script owns replaced:
# http_address, grpc_address, tls.certificate and tls.key. Every other line,
# comments included, is copied unchanged, and line endings are normalized to LF.
# Returns 1 when one of the four keys is missing, so a changed template is refused
# instead of installed with its defaults.
panel_config() {
  local file=$1 http=$2 grpc=$3 crt=$4 key=$5
  local line in_tls=false seen_http=false seen_grpc=false seen_crt=false seen_key=false
  while IFS= read -r line || [[ -n $line ]]; do
    line=${line%$'\r'}
    if [[ $line =~ ^tls[[:space:]]*: ]]; then
      in_tls=true
      printf '%s\n' "$line"
      continue
    fi
    if [[ $line =~ ^http_address[[:space:]]*: ]]; then
      printf 'http_address: %s\n' "$http"
      seen_http=true
      continue
    fi
    if [[ $line =~ ^grpc_address[[:space:]]*: ]]; then
      printf 'grpc_address: %s\n' "$grpc"
      seen_grpc=true
      continue
    fi
    # Any other top-level key ends the tls block.
    if [[ $line =~ ^[^[:space:]#] ]]; then in_tls=false; fi
    if $in_tls && [[ $line =~ ^([[:space:]]+)certificate[[:space:]]*: ]]; then
      printf '%scertificate: %s\n' "${BASH_REMATCH[1]}" "$crt"
      seen_crt=true
      continue
    fi
    if $in_tls && [[ $line =~ ^([[:space:]]+)key[[:space:]]*: ]]; then
      printf '%skey: %s\n' "${BASH_REMATCH[1]}" "$key"
      seen_key=true
      continue
    fi
    printf '%s\n' "$line"
  done < "$file"
  $seen_http && $seen_grpc && $seen_crt && $seen_key
}

# A first install creates the configuration; anything already there is never
# replaced, so a re-run after an uninstall keeps the settings and the database the
# operator already has.
config_first_install() {
  [[ ! -e $config && ! -L $config ]]
}

# The temporary download directory is removed on every exit path; nothing else in
# this script is cleaned up by the trap.
cleanup() { if [[ -n $directory ]]; then rm -rf -- "$directory"; fi; }
trap cleanup EXIT

# Print the default route's IPv4 address, which is the panel's own address on a
# normal single-homed server; asks the host rather than an external service, so the
# installer works without HTTPS egress and does not leak a request.
detect_address() {
  local detected=''
  if command -v ip >/dev/null 2>&1; then
    detected=$(ip -4 route get 1.1.1.1 2>/dev/null \
      | sed -n 's/.*[[:space:]]src[[:space:]]\([0-9][0-9.]*\).*/\1/p' | head -n 1)
  fi
  [[ -n $detected ]] || detected=$(hostname -I 2>/dev/null | awk '{print $1}')
  printf '%s' "$detected"
}

# The port a listener in the effective configuration binds: the port of the named
# top-level key (http_address, grpc_address), or $2 when the configuration does not
# name it — the port this run is about to write, so the summary printed ahead of an
# install shows the chosen one too. The configuration is the reference, so every
# line the script prints matches what the panel actually listens on.
config_port() {
  local key=$1 fallback=${2:-} value=''
  value=$(read_value "$config" "$key" 2>/dev/null || true)
  if [[ $value == *:* && ${value##*:} =~ ^[0-9]+$ ]]; then
    printf '%s\n' "${value##*:}"
    return 0
  fi
  printf '%s\n' "$fallback"
}

# The host part of a URL or of the Agent's control address: the address this run
# chose or the one the host still has, with the brackets an IPv6 literal needs.
display_host() {
  local host=''
  host=${address:-$(detect_address)}
  host=$(address_host "${host:-$(hostname)}")
  if is_ipv6_literal "$host"; then host="[$host]"; fi
  printf '%s\n' "$host"
}

# The URL to open: the HTTPS port the configuration binds (the port of
# http_address) on the address this run chose or the one the host still has. Before
# the configuration exists (the summary printed ahead of the install) it falls back
# to the port this run is about to write, so the default install shows :22550 too. An
# IPv6 address is printed in the brackets a URL needs, https://[v6]:port/.
panel_url() {
  local port='' host=''
  port=$(config_port http_address "${http_port:-443}")
  host=$(display_host)
  if [[ $port == 443 ]]; then printf 'https://%s/\n' "$host"; else printf 'https://%s:%s/\n' "$host" "$port"; fi
}

# Succeeds when something already listens on the TCP port. The check needs ss; when
# it is missing the run continues without it, and the panel reports the bind failure
# itself.
port_in_use() {
  local port=$1
  command -v ss >/dev/null 2>&1 || return 1
  ss -H -ltn 2>/dev/null \
    | awk -v port="$port" '{ address=$4 } address ~ (":" port "$") { found=1 } END { exit found ? 0 : 1 }'
}


# --no-insecure overrides a -k that a curl wrapper or alias may add in front of the
# arguments, so a download is never accepted without a verified certificate.
download() {
  curl --no-insecure -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 5 \
    --output "$2" -- "$1" || fail "下载失败：$1"
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

# Name of the release archive a version ships for an architecture. fetch_release
# unpacks it into $directory/<bundle>, so a caller can name the extracted layout
# without repeating the pattern.
release_bundle() {
  printf 'proxy-control-%s-linux-%s\n' "$1" "$2"
}

# Download the release archive into $directory and verify it against the
# SHA256SUMS published with it. The offline path copies --bundle/--sums under the
# same names, so both paths end in the same digest comparison and neither is
# trusted more than the other. The archive is unpacked only once the digest matches.
fetch_release() {
  local base
  bundle=$(release_bundle "$version" "$architecture")
  base=https://github.com/$repository/releases/download/v$version
  if [[ -n $bundle_file ]]; then
    install -m 0600 -- "$bundle_file" "$directory/$bundle.tar.gz" || fail '无法复制离线归档。'
    install -m 0600 -- "$sums_file" "$directory/SHA256SUMS" || fail '无法复制离线 SHA256SUMS。'
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
}

# Print the version of the latest GitHub release; releases/latest never returns a
# pre-release, which is what a plain install should get.
latest_version() {
  local release='' tag=''
  printf '查询 GitHub 最新发布 ...\n' >&2
  release=$(curl --no-insecure -fsSL --proto '=https' --tlsv1.2 \
    "https://api.github.com/repos/$repository/releases/latest") \
    || fail '无法查询 GitHub 最新发布；请检查网络，或用 --version 指定版本。'
  tag=$(printf '%s\n' "$release" | grep -m1 '"tag_name"' \
    | sed -e 's/.*"tag_name"[[:space:]]*:[[:space:]]*"//' -e 's/".*//') || tag=''
  [[ $tag == v* ]] && is_version "${tag#v}" \
    || fail "GitHub 返回的最新发布标签不是 vX.Y.Z 或 vX.Y.Z-rc.N：$tag"
  printf '%s\n' "${tag#v}"
}


# Ask a question with a default answer. The answer lands in $answer; an empty reply
# keeps the default. With --yes, or when stdin is not a terminal, the default is
# taken without asking, so an unattended run never blocks.
ask() {
  local prompt=$1 fallback=${2:-} reply=''
  answer=$fallback
  if $assume_yes || [[ ! -t 0 ]]; then return 0; fi
  if [[ -n $fallback ]]; then printf '%s [%s]：' "$prompt" "$fallback"; else printf '%s：' "$prompt"; fi
  IFS= read -r reply || reply=''
  [[ -z $reply ]] || answer=$reply
}

# The installed program validates the configuration it is going to read, as the
# service account that reads it; the panel is not started when that fails.
check_configuration() {
  local output=''
  [[ -f $config && ! -L $config ]] || fail "缺少配置文件：$config"
  if ! output=$(runuser -u "$account" -- "$program" --config "$config" --check-config 2>&1); then
    printf '%s\n' "$output" >&2
    fail "配置未通过校验：$config（面板未启动；修正后重跑本脚本，或改好后 systemctl start $service）。"
  fi
  printf '配置校验通过：%s\n' "$config"
}

# Install the certificate and its key to the two fixed paths. An existing pair is
# copied there instead, because the unit cannot read anything under /root
# (ProtectHome=true) and cannot write outside /etc/proxy-panel.
install_tls() {
  local generated=''
  [[ -d $etc/tls ]] || install -d -o "$account" -g "$account" -m 0750 -- "$etc/tls" \
    || fail "无法创建 $etc/tls。"
  if [[ $tls_mode == files ]]; then
    install -o "$account" -g "$account" -m 0600 -- "$certificate_source" "$certificate" \
      || fail "无法安装证书到 $certificate。"
    install -o "$account" -g "$account" -m 0600 -- "$key_source" "$certificate_key" \
      || fail "无法安装私钥到 $certificate_key。"
    printf '已安装已有证书：%s 与 %s（面板服务只能读 /etc/proxy-panel，证书必须放在这里）。\n' \
      "$certificate" "$certificate_key"
  else
    generated=$(mktemp -d) || fail '无法创建证书临时目录。'
    if ! generate_self_signed "$generated" "$address"; then
      rm -rf -- "$generated"
      fail '自签名证书生成失败（openssl）；请安装 openssl，或用 --tls files 指定已有证书。'
    fi
    install -o "$account" -g "$account" -m 0600 -- "$generated/server.crt" "$certificate" \
      || { rm -rf -- "$generated"; fail "无法安装证书到 $certificate。"; }
    install -o "$account" -g "$account" -m 0600 -- "$generated/server.key" "$certificate_key" \
      || { rm -rf -- "$generated"; fail "无法安装私钥到 $certificate_key。"; }
    rm -rf -- "$generated"
    printf '已生成自签名证书：%s（RSA 2048，10 年有效，SAN 含 %s）。\n' "$certificate" "$(san_for "$address")"
  fi
}

# Write the ports and the certificate paths of the packaged template over the
# configuration install.sh has just created. Only a first install calls this (see
# configure_panel): the temporary file is created in the target directory, gets the
# owner, group and mode of the file it replaces, and is renamed into place, so the
# configuration is never visible half-written and is never left unreadable for the
# service account.
install_configuration() {
  local template=$1 temporary='' owner='' group='' mode='0600'
  regular_file "$template" || fail "发布归档里缺少配置模板：$template"
  if [[ -f $config && ! -L $config ]]; then
    owner=$(stat -c '%u' -- "$config") || fail "无法读取 $config 的属主。"
    group=$(stat -c '%g' -- "$config") || fail "无法读取 $config 的属组。"
    mode=$(stat -c '%a' -- "$config") || fail "无法读取 $config 的权限。"
  else
    owner=$(id -u "$account" 2>/dev/null || true)
    group=$(id -g "$account" 2>/dev/null || true)
  fi
  temporary=$(mktemp "$etc/.config.XXXXXXXX") || fail '无法创建临时配置文件。'
  if ! panel_config "$template" "0.0.0.0:$http_port" "0.0.0.0:$grpc_port" \
    "$certificate" "$certificate_key" > "$temporary"; then
    rm -f -- "$temporary"
    fail "配置模板缺少 http_address、grpc_address 或 tls 段，拒绝写入：$template"
  fi
  if [[ -n $owner ]]; then
    chown "$owner:$group" -- "$temporary" || { rm -f -- "$temporary"; fail '无法设置配置文件属主。'; }
  fi
  chmod "$mode" -- "$temporary" || { rm -f -- "$temporary"; fail '无法设置配置文件权限。'; }
  mv -T -- "$temporary" "$config" || fail "无法写入 $config。"
  printf '已写入 %s（http_address 0.0.0.0:%s，grpc_address 0.0.0.0:%s）。\n' \
    "$config" "$http_port" "$grpc_port"
}

# The configuration step of an install: the archive's install.sh writes the packaged
# template when /etc/proxy-panel/config.yaml does not exist yet, so a first install
# rewrites that file with the ports and certificate paths of this run, while any
# other run leaves the configuration, the certificate and the database the operator
# already has exactly as they are. Whether this run is the first install is decided
# before install.sh runs and passed in, because afterwards the file install.sh just
# created looks like a pre-existing configuration.
configure_panel() {
  local first_install=$1 template=$2
  if ! $first_install; then
    printf '\n配置、证书与数据库保持不变（重跑不覆盖）。\n'
    return 0
  fi
  install_tls
  install_configuration "$template"
}

# The unit restarts a crashed panel after five seconds, so a crash loop looks active
# at every poll but changes the main PID. The panel counts as up only when that PID
# is unchanged ten seconds later, the same check qui-update makes.
service_healthy() {
  local first='' pid='' waited=0
  while ((waited < 15)); do
    if systemctl is-active --quiet "$service"; then
      pid=$(systemctl show -p MainPID --value "$service" 2>/dev/null || true)
      if [[ -n $pid && $pid != 0 ]]; then first=$pid; break; fi
    fi
    sleep 1
    waited=$((waited + 1))
  done
  [[ -n $first ]] || return 1
  for ((waited = 0; waited < 10; waited += 1)); do
    sleep 1
    systemctl is-active --quiet "$service" || return 1
    pid=$(systemctl show -p MainPID --value "$service" 2>/dev/null || true)
    [[ $pid == "$first" ]] || return 1
  done
  return 0
}


# Enabled for boot and started now; a panel that does not stay up is reported, not
# left to the operator to notice.
start_panel() {
  systemctl enable --now "$service" || fail "无法启用并启动 $service"
  if service_healthy; then
    printf '%s 已开机自启并稳定运行（10 秒内没有重启）。\n' "$service"
  else
    fail "服务没有稳定运行；日志：journalctl -u $service -n 50（service 带 Restart=on-failure，反复重启通常是配置或端口问题）。"
  fi
}

# Offer to open the two ports when a firewall is already running. A firewall that is
# off is never enabled here, and without one the run only prints the reminder: the
# cloud security group is outside this host and outside this script.
open_firewall() {
  local answer_value='' port http='' grpc=''
  # The ports the configuration actually binds: a re-run keeps the configuration it
  # already has, so opening the ports chosen for this run would open the wrong ones.
  http=$(config_port http_address "$http_port")
  grpc=$(config_port grpc_address "$grpc_port")
  local ports=("$http/tcp" "$grpc/tcp")
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi '^Status: active'; then
    ask "检测到 ufw 已启用，是否放行 TCP $http 与 $grpc" y
    answer_value=$answer
    case ${answer_value,,} in
      y|yes)
        for port in "$http" "$grpc"; do
          ufw allow "$port/tcp" >/dev/null || printf 'ufw allow %s/tcp 失败，请手动放行。\n' "$port"
        done
        printf 'ufw 已放行 TCP %s 与 %s。\n' "$http" "$grpc";;
      *) printf '未修改 ufw；请手动放行 TCP %s 与 %s。\n' "$http" "$grpc";;
    esac
  elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
    ask "检测到 firewalld 已启用，是否放行 TCP $http 与 $grpc" y
    answer_value=$answer
    case ${answer_value,,} in
      y|yes)
        for port in "${ports[@]}"; do firewall-cmd --permanent --add-port="$port" >/dev/null; done
        firewall-cmd --reload >/dev/null
        printf 'firewalld 已放行 TCP %s 与 %s。\n' "$http" "$grpc";;
      *) printf '未修改 firewalld；请手动放行 TCP %s 与 %s。\n' "$http" "$grpc";;
    esac
  else
    printf '未检测到启用中的 ufw 或 firewalld（不会替你打开防火墙）：请确认云安全组或上游防火墙已放行 TCP %s 与 %s。\n' \
      "$http" "$grpc"
  fi
}

# Everything the operator needs after the install: the URL, the one-time token, the
# certificate fingerprint the register dialog compares against, and what to do next.
# The token only exists while there is no Owner yet, so a re-run against an
# initialized database prints nothing and says why.
report() {
  local token='' digest='' waited=0 keepalive=''
  printf '\n面板已安装并启动：\n'
  printf '  访问地址：%s\n' "$(panel_url)"
  printf '  程序版本：%s\n' "$("$program" --version 2>/dev/null || printf '未知')"
  printf '  Agent gRPC：%s:%s（节点注册时用这个端口）\n' "$(display_host)" "$(config_port grpc_address "${grpc_port:-18880}")"
  if [[ $tls_mode == files ]]; then
    printf '  TLS 证书：%s（已有证书，原样安装）\n' "$certificate"
  else
    printf '  TLS 证书：%s（自签名，10 年有效）\n' "$certificate"
  fi
  if digest=$(sha256sum -- "$certificate" 2>/dev/null); then
    printf '  证书文件 SHA-256：%s\n' "${digest%% *}"
    if [[ $tls_mode == files ]]; then
      printf '  （含证书链时该值覆盖整个文件；节点注册对话框显示的是链顶证书的指纹，两者不一致时以面板显示的为准。）\n'
    else
      printf '  （节点注册对话框显示的就是该指纹：自签名证书靠它建立信任，两者不一致时以面板显示的为准。）\n'
    fi
  fi
  while ((waited < 15)); do
    if [[ -f $data/setup-token && ! -L $data/setup-token && -s $data/setup-token ]]; then
      token=$(< "$data/setup-token")
      break
    fi
    sleep 1
    waited=$((waited + 1))
  done
  if [[ -n $token ]]; then
    printf '\n首次登录的一次性安装令牌：%s\n' "$token"
    printf '  打开 %s，在初始化页面粘贴该令牌，设置 Owner 用户名、密码、平台名称与时区；\n' "$(panel_url)"
    printf '  令牌只在还没有 Owner 时生成，写在 %s（0600）并只在日志里以 WARN 打印一次，初始化成功后立即失效；\n' \
      "$data/setup-token"
    printf '  每个新的令牌都覆盖旧文件：面板在还没有 Owner 时每次重启都重新生成，以文件或最近一条日志为准。\n'
  else
    printf '\n没有找到一次性安装令牌文件（%s/setup-token）：这台主机的数据库通常已经创建过 Owner。\n' "$data"
    printf '  全新安装时可用 journalctl -u %s -n 50 查看 “first-run setup token” 一行。\n' "$service"
  fi
  keepalive=$(systemctl cat "$service" 2>/dev/null | grep -c '^Restart=on-failure' || true)
  if [[ ${keepalive:-0} -ge 1 ]]; then
    printf '\n保活：%s 已 enable（开机自启），单元带 Restart=on-failure、RestartSec=5（崩溃后 5 秒重启）；\n' "$service"
    printf '  本脚本不安装定时任务或看门狗守护进程，systemd 的重启与开机自启就是保活方式。\n'
  else
    printf '\n警告：没有在 %s 的单元里找到 Restart=on-failure；请确认 %s 的内容，否则崩溃后不会自动重启。\n' \
      "$service" "/etc/systemd/system/$service"
  fi
  printf '\n接下来：\n'
  printf '  1. 打开上面的地址创建 Owner（如果还没有），在系统设置里确认时区。\n'
  printf '  2. 服务器 → 新建节点，把注册对话框里的一键命令粘贴到节点服务器执行。\n'
  printf '  3. 日常维护在面板主机输入 qui 打开终端菜单（改密码、改端口、重启、更新）。\n'
  printf '  4. 以后升级：重跑本脚本选 1（有离线归档时也可用 --update），或执行 qui-update --component panel [--version X]。\n'
  printf '  5. 备份、证书与排错见 docs/operations.md；安装细节见 docs/deployment.md。\n'
}


# The interactive install: ask for what was not given as a flag, summarise, confirm,
# then download, verify, install, configure, validate and start.
install_panel() {
  local detected='' first_install=false template='' binary='' digest='' file
  local marker=$registry/panel.removed reinstall=false arguments=()
  # install.sh writes the configuration from the packaged template as soon as the
  # file does not exist, so "first install" has to be decided before it runs: after
  # that the file is always there and a check would keep the template's ports.
  if config_first_install; then first_install=true; fi
  # uninstall.sh leaves its verified record behind; install.sh refuses to overwrite
  # such a host silently and asks for --reinstall, which re-validates that record.
  if [[ -e $marker || -L $marker ]]; then
    reinstall=true
    printf '\n上次卸载留下了记录 %s：本次按重新安装处理（install.sh --reinstall），\n' "$marker"
    printf '配置、证书与数据库照旧保留，不会被覆盖。\n'
  fi

  if [[ -z $version ]]; then version=$(latest_version); fi
  case $(uname -m) in
    x86_64) architecture=amd64;;
    aarch64|arm64) architecture=arm64;;
    *) fail "不支持的架构：$(uname -m)";;
  esac

  if [[ -z $http_port ]]; then ask '管理后台与 API 的 HTTPS 端口' 22550; http_port=$answer; fi
  valid_port "$http_port" || fail "HTTPS 端口必须在 1-65535 之间：$http_port"
  if [[ -z $grpc_port ]]; then ask '节点 Agent 的 gRPC 端口' 18880; grpc_port=$answer; fi
  valid_port "$grpc_port" || fail "gRPC 端口必须在 1-65535 之间：$grpc_port"
  [[ $http_port != "$grpc_port" ]] || fail 'HTTPS 端口与 gRPC 端口不能相同。'
  if [[ -z $address ]]; then
    detected=$(detect_address)
    ask '面板对外地址（域名、公网 IPv4 或 IPv6，用于访问链接与证书）' "${detected:-panel.example.com}"
    address=$answer
  fi
  address_allowed "$address" || fail "地址只能是域名、IPv4 或 IPv6：$address"
  address=$(address_host "$address")
  if [[ -z $tls_mode ]]; then
    printf '\nTLS 证书：\n'
    printf '  1) 自动生成自签名证书（推荐：openssl 现场生成，RSA 2048，10 年有效）\n'
    printf '  2) 使用已有证书文件（证书与私钥必须匹配，会被复制到 %s）\n' "$etc/tls"
    ask '请选择' 1
    case $answer in
      1) tls_mode=self-signed;;
      2) tls_mode=files;;
      *) fail "只能选择 1 或 2（收到 $answer）。";;
    esac
  fi
  if [[ $tls_mode == files ]]; then
    if [[ -z $certificate_source ]]; then ask '证书文件路径（可含中间证书链）' ''; certificate_source=$answer; fi
    if [[ -z $key_source ]]; then ask '私钥文件路径' ''; key_source=$answer; fi
    for file in "$certificate_source" "$key_source"; do
      regular_file "$file" || fail "证书与私钥必须是普通文件：$file"
    done
    certificate_pair_matches "$certificate_source" "$key_source" \
      || fail '证书与私钥不匹配（openssl 校验失败），请确认两个文件属于同一张证书。'
    certificate_valid_now "$certificate_source" || fail '证书已过期或无法解析，请换一张有效证书。'
  elif ! command -v openssl >/dev/null 2>&1; then
    fail '缺少 openssl，无法生成自签名证书：请安装 openssl，或用 --tls files 指定已有证书。'
  fi

  # The ports that will actually be bound: on a first install the chosen ones (there is
  # no configuration yet), otherwise the ones the configuration already names, because
  # a re-run keeps that configuration untouched.
  local effective_http='' effective_grpc=''
  effective_http=$(config_port http_address "$http_port")
  effective_grpc=$(config_port grpc_address "$grpc_port")
  if port_in_use "$effective_http"; then
    fail "HTTPS 端口 $effective_http 已被占用（ss -ltnp 查看）；用 --http-port 换一个端口，或先停掉占用它的服务。"
  fi
  if port_in_use "$effective_grpc"; then
    fail "gRPC 端口 $effective_grpc 已被占用（ss -ltnp 查看）；用 --grpc-port 换一个端口。"
  fi

  printf '\n即将安装面板：\n'
  printf '  版本：%s（%s）\n' "$version" "$architecture"
  printf '  Web/API：0.0.0.0:%s（HTTPS）\n' "$effective_http"
  printf '  Agent gRPC：0.0.0.0:%s\n' "$effective_grpc"
  printf '  访问地址：%s\n' "$(panel_url)"
  if [[ $tls_mode == files ]]; then
    printf '  TLS：已有证书 %s\n' "$certificate_source"
  else
    printf '  TLS：自签名证书（自动生成）\n'
  fi
  printf '  程序与服务：/usr/local/bin/proxy-panel、/etc/systemd/system/%s\n' "$service"
  printf '  配置与数据：%s、%s\n' "$config" "$data"
  if ! $first_install; then
    printf '  注意：本机已有配置文件，示例配置、证书与数据库不会被覆盖。\n'
  fi
  if ! $assume_yes; then
    if [[ ! -t 0 ]]; then fail '需要终端确认；无人值守安装请加 --yes。'; fi
    printf '\n确认安装？输入 y 继续：'
    if ! IFS= read -r answer || [[ $answer != y && $answer != Y ]]; then
      printf '已取消，未做任何修改。\n'
      exit 0
    fi
  fi

  directory=$(mktemp -d) || fail '无法创建临时目录。'
  fetch_release
  for file in bin/proxy-panel deploy/linux/install.sh deploy/examples/panel.yaml; do
    regular_file "$source_dir/$file" || fail "归档里缺少 $file。"
  done
  template=$source_dir/deploy/examples/panel.yaml
  binary=$source_dir/bin/proxy-panel
  digest=$(sha256sum -- "$binary"); digest=${digest%% *}
  printf '\n安装程序、服务账号、目录与 systemd 单元 ...\n'
  arguments=(--panel --binary "$binary" --sha256 "$digest")
  if $reinstall; then arguments+=(--reinstall); fi
  "$source_dir/deploy/linux/install.sh" "${arguments[@]}" \
    || fail '安装失败（原因见上面 install.sh 的输出）；修正后可以重跑本脚本：install.sh 原地升级，数据与配置保留。'

  configure_panel "$first_install" "$template"
  check_configuration
  start_panel
  open_firewall
  report
}


# The menu on a host that already has the panel. Keep it small: updating goes
# through qui-update, which owns the backup, snapshot, health check and rollback,
# and everything else the operator needs is in the qui menu.
show_status() {
  local state='' version_text='' on_boot=''
  state=$(systemctl is-active "$service" 2>/dev/null || true)
  version_text=$("$program" --version 2>/dev/null || printf '未知')
  on_boot=$(systemctl is-enabled "$service" 2>/dev/null || printf '未知')
  printf '\n服务状态：%s（开机自启：%s）\n' "${state:-未知}" "$on_boot"
  printf '程序版本：%s\n' "$version_text"
  printf '访问地址：%s\n' "$(panel_url)"
  printf '配置文件：%s\n' "$config"
  printf '日志：journalctl -u %s -f\n' "$service"
}

restart_panel() {
  if systemctl restart "$service"; then
    sleep 1
    printf '%s 已重启；状态：%s\n' "$service" "$(systemctl is-active "$service" 2>/dev/null || true)"
  else
    printf '重启 %s 失败；日志：journalctl -u %s -n 50\n' "$service" "$service"
  fi
}

# Updating is qui-update's job: it takes the database backup, snapshots the files,
# checks the database with the target version, waits for a stable service and
# rolls back when any of that fails. This script only picks the version — from
# --version, or from the offline archive the run was started with, so a host without
# a route to GitHub is updated from the same verified file an install would use — and
# asks for it only when the run did not say.
update_panel() {
  local target='' update_arguments=(--component panel)
  [[ -x $updater ]] || fail "缺少更新程序：$updater（重新执行一次安装流程即可装上）。"
  if [[ -n $bundle_file ]]; then
    target=$version
    printf '使用本次运行给出的离线归档 %s（版本 %s）。\n' "${bundle_file##*/}" "$target"
    update_arguments+=(--version "$target" --bundle "$bundle_file" --sums "$sums_file")
  elif [[ -n $version ]]; then
    target=$version
    update_arguments+=(--version "$target")
  else
    ask '目标版本（留空为最新发布）' ''
    target=$answer
    [[ -z $target ]] || is_version "$target" || fail "版本号必须是 X.Y.Z 或 X.Y.Z-rc.N：$target"
    if [[ -n $target ]]; then update_arguments+=(--version "$target"); fi
  fi
  if $assume_yes; then update_arguments+=(--yes); fi
  printf '运行 qui-update %s ...\n' "${update_arguments[*]}"
  "$updater" "${update_arguments[@]}"
}

# Print the uninstall.sh that sits next to this script, but only when the run came
# from an extracted release archive or a checkout: the script's own directory has to
# end in deploy/linux and hold install.sh, uninstall.sh and service.sh as regular
# files. The documented one-liner leaves panel-install.sh in an arbitrary directory
# such as /root, where an unrelated uninstall.sh would then be run as root, so that
# copy is never used. The first argument is the path of this script; it defaults to
# this script itself.
sibling_uninstall() {
  local source=${1:-${BASH_SOURCE[0]}} here='' file=''
  here=$(cd -- "$(dirname -- "$source")" 2>/dev/null && pwd -P) || return 1
  [[ ${here##*/} == linux && ${here%/*} == */deploy ]] || return 1
  for file in install.sh uninstall.sh service.sh; do
    regular_file "$here/$file" || return 1
  done
  printf '%s\n' "$here/uninstall.sh"
}

# Uninstall uses the packaged uninstall.sh: a copy next to this script when the run
# came from an extracted archive or the repository (see sibling_uninstall), otherwise
# the one inside the archive of the installed version, downloaded and verified the
# same way an install is. Nothing is fetched from a moving branch. Data is kept
# unless the operator types the second confirmation. Which of the two paths is
# taken is announced before the confirmation, because the download path fails on a
# version that was never published on GitHub.
uninstall_panel() {
  local confirmation='' script='' installed=''
  # Only a sibling deploy/linux layout (an extracted release archive, or a checkout)
  # provides uninstall.sh; a plain download directory such as /root never does, and
  # uninstall.sh then comes out of the archive of the installed version.
  script=$(sibling_uninstall || true)
  printf '\n卸载会停止并删除面板程序、服务单元、终端菜单与更新程序；\n'
  printf '数据库、备份、证书、配置与服务账号默认保留。\n'
  if [[ -n $script ]]; then
    printf '卸载脚本：本目录 release 布局里的 %s。\n' "$script"
  else
    printf '本目录没有 release 布局的 uninstall.sh：卸载会从 GitHub 下载已安装版本的发布归档，\n'
    printf '用归档里的 deploy/linux/uninstall.sh 执行（版本没在 GitHub 发布时这一步会失败）。\n'
  fi
  printf '确认卸载请输入 uninstall（其他任意输入取消）：'
  IFS= read -r confirmation || confirmation=''
  [[ $confirmation == uninstall ]] || { printf '已取消。\n'; return 0; }

  if [[ -z $script ]]; then
    installed=$("$program" --version 2>/dev/null || true)
    installed=${installed#proxy-panel }
    is_version "$installed" \
      || fail '无法确定已安装版本，取不到 uninstall.sh；请从已审核的发布归档里解包后运行它的 deploy/linux/uninstall.sh panel。'
    version=$installed
    case $(uname -m) in
      x86_64) architecture=amd64;;
      aarch64|arm64) architecture=arm64;;
      *) fail "不支持的架构：$(uname -m)";;
    esac
    printf '从已安装版本 %s 的发布归档取出 uninstall.sh ...\n' "$version"
    directory=$(mktemp -d) || fail '无法创建临时目录。'
    # The download runs in a subshell, so its failure can be reported with the route
    # that works offline instead of ending the script inside fail().
    if ! ( fetch_release ); then
      printf '\n取不到已安装版本 %s 的发布归档（原因见上）。\n' "$version" >&2
      printf '请从已审核的发布归档解包，再运行其中自带的卸载脚本：\n' >&2
      printf '  tar -xzf proxy-control-%s-linux-%s.tar.gz && bash proxy-control-%s-linux-%s/deploy/linux/uninstall.sh panel\n' \
        "$version" "$architecture" "$version" "$architecture" >&2
      return 1
    fi
    script=$directory/$(release_bundle "$version" "$architecture")/deploy/linux/uninstall.sh
    regular_file "$script" || fail '发布归档里缺少 deploy/linux/uninstall.sh。'
  fi
  "$script" panel || fail '卸载失败，见上面的输出。'
  rm -rf -- "$directory"
  directory=''

  printf '\n数据默认保留：%s（数据库、备份、节点 CA）与 %s（配置、证书）。\n' "$data" "$etc"
  ask '是否删除这两个目录（删除后无法恢复）' n
  case ${answer,,} in
    y|yes)
      printf '再次确认：删除 %s 与 %s 后无法恢复。输入 delete 继续：' "$data" "$etc"
      IFS= read -r confirmation || confirmation=''
      if [[ $confirmation != delete ]]; then printf '已取消，数据与配置保留。\n'; return 0; fi
      rm -rf -- "$data" "$etc" || fail '删除数据或配置目录失败。'
      printf '已删除 %s 与 %s；服务账号 %s 保留（如需删除：userdel %s）。\n' "$data" "$etc" "$account" "$account"
      ;;
    *) printf '数据与配置已保留。\n';;
  esac
}

print_menu() {
  local state='' version_text=''
  state=$(systemctl is-active "$service" 2>/dev/null || true)
  version_text=$("$program" --version 2>/dev/null || printf '未知')
  printf '\n代理控制平台 面板（已安装）\n'
  printf '  服务状态：%s　程序版本：%s\n' "${state:-未知}" "$version_text"
  printf '  访问地址：%s\n\n' "$(panel_url)"
  printf '1. 更新到最新版本或指定版本\n'
  printf '2. 查看状态\n'
  printf '3. 重启\n'
  printf '4. 卸载\n'
  printf '0. 退出\n'
  printf '（修改管理员密码或端口请用 qui 菜单。）\n\n'
}

# One menu step, run in a subshell so a failure cannot end the script: set -e stays
# in force inside the step, its own diagnostics are printed, and this prints the exit
# code and returns to the menu. Nothing a step sets survives into the next choice.
menu_action() {
  local status=0
  ( "$@" ) || status=$?
  if ((status != 0)); then
    printf '\n上一步失败（退出码 %s，原因见上面的输出）；已回到菜单。\n' "$status"
  fi
  return 0
}

menu() {
  local choice=''
  while true; do
    print_menu
    printf '请选择：'
    if ! IFS= read -r choice; then printf '\n'; return 0; fi
    case $choice in
      1) menu_action update_panel;;
      2) menu_action show_status;;
      3) menu_action restart_panel;;
      4) menu_action uninstall_panel;;
      0) printf '退出。\n'; return 0;;
      *) printf '无效选择：%s\n' "$choice"; continue;;
    esac
    printf '\n按 Enter 返回菜单。'
    IFS= read -r _ || true
    printf '\n'
  done
}

main() {
  local tool
  require_host
  for tool in curl sha256sum install mktemp stat chown chmod sed awk grep rm tar uname \
    systemctl runuser openssl; do
    command -v "$tool" >/dev/null 2>&1 || fail "缺少命令：$tool"
  done
  if installed_panel; then
    # --update is the update of the menu without the menu; --yes on an installed host
    # must never look like a successful install, so it reports what is installed and
    # leaves the configuration, the certificate and the database untouched.
    if $update_only; then update_panel; return 0; fi
    if $assume_yes; then
      show_status
      printf '\n本次 --yes 没有改动任何东西：不带 --yes 运行本脚本可进入菜单（更新、状态、重启、卸载），\n'
      printf '直接更新用 --update，或执行 qui-update --component panel [--version X]。\n'
      return 0
    fi
    menu
  else
    if $update_only; then
      fail "本机没有安装面板（缺少 $manifest）：--update 只用于已安装的主机；首次安装请不带 --update 运行本脚本。"
    fi
    install_panel
  fi
}

main "$@"

