#!/usr/bin/env bash
# Regression test for deploy/linux/node-install.sh, the one-command node installer.
#
# Run it on any host with bash and coreutils (CI and a developer machine):
#
#   bash deploy/linux/tests/node-install-test.sh
#
# Nothing here installs an Agent, starts a service or contacts a panel. The
# argument checks stop before the first download and run as a child process; the
# functions that talk to the panel are sourced out of the installer with extract and
# run against a curl stub below $TOP that answers with the exit code and HTTP status
# each scenario needs. The scenarios are the failures an operator actually meets:
# the same port given for both panel listeners, a CA download that reaches gRPC or
# the wrong web server, and the certificate the Agent will see on the gRPC port when
# the panel sits behind a reverse proxy or Cloudflare.
set -euo pipefail

missing=''
for tool in sed grep sha256sum mktemp cp cmp mkdir rm dirname; do
  command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
done
if [[ -n $missing ]]; then
  printf 'node-install-test: missing commands:%s.\n' "$missing"
  exit 1
fi

script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
repo=$(cd -- "$script_dir/../../.." && pwd -P)
installer=$repo/deploy/linux/node-install.sh
[[ -f $installer ]] || { printf 'node-install-test: missing %s\n' "$installer" >&2; exit 1; }

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

# Print one function of the script under test, from its definition to the closing
# brace in the first column. The script defines every helper that way, so the
# scenarios below run the real code instead of a copy of it.
extract() { sed -n "/^$1() {/,/^}/p" "$installer"; }

TOP=$(mktemp -d "${TMPDIR:-/tmp}/qui-node-install-test.XXXXXXXX") || exit 1
cleanup_top() {
  if [[ ${QUI_NODE_INSTALL_TEST_KEEP:-0} == 1 ]]; then
    printf 'node-install-test: temporary directory kept: %s\n' "$TOP"
    return 0
  fi
  rm -rf -- "$TOP"
}
trap cleanup_top EXIT

# --- Argument checks --------------------------------------------------------------
# Only the argument checks run the installer itself: they stop long before the host
# gate, so this works on any machine. refuse keeps the refusal text out of the report.
refuse() { ( "$@" ) >/dev/null 2>&1; }
token=AAAAAAAAAAAAAAAAAAAAAAAA
zeros=$(printf '0%.0s' {1..64})

# The panel's two listeners cannot share one port. The command below is the one that
# reached the gRPC port and failed at the certificate download instead.
same_port() {
  bash "$installer" --version 1.4.1 --panel panel.example.com --http-port 18880 \
    --grpc-port 18880 --ca-sha256 "$zeros" --token "$token" --core xray 2>&1
}
check 'identical --http-port and --grpc-port refused' 1 refuse same_port
case $(same_port || true) in
  *'不能是同一个端口'*'--http-port'*'--grpc-port'*)
    check 'the refusal names both ports' 0 true;;
  *) check 'the refusal names both ports' 1 true;;
esac
# The same value spelled with a leading zero is still the same port.
same_port_padded() {
  bash "$installer" --version 1.4.1 --panel panel.example.com --http-port 0043 \
    --grpc-port 43 --ca-sha256 "$zeros" --token "$token" --core xray 2>&1
}
check 'a leading zero does not hide the same port' 1 refuse same_port_padded
# Two different ports pass this check: the run continues into the host gate here,
# which refuses a host that is not Linux root or has no init system, and either way
# the message is no longer the port one.
different_ports() {
  bash "$installer" --version 1.4.1 --panel panel.example.com --http-port 22550 \
    --grpc-port 18880 --ca-sha256 "$zeros" --token "$token" --core xray 2>&1
}
case $(different_ports || true) in
  *'不能是同一个端口'*) check 'two different ports pass the port check' 1 true;;
  *) check 'two different ports pass the port check' 0 true;;
esac
# The gRPC address may differ from the panel address (a panel whose Web side is a
# CDN or reverse proxy); it has to pass the same host check and is otherwise used
# verbatim as control_address and server_name.
bad_grpc_host() {
  bash "$installer" --version 1.4.1 --panel panel.example.com --grpc-host https://grpc.example.com \
    --ca-sha256 "$zeros" --token "$token" --core xray 2>&1
}
check 'a gRPC address with a scheme is refused' 1 refuse bad_grpc_host
case $(bad_grpc_host || true) in
  *'Agent gRPC 地址只能是域名或 IPv4'*) check 'the refusal names --grpc-host' 0 true;;
  *) check 'the refusal names --grpc-host' 1 true;;
esac
port_in_grpc_host() {
  bash "$installer" --version 1.4.1 --panel panel.example.com --grpc-host grpc.example.com:443 \
    --ca-sha256 "$zeros" --token "$token" --core xray 2>&1
}
check 'a gRPC address with a port is refused' 1 refuse port_in_grpc_host
different_grpc_host() {
  bash "$installer" --version 1.4.1 --panel panel.example.com --grpc-host grpc.example.com \
    --ca-sha256 "$zeros" --token "$token" --core xray 2>&1
}
case $(different_grpc_host || true) in
  *'Agent gRPC 地址只能是域名或 IPv4'*) check 'a valid gRPC host passes the host check' 1 true;;
  *) check 'a valid gRPC host passes the host check' 0 true;;
esac

# --web-upgrade takes no value and is accepted by argument parsing: with a bad port
# pair the refusal is the port one, not "unknown argument"; a value after it is read
# as the next (unknown) argument.
web_upgrade_flag() {
  bash "$installer" --version 1.4.1 --panel panel.example.com --http-port 18880 \
    --grpc-port 18880 --ca-sha256 "$zeros" --token "$token" --core xray --web-upgrade 2>&1
}
case $(web_upgrade_flag || true) in
  *'未知参数'*) check '--web-upgrade is accepted by argument parsing' 1 true;;
  *'不能是同一个端口'*) check '--web-upgrade is accepted by argument parsing' 0 true;;
  *) check '--web-upgrade is accepted by argument parsing' 1 true;;
esac
web_upgrade_with_value() {
  bash "$installer" --version 1.4.1 --panel panel.example.com --web-upgrade yes 2>&1
}
case $(web_upgrade_with_value || true) in
  *'未知参数：yes'*) check '--web-upgrade takes no value' 0 true;;
  *) check '--web-upgrade takes no value' 1 true;;
esac
# The enabling call needs the whole installer to run, so its gating is asserted on
# the source: the only call sits inside a condition on the flag variable.
gated_enable() {
  [[ $(grep -c '^ *agent_enable_web_upgrade' "$installer") == 1 ]] \
    && grep -B1 '^ *agent_enable_web_upgrade' "$installer" | grep -q 'if \[\[ \$web_upgrade == true \]\]; then'
}
check 'agent_enable_web_upgrade is gated on --web-upgrade' 0 gated_enable

# --- curl and install stubs -------------------------------------------------------
# Every network call of the functions under test goes through $TOP/bin/curl, and the
# trust step installs its anchor with install(1); both are stubbed here. A scenario
# sets STUB_CURL_STATUS (the %{http_code} the stub prints), STUB_CURL_EXIT (its exit
# code), STUB_CURL_VERIFIED_EXIT (the exit code for a request carrying --cacert,
# which is how the public-CA branch is separated from the pinned one),
# STUB_CURL_BODY (the file written to --output) and STUB_CURL_FEATURES (the -V list).
mkdir -p -- "$TOP/bin"
cat > "$TOP/bin/curl" <<'STUB'
#!/usr/bin/env bash
# curl stub: no network, chosen answers.
if [[ ${1-} == -V ]]; then
  printf 'curl 8.9.0 (stub)\nProtocols: https\nFeatures: %s\n' "${STUB_CURL_FEATURES-HTTP2}"
  exit 0
fi
output=''
write_out=''
seen_cacert=false
while (($#)); do
  case $1 in
    --output|-o) output=${2-}; shift 2 ;;
    --write-out|-w) write_out=${2-}; shift 2 ;;
    --cacert) seen_cacert=true; shift 2 ;;
    *) shift ;;
  esac
done
if [[ -n $output && $output != /dev/null ]]; then
  if [[ -n ${STUB_CURL_BODY-} ]]; then
    cp -- "$STUB_CURL_BODY" "$output"
  else
    : > "$output"
  fi
fi
if [[ -n $write_out ]]; then printf '%s' "${STUB_CURL_STATUS-000}"; fi
code=${STUB_CURL_EXIT-0}
if $seen_cacert && [[ -n ${STUB_CURL_VERIFIED_EXIT-} ]]; then code=$STUB_CURL_VERIFIED_EXIT; fi
exit "$code"
STUB
cat > "$TOP/bin/install" <<'STUB'
#!/usr/bin/env bash
# install(1) stub: copies the source operand to the destination, so a scenario can
# assert which file became the Agent's trust anchor. -o/-g/-m are ignored.
if (($# >= 2)); then
  destination=${!#}
  source=${@: -2:1}
  mkdir -p -- "$(dirname -- "$destination")"
  cp -- "$source" "$destination"
fi
STUB
cat > "$TOP/bin/openssl" <<'STUB'
#!/usr/bin/env bash
# openssl(1) stub: the fixtures are not real certificates, so the answer to
# "does this file chain to the system bundle" is chosen by the scenario.
exit "${STUB_OPENSSL_EXIT-0}"
STUB
chmod 0755 -- "$TOP/bin/curl" "$TOP/bin/install" "$TOP/bin/openssl"
export PATH=$TOP/bin:$PATH

# --- Certificate download ---------------------------------------------------------
eval "$(extract ca_download_diagnosis)"
eval "$(extract panel_ca_is_public)"
eval "$(extract install_panel_ca)"

panel=panel.example.com
http_port=22550
authority=panel.example.com:22550
ca_url=https://$authority/install/ca.pem
panel_cert=$TOP/panel-cert.pem
system_bundle_file=$TOP/system-bundle.crt
printf -- '-----BEGIN CERTIFICATE-----\nstub panel certificate\n-----END CERTIFICATE-----\n' > "$panel_cert"
printf -- '-----BEGIN CERTIFICATE-----\nstub public CA bundle\n-----END CERTIFICATE-----\n' > "$system_bundle_file"
digest_of() { local digest; digest=$(sha256sum -- "$1"); printf '%s' "${digest%% *}"; }
panel_digest=$(digest_of "$panel_cert")
wrong_digest=$(printf '0%.0s' {1..64})

# stub_ca <http status> <exit code> [<exit code with --cacert>] [<body file>]
stub_ca() {
  export STUB_CURL_STATUS=$1 STUB_CURL_EXIT=$2
  export STUB_CURL_VERIFIED_EXIT=${3:-$2}
  export STUB_CURL_BODY=${4-}
  export STUB_CURL_FEATURES=HTTP2
}

# Run install_panel_ca over the fixtures: $1 is the pinned digest, $2 the system CA
# bundle of this host (empty: none), which decides whether the public-CA branch is
# reachable at all.
install_ca() (
  ca_sha256=$1
  system_bundle=$2
  fail() { printf '安装被拒绝：%s\n' "$*" >&2; exit 1; }
  retry_note='本机尚未登记：注册码未过期时可以原样重跑本脚本。'
  work=$TOP/ca-work
  rm -rf -- "$work"
  mkdir -p -- "$work/tls"
  fetched=$work/panel-ca.pem
  ca_url=https://$authority/install/ca.pem
  ca_file=$work/tls/ca.crt
  install_panel_ca
)

# A public certificate that is not this panel's (another panel, or a proxy answering
# with its own certificate) is caught by the digest, which the public branch now
# checks as well.
stub_ca 200 0 0 "$panel_cert"
wrong_panel() { install_ca "$wrong_digest" "$system_bundle_file"; }
check 'public-CA download with a wrong digest refused' 1 wrong_panel
check 'the refusal says the certificate is not this panel' 0 \
  grep -q '不是本面板的证书' <<<"$(wrong_panel 2>&1 || true)"
stub_ca 200 0 0 "$panel_cert"
check 'public-CA download with a matching digest accepted' 0 \
  install_ca "$panel_digest" "$system_bundle_file"
check 'the public branch installs the system CA bundle' 0 \
  cmp -s -- "$TOP/ca-work/tls/ca.crt" "$system_bundle_file"

# The field failure behind Cloudflare: the edge certificate is public, so the
# download with --cacert succeeds, but /install/ca.pem returns the panel's own
# self-signed certificate and the gRPC port presents that one. The trust decision
# has to read the fetched file, not the connection; otherwise the Agent gets a
# system bundle that cannot verify the panel and enrollment fails with exit 60.
export STUB_OPENSSL_EXIT=1
stub_ca 200 0 0 "$panel_cert"
check 'a public edge with a private panel certificate is pinned' 0 \
  install_ca "$panel_digest" "$system_bundle_file"
check 'the fallback installs the fetched panel certificate, not the system bundle' 0 \
  cmp -s -- "$TOP/ca-work/tls/ca.crt" "$panel_cert"
check 'the fallback names the reverse proxy or CDN case' 0 \
  grep -q '反向代理／CDN' <<<"$(install_ca "$panel_digest" "$system_bundle_file" 2>&1)"
check 'the fallback still enforces the pinned digest' 1 \
  install_ca "$wrong_digest" "$system_bundle_file"
unset STUB_OPENSSL_EXIT

# The field failure: --http-port pointed at the gRPC listener, which answers a plain
# GET with 415.
stub_ca 415 0 60
grpc_port_download() { install_ca "$panel_digest" "$system_bundle_file"; }
check 'a CA URL answered by gRPC refused' 1 grpc_port_download
check 'the refusal names --http-port as the gRPC port' 0 \
  grep -q -- '--http-port 指向的是 gRPC 端口' <<<"$(grpc_port_download 2>&1 || true)"

stub_ca 000 7 7
check 'a refused connection is reported as unreachable' 0 \
  grep -q '连接被拒绝或不可达' <<<"$(install_ca "$panel_digest" "$system_bundle_file" 2>&1 || true)"
stub_ca 000 28 28
check 'a connection timeout is reported as a timeout' 0 \
  grep -q '超时' <<<"$(install_ca "$panel_digest" "$system_bundle_file" 2>&1 || true)"
stub_ca 404 0 22
check 'a 404 is reported as a development panel or another web server' 0 \
  grep -q '开发模式' <<<"$(install_ca "$panel_digest" "$system_bundle_file" 2>&1 || true)"

# The pinned branch: the certificate itself, checked against --ca-sha256.
stub_ca 200 0 60 "$panel_cert"
check 'pinned certificate with a matching digest accepted' 0 install_ca "$panel_digest" ''
check 'the pinned branch installs the fetched certificate' 0 \
  cmp -s -- "$TOP/ca-work/tls/ca.crt" "$panel_cert"
stub_ca 200 0 60 "$panel_cert"
check 'pinned certificate with a wrong digest refused' 1 install_ca "$wrong_digest" ''
check 'the refusal reports the digest mismatch' 0 \
  grep -q 'SHA-256 与面板显示的不一致' <<<"$(install_ca "$wrong_digest" '' 2>&1 || true)"

# The diagnosis itself, for the statuses the stubs above do not reach.
check 'the diagnosis reads 426 as gRPC too' 0 \
  grep -q 'gRPC 端口，不是面板 Web 端口' <<<"$(ca_download_diagnosis 426 0)"
check 'the diagnosis names DNS for exit code 6' 0 \
  grep -q 'DNS 查询失败' <<<"$(ca_download_diagnosis 000 6)"
check 'the diagnosis keeps unknown statuses' 0 \
  grep -q 'HTTP 500' <<<"$(ca_download_diagnosis 500 22)"

# --- gRPC preflight ---------------------------------------------------------------
# The preflight is what turns "enrollment failed" into a named cause when the panel
# is behind Cloudflare or a TLS-terminating reverse proxy: the Agent connects to
# $panel:$grpc_port directly, and the certificate there is the panel's own.
eval "$(extract curl_supports_http2)"
eval "$(extract tcp_reachable)"
eval "$(extract grpc_preflight_verdict)"
eval "$(extract check_grpc_endpoint)"

check 'an exit code of 0 is a verified handshake' 0 test "$(grpc_preflight_verdict 0)" = ok
check 'an HTTP error from gRPC is a verified handshake' 0 test "$(grpc_preflight_verdict 22)" = ok
check 'certificate failures stay separate' 0 test "$(grpc_preflight_verdict 60)" = untrusted
check 'an untrusted name is not called unreachable' 0 test "$(grpc_preflight_verdict 51)" = untrusted
check 'a refused port is unreachable' 0 test "$(grpc_preflight_verdict 7)" = unreachable
check 'a timeout is unreachable' 0 test "$(grpc_preflight_verdict 28)" = unreachable
check 'an ambiguous code stays ambiguous' 0 test "$(grpc_preflight_verdict 35)" = unknown

# stub_grpc <exit code> [<curl -V features>]
stub_grpc() {
  export STUB_CURL_STATUS=000 STUB_CURL_EXIT=$1 STUB_CURL_VERIFIED_EXIT=$1
  export STUB_CURL_BODY=''
  export STUB_CURL_FEATURES=${2-HTTP2}
}
# preflight <grpc host> <grpc port>
preflight() (
  fail() { printf '安装被拒绝：%s\n' "$*" >&2; exit 1; }
  grpc_host=$1
  grpc_port=$2
  ca_file=$TOP/ca-work/tls/ca.crt
  retry_note='本机尚未登记：注册码未过期时可以原样重跑本脚本。'
  check_grpc_endpoint
)

stub_grpc 0
check 'curl -V is read for HTTP/2 support' 0 curl_supports_http2
stub_grpc 0 ''
check 'a curl without HTTP/2 is detected' 1 curl_supports_http2

stub_grpc 0
check 'a verified handshake passes' 0 preflight "$panel" 18880
check 'the success line is printed' 0 \
  grep -q 'TLS 握手验证通过' <<<"$(preflight "$panel" 18880)"

stub_grpc 22
check 'an HTTP error from gRPC counts as a verified handshake' 0 preflight "$panel" 18880

# Exit code 60 is the one the field saw once the Web side was a CDN: the certificate
# on the gRPC port is the panel's own and is not covered by what the Agent trusts.
stub_grpc 60
check 'an untrusted gRPC certificate stops before enrollment' 1 preflight "$panel" 18880
check 'the refusal names Cloudflare and the direct gRPC port' 0 \
  grep -q 'Cloudflare' <<<"$(preflight "$panel" 18880 2>&1 || true)"
check 'the refusal says the command can be re-run' 0 \
  grep -q '可以原样重跑本脚本' <<<"$(preflight "$panel" 18880 2>&1 || true)"
check 'the refusal points at the saved install endpoint' 0 \
  grep -q '节点安装连接地址' <<<"$(preflight "$panel" 18880 2>&1 || true)"

stub_grpc 7
check 'a refused gRPC port stops before enrollment' 1 preflight "$panel" 18880
check 'the refusal reports the unreachable port' 0 \
  grep -q '无法连接面板的 gRPC 端口 panel.example.com:18880' \
  <<<"$(preflight "$panel" 18880 2>&1 || true)"
# A saved gRPC address that differs from --panel is what the preflight probes and
# what the refusal names; the Web certificate download still uses --panel.
check 'a separate gRPC host is probed and named' 0 \
  grep -q '无法连接面板的 gRPC 端口 grpc.example.com:18880' \
  <<<"$(preflight grpc.example.com 18880 2>&1 || true)"

# Anything else may be an ALPN or HTTP/2 quirk of this curl: warn and let enrollment
# decide instead of blocking a run that could work.
stub_grpc 35
check 'an ambiguous result does not block enrollment' 0 preflight "$panel" 18880
check 'the ambiguous result is reported as a warning' 0 \
  grep -q '警告' <<<"$(preflight "$panel" 18880 2>&1 || true)"

# Without HTTP/2 the preflight only proves the TCP port answers, and says so.
stub_grpc 0 ''
check 'a curl without HTTP/2 falls back to a TCP check' 1 preflight 127.0.0.1 1
check 'the fallback is reported as a TCP-only result' 0 \
  grep -q 'TCP 连接被拒绝或超时' <<<"$(preflight 127.0.0.1 1 2>&1 || true)"

# --- Summary ---------------------------------------------------------------------
printf '\nnode-install-test: %s passed, %s failed\n' "$passes" "$failed"
((failed == 0)) || exit 1

