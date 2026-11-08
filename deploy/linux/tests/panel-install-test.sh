#!/usr/bin/env bash
# Regression test for deploy/linux/panel-install.sh, the one-command panel
# installer.
#
# Run it on Linux, root not required:
#
#   bash deploy/linux/tests/panel-install-test.sh
#
# Nothing here installs a panel, starts a service or touches the host: every
# scenario either stops in the argument or host validation, or calls one extracted
# function with fixtures below one temporary directory. The script honours
# QUI_PANEL_INSTALL_TEST_ROOT as a prefix for every absolute path it touches and
# QUI_PANEL_INSTALL_TEST_UNAME/_EUID/_INIT_MARKER for the three host facts, so the
# refusal paths can be exercised on a host that does have root and systemd.
set -euo pipefail

if [[ $(uname -s) != Linux ]]; then
  printf 'SKIP panel-install-test: needs Linux (uname -s %s).\n' "$(uname -s)"
  if [[ $(uname -s) == MINGW* || $(uname -s) == MSYS* || ${QUI_PANEL_INSTALL_TEST_ALLOW_SKIP:-0} == 1 ]]; then exit 0; fi
  exit 1
fi

missing=''
for tool in awk grep sed mktemp tar sha256sum install diff; do
  command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
done
if [[ -n $missing ]]; then
  printf 'panel-install-test: missing commands:%s (coreutils and tar).\n' "$missing"
  exit 1
fi

script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
repo=$(cd -- "$script_dir/../../.." && pwd -P)
installer=$repo/deploy/linux/panel-install.sh
template=$repo/deploy/examples/panel.yaml
for file in "$installer" "$template"; do
  [[ -f $file ]] || { printf 'panel-install-test: missing %s\n' "$file" >&2; exit 1; }
done

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

TOP=$(mktemp -d "${TMPDIR:-/tmp}/qui-panel-install-test.XXXXXXXX") || exit 1
cleanup_top() {
  if [[ ${QUI_PANEL_INSTALL_TEST_KEEP:-0} == 1 ]]; then
    printf 'panel-install-test: temporary directory kept: %s\n' "$TOP"
    return 0
  fi
  rm -rf -- "$TOP"
}
trap cleanup_top EXIT

# Run the installer as the host gate sees it: Linux, root, and an init marker that
# exists. $1 is the marker directory, the rest is passed to the script.
run_installer() {
  local marker=$1
  shift
  QUI_PANEL_INSTALL_TEST_UNAME=Linux QUI_PANEL_INSTALL_TEST_EUID=0 \
    QUI_PANEL_INSTALL_TEST_INIT_MARKER="$marker" bash "$installer" "$@"
}

marker=$TOP/init-marker
mkdir -p -- "$marker"

# --- Argument parsing -----------------------------------------------------------
# --help must work without root, without systemd and without a release: it is what
# an operator reads before deciding to run the script at all.
help_output=$(bash "$installer" --help)
case $help_output in
  *'用法：bash panel-install.sh'*) check '--help prints the usage' 0 true;;
  *) check '--help prints the usage' 1 true;;
esac
case $help_output in
  *--http-port*--grpc-port*--bundle*) check '--help lists the flags' 0 true;;
  *) check '--help lists the flags' 1 true;;
esac
# The documented defaults are the Owner decision (22550 web/API, 18880 gRPC):
# an operator reads them here before running the install.
case $help_output in
  *'默认 22550'*'默认 18880'*) check '--help documents the default ports' 0 true;;
  *) check '--help documents the default ports' 1 true;;
esac
help_quiet() { bash "$installer" --help >/dev/null; }
check '--help with no other flag exits 0' 0 help_quiet

refuse() { ( "$@" ) 2>/dev/null; }
check 'unknown flag refused' 1 refuse bash "$installer" --no-such-flag
check 'missing value refused' 1 refuse bash "$installer" --version
check 'bad version refused' 1 refuse bash "$installer" --version 1.2
check 'rc version without number refused' 1 refuse bash "$installer" --version 1.2.3-rc
check 'leading zero refused' 1 refuse bash "$installer" --version 01.2.3
check 'http port 0 refused' 1 refuse bash "$installer" --http-port 0
check 'http port above 65535 refused' 1 refuse bash "$installer" --http-port 70000
check 'non-numeric grpc port refused' 1 refuse bash "$installer" --grpc-port abc
check 'identical ports refused' 1 refuse bash "$installer" --http-port 443 --grpc-port 443
check 'unknown tls mode refused' 1 refuse bash "$installer" --tls letsencrypt
check 'tls files without cert refused' 1 refuse bash "$installer" --tls files
check 'cert without tls files refused' 1 refuse bash "$installer" --cert "$template"
check 'address with port refused' 1 refuse bash "$installer" --address 203.0.113.10:443
check 'bundle without sums refused' 1 refuse bash "$installer" --bundle "$template"
check 'offline without version refused' 1 refuse bash "$installer" --bundle "$template" --sums "$template"
check 'missing cert file refused' 1 refuse bash "$installer" --tls files --cert "$TOP/none.pem" --key "$TOP/none.key"

# --- Host gate ------------------------------------------------------------------
# A test cannot drop privileges or stop systemd, so the three facts are overridden.
nonlinux() { QUI_PANEL_INSTALL_TEST_UNAME=Darwin bash "$installer"; }
check 'non-Linux host refused' 1 refuse nonlinux
nonroot() { QUI_PANEL_INSTALL_TEST_UNAME=Linux QUI_PANEL_INSTALL_TEST_EUID=1000 bash "$installer"; }
check 'non-root host refused' 1 refuse nonroot
nosystemd() {
  QUI_PANEL_INSTALL_TEST_UNAME=Linux QUI_PANEL_INSTALL_TEST_EUID=0 \
    QUI_PANEL_INSTALL_TEST_INIT_MARKER="$TOP/no-systemd-here" bash "$installer"
}
check 'host without systemd refused' 1 refuse nosystemd
refusal_message() { nosystemd 2>&1 || true; }
case $(refusal_message) in
  *'需要 systemd'*) check 'refusal names systemd' 0 true;;
  *) check 'refusal names systemd' 1 true;;
esac


# --- Configuration template -----------------------------------------------------
eval "$(extract panel_config)"
eval "$(extract config_first_install)"
eval "$(extract installed_panel)"

config_copy=$TOP/panel.yaml
cp -- "$template" "$config_copy"
rewritten=$TOP/panel.rewritten
panel_config "$config_copy" '0.0.0.0:8080' '0.0.0.0:8443' \
  '/etc/proxy-panel/tls/server.crt' '/etc/proxy-panel/tls/server.key' > "$rewritten"
check 'rewrite keeps the template line count' 0 test \
  "$(wc -l < "$rewritten")" = "$(wc -l < "$config_copy")"
check 'rewrite writes http_address' 0 grep -qx 'http_address: 0.0.0.0:8080' "$rewritten"
check 'rewrite writes grpc_address' 0 grep -qx 'grpc_address: 0.0.0.0:8443' "$rewritten"
check 'rewrite writes the certificate path' 0 grep -qx \
  '  certificate: /etc/proxy-panel/tls/server.crt' "$rewritten"
check 'rewrite writes the key path' 0 grep -qx '  key: /etc/proxy-panel/tls/server.key' "$rewritten"
check 'rewrite keeps the tls section' 0 grep -qx 'tls:' "$rewritten"
check 'rewrite keeps unrelated keys' 0 grep -qx 'development: false' "$rewritten"
check 'rewrite keeps the database path' 0 grep -qx \
  'database: /var/lib/proxy-panel/panel.db' "$rewritten"
# Everything the script does not own has to come through byte for byte.
pattern='^(http_address|grpc_address)[[:space:]]*:|^[[:space:]]+(certificate|key)[[:space:]]*:'
grep -v -E "$pattern" "$config_copy" > "$TOP/rest.before" || true
grep -v -E "$pattern" "$rewritten" > "$TOP/rest.after" || true
check 'rewrite changes only the four owned lines' 0 diff -u "$TOP/rest.before" "$TOP/rest.after"

grep -v '^grpc_address:' "$config_copy" > "$TOP/no-grpc.yaml"
without_grpc() { panel_config "$TOP/no-grpc.yaml" '0.0.0.0:443' '0.0.0.0:9443' a b > /dev/null; }
check 'template without grpc_address refused' 1 without_grpc
grep -v -E '^[[:space:]]+key[[:space:]]*:' "$config_copy" > "$TOP/no-key.yaml"
without_key() { panel_config "$TOP/no-key.yaml" '0.0.0.0:443' '0.0.0.0:9443' a b > /dev/null; }
check 'template without a tls key line refused' 1 without_key

config=$TOP/first-install/etc/proxy-panel/config.yaml
mkdir -p -- "$TOP/first-install/etc/proxy-panel"
check 'no configuration means a first install' 0 config_first_install
printf 'http_address: 0.0.0.0:443\n' > "$config"
check 'an existing configuration is never replaced' 1 config_first_install

manifest=$TOP/first-install/var/lib/proxycontrol-install/panel.manifest
mkdir -p -- "${manifest%/*}"
check 'missing install record is not an installation' 1 installed_panel
printf 'proxycontrol-install-v2\n' > "$manifest"
check 'install record marks the panel as installed' 0 installed_panel
# --- First install rewrites the configuration install.sh wrote -------------------
# install.sh writes the packaged template as soon as /etc/proxy-panel/config.yaml
# does not exist, so by the time the installer configures the panel the file exists.
# The decision is taken before install.sh runs; with it set, the template that
# install.sh just wrote is rewritten with the chosen ports and certificate paths, and
# its owner, group and mode are kept.
eval "$(extract regular_file)"
eval "$(extract install_configuration)"
eval "$(extract configure_panel)"

etc=$TOP/first-install/etc/proxy-panel
config=$etc/config.yaml
certificate=$etc/tls/server.crt
certificate_key=$etc/tls/server.key
http_port=23456
grpc_port=23457
account=$(id -un)
cp -- "$template" "$config"
chmod 0640 -- "$config"
owner_before=$(stat -c '%u:%g:%a' -- "$config")
install_tls() { printf 'install_tls would run here\n'; }
# Both functions stop through fail() on error; the stub keeps such a failure inside
# the scenario (the check then reports it) instead of ending the test.
write_config() ( fail() { printf 'fail: %s\n' "$*" >&2; exit 1; }; configure_panel "$@" )
check 'a first install rewrites the template install.sh left behind' 0 \
  write_config true "$template"
check 'the rewrite writes the chosen http port' 0 grep -qx 'http_address: 0.0.0.0:23456' "$config"
check 'the rewrite writes the chosen grpc port' 0 grep -qx 'grpc_address: 0.0.0.0:23457' "$config"
check 'the rewrite writes the certificate path' 0 grep -qx \
  "  certificate: $certificate" "$config"
check 'the rewrite keeps the owner, group and mode' 0 test \
  "$(stat -c '%u:%g:%a' -- "$config")" = "$owner_before"

# An existing configuration that is not the template is left byte for byte: the
# install path never rewrites a configuration the operator already has.
printf 'http_address: 0.0.0.0:443\ngrpc_address: 0.0.0.0:9443\n' > "$config"
cp -- "$config" "$TOP/config.before-rewrite"
check 'an existing configuration is not rewritten' 0 write_config false "$template"
check 'an existing configuration is byte-identical afterwards' 0 diff -u \
  "$TOP/config.before-rewrite" "$config"




# --- Address handling -----------------------------------------------------------
# A domain and an IPv4 literal keep their DNS:/IP: form; an IPv6 literal, with or
# without the brackets of a URL, becomes an IP: entry without brackets, and the URL
# shows it in brackets.
eval "$(extract address_host)"
eval "$(extract is_ipv6_literal)"
eval "$(extract address_allowed)"
eval "$(extract san_for)"
eval "$(extract read_value)"
eval "$(extract config_port)"
eval "$(extract display_host)"
eval "$(extract panel_url)"

# The flag path end to end: a refused address stops in validation, an accepted one
# reaches the host gate (overridden here to say Darwin, so nothing is installed).
address_verdict() { QUI_PANEL_INSTALL_TEST_UNAME=Darwin bash "$installer" --address "$1" 2>&1 || true; }
address_passes() { case $(address_verdict "$1") in *'只支持 Linux'*) return 0;; *) return 1;; esac; }
address_rejected() { case $(address_verdict "$1") in *'地址只能是'*) return 0;; *) return 1;; esac; }
check '--address accepts an IPv6 literal' 0 address_passes 2001:db8::1
check '--address accepts a bracketed IPv6 literal' 0 address_passes '[2001:db8::1]'
check '--address with a port stops at validation' 0 address_rejected 203.0.113.10:443

check 'a domain is an acceptable address' 0 address_allowed panel.example.com
check 'an IPv4 address is acceptable' 0 address_allowed 203.0.113.10
check 'an IPv6 address is acceptable' 0 address_allowed 2001:db8::1
check 'a bracketed IPv6 address is acceptable' 0 address_allowed '[2001:db8::1]'
check 'an address with a port is still refused' 1 address_allowed 203.0.113.10:443
check 'a single colon is not an IPv6 literal' 1 is_ipv6_literal 2001:db8

# SAN of the chosen address, exactly as it is written into the certificate.
san_is() { [[ $(san_for "$1") == "$2" ]]; }
check 'SAN of a domain stays DNS' 0 san_is panel.example.com \
  'DNS:panel.example.com,DNS:localhost,IP:127.0.0.1'
check 'SAN of an IPv4 address stays IP' 0 san_is 203.0.113.10 \
  'IP:203.0.113.10,DNS:localhost,IP:127.0.0.1'
check 'SAN of an IPv6 address is IP:2001:db8::1' 0 san_is 2001:db8::1 \
  'IP:2001:db8::1,DNS:localhost,IP:127.0.0.1'
check 'SAN of a bracketed IPv6 address drops the brackets' 0 san_is '[2001:db8::1]' \
  'IP:2001:db8::1,DNS:localhost,IP:127.0.0.1'

# The URL the operator is told to open; an IPv6 host needs brackets there.
url_is() (
  address=$1
  http_port=$2
  config=$TOP/no-config-yet.yaml
  [[ $(panel_url) == "$3" ]]
)
check 'IPv4 URL keeps its port' 0 url_is 203.0.113.10 22550 'https://203.0.113.10:22550/'
check 'domain URL stays unbracketed' 0 url_is panel.example.com 443 'https://panel.example.com/'
check 'IPv6 URL is bracketed' 0 url_is 2001:db8::1 22550 'https://[2001:db8::1]:22550/'
check 'bracketed IPv6 URL is not bracketed twice' 0 url_is '[2001:db8::1]' 443 \
  'https://[2001:db8::1]/'


# --- Self-signed certificate ----------------------------------------------------
if command -v openssl >/dev/null 2>&1; then
  eval "$(extract generate_self_signed)"
  eval "$(extract certificate_pair_matches)"
  eval "$(extract certificate_valid_now)"

  ip_directory=$TOP/cert-ip
  dns_directory=$TOP/cert-dns
  ipv6_directory=$TOP/cert-ipv6
  mkdir -p -- "$ip_directory" "$dns_directory" "$ipv6_directory"
  check 'self-signed certificate for an IPv4 address generated' 0 \
    generate_self_signed "$ip_directory" 203.0.113.10
  check 'generated certificate is currently valid' 0 \
    certificate_valid_now "$ip_directory/server.crt"
  check 'generated certificate and key match' 0 \
    certificate_pair_matches "$ip_directory/server.crt" "$ip_directory/server.key"
  check 'generated private key is 0600' 0 test \
    "$(stat -c '%a' "$ip_directory/server.key")" = 600
  ip_text=$(openssl x509 -in "$ip_directory/server.crt" -noout -text)
  case $ip_text in
    *'IP Address:203.0.113.10'*) check 'SAN carries the chosen IPv4 address' 0 true;;
    *) check 'SAN carries the chosen IPv4 address' 1 true;;
  esac
  case $ip_text in
    *'DNS:localhost'*) check 'SAN carries localhost' 0 true;;
    *) check 'SAN carries localhost' 1 true;;
  esac
  # C=10 years; nine years of validity is what a ten-year certificate still has.
  lasts_nine_years() { openssl x509 -in "$1" -noout -checkend 284018400 >/dev/null 2>&1; }
  check 'generated certificate lasts about ten years' 0 lasts_nine_years "$ip_directory/server.crt"

  check 'self-signed certificate for a domain generated' 0 \
    generate_self_signed "$dns_directory" panel.example.com
  dns_text=$(openssl x509 -in "$dns_directory/server.crt" -noout -text)
  case $dns_text in
    *'DNS:panel.example.com'*) check 'SAN carries the chosen domain' 0 true;;
    *) check 'SAN carries the chosen domain' 1 true;;
  esac
  check 'mismatched certificate and private key refused' 1 \
    certificate_pair_matches "$ip_directory/server.crt" "$dns_directory/server.key"

  # An IPv6 address has to reach the SAN as an IP: entry, not as DNS:2001:db8::1.
  check 'self-signed certificate for an IPv6 address generated' 0 \
    generate_self_signed "$ipv6_directory" 2001:db8::1
  ipv6_text=$(openssl x509 -in "$ipv6_directory/server.crt" -noout -text)
  case $ipv6_text in
    *'IP Address:2001'*) check 'SAN carries the chosen IPv6 address as IP' 0 true;;
    *) check 'SAN carries the chosen IPv6 address as IP' 1 true;;
  esac
else
  printf 'skip - openssl is missing, certificate scenarios not run\n'
fi

# --- uninstall.sh lookup --------------------------------------------------------
# The documented one-liner leaves panel-install.sh in an arbitrary directory such as
# /root: an unrelated uninstall.sh sitting next to it must never be run as root. Only
# a sibling deploy/linux layout of an extracted archive or a checkout provides one.
eval "$(extract regular_file)"
eval "$(extract sibling_uninstall)"

plain=$TOP/plain-download
mkdir -p -- "$plain"
printf '#!/bin/sh\nexit 0\n' > "$plain/panel-install.sh"
printf '#!/bin/sh\nexit 0\n' > "$plain/uninstall.sh"
plain=$(cd -- "$plain" && pwd -P)
check 'a plain directory never provides uninstall.sh' 1 \
  sibling_uninstall "$plain/panel-install.sh"
check 'a plain directory does not run its decoy uninstall.sh' 0 test -z \
  "$(sibling_uninstall "$plain/panel-install.sh" || true)"

layout=$TOP/release-layout
mkdir -p -- "$layout/deploy/linux"
for file in install.sh uninstall.sh service.sh; do
  printf '#!/bin/sh\nexit 0\n' > "$layout/deploy/linux/$file"
done
layout=$(cd -- "$layout" && pwd -P)
check 'an extracted release layout provides its own uninstall.sh' 0 test \
  "$(sibling_uninstall "$layout/deploy/linux/panel-install.sh")" = \
  "$layout/deploy/linux/uninstall.sh"

# A symlinked sibling is not a regular file, so the layout does not count.
mv -- "$layout/deploy/linux/service.sh" "$layout/deploy/linux/service.real.sh"
ln -s -- "$layout/deploy/linux/service.real.sh" "$layout/deploy/linux/service.sh"
check 'a symlinked service.sh is not a release layout' 1 \
  sibling_uninstall "$layout/deploy/linux/panel-install.sh"
rm -f -- "$layout/deploy/linux/service.sh"
mv -- "$layout/deploy/linux/service.real.sh" "$layout/deploy/linux/service.sh"
check 'the layout counts once all three files are regular' 0 test \
  "$(sibling_uninstall "$layout/deploy/linux/panel-install.sh")" = \
  "$layout/deploy/linux/uninstall.sh"

# --- Offline release verification ----------------------------------------------
eval "$(extract download)"
eval "$(extract sums_digest)"
eval "$(extract release_bundle)"
eval "$(extract fetch_release)"

fixture=$TOP/release
bundle_name=proxy-control-9.9.9-linux-amd64
mkdir -p -- "$fixture/tree/$bundle_name/bin"
printf 'fixture panel binary\n' > "$fixture/tree/$bundle_name/bin/proxy-panel"
tar -czf "$fixture/$bundle_name.tar.gz" -C "$fixture/tree" "$bundle_name"
digest=$(sha256sum -- "$fixture/$bundle_name.tar.gz"); digest=${digest%% *}
printf '%s  %s\n' "$digest" "$bundle_name.tar.gz" > "$fixture/SHA256SUMS"
printf '%s  %s\n' "$(printf '0%.0s' {1..64})" "$bundle_name.tar.gz" > "$fixture/SHA256SUMS.bad"
printf '%s  %s\n' "$digest" 'proxy-control-1.0.0-linux-amd64.tar.gz' > "$fixture/SHA256SUMS.other"

# fetch_release with the offline files, in a subshell so fail() ends only that run.
offline_fetch() (
  fail() { exit 1; }
  version=9.9.9
  architecture=amd64
  repository=jingniao/qui
  bundle_file=$fixture/$bundle_name.tar.gz
  sums_file=$2
  directory=$1
  mkdir -p -- "$directory"
  fetch_release
)
check 'offline archive with a matching digest accepted' 0 \
  offline_fetch "$TOP/fetch-good" "$fixture/SHA256SUMS"
check 'offline archive unpacked from the verified tarball' 0 test \
  -f "$TOP/fetch-good/$bundle_name/bin/proxy-panel"
check 'offline archive with a bad digest refused' 1 \
  offline_fetch "$TOP/fetch-bad" "$fixture/SHA256SUMS.bad"
check 'SHA256SUMS without this archive refused' 1 \
  offline_fetch "$TOP/fetch-other" "$fixture/SHA256SUMS.other"

# --- Re-install after an uninstall ----------------------------------------------
# uninstall.sh leaves panel.removed behind and install.sh then refuses to run
# without --reinstall, so the installer has to detect that record and hand the flag
# over. A stub install.sh inside the release layout records its own arguments and
# returns 0; the run stops later (there is no proxy-panel account here), which is
# irrelevant to what is checked: which arguments install.sh received.
stub_bundle=proxy-control-9.9.9-linux-amd64
stub_tree=$fixture/stub/tree
mkdir -p -- "$stub_tree/$stub_bundle/bin" "$stub_tree/$stub_bundle/deploy/linux" \
  "$stub_tree/$stub_bundle/deploy/examples"
printf 'fixture panel binary\n' > "$stub_tree/$stub_bundle/bin/proxy-panel"
cp -- "$template" "$stub_tree/$stub_bundle/deploy/examples/panel.yaml"
printf '#!/bin/sh\nprintf "%%s\\n" "$*" >> "$QUI_PANEL_INSTALL_TEST_ROOT/install-arguments"\nexit 0\n' \
  > "$stub_tree/$stub_bundle/deploy/linux/install.sh"
chmod 0755 -- "$stub_tree/$stub_bundle/deploy/linux/install.sh"
tar -czf "$fixture/stub/$stub_bundle.tar.gz" -C "$stub_tree" "$stub_bundle"
stub_digest=$(sha256sum -- "$fixture/stub/$stub_bundle.tar.gz"); stub_digest=${stub_digest%% *}
printf '%s  %s\n' "$stub_digest" "$stub_bundle.tar.gz" > "$fixture/stub/SHA256SUMS"

# One unattended run against $1; the stub records the arguments install.sh got.
stub_install() {
  QUI_PANEL_INSTALL_TEST_ROOT="$1" run_installer "$marker" --version 9.9.9 \
    --bundle "$fixture/stub/$stub_bundle.tar.gz" --sums "$fixture/stub/SHA256SUMS" \
    --address 203.0.113.10 --http-port 24550 --grpc-port 24551 --tls self-signed --yes \
    >/dev/null 2>&1 || true
}
reinstall_root=$TOP/reinstall-root
mkdir -p -- "$reinstall_root/var/lib/proxycontrol-install"
printf 'proxycontrol-install-v2\npanel\n' > "$reinstall_root/var/lib/proxycontrol-install/panel.removed"
fresh_root=$TOP/fresh-root
mkdir -p -- "$fresh_root"
# The runs go through the installer's host gate, which needs these tools plus root
# and systemd stand-ins; without them the arguments are never recorded.
if command -v systemctl >/dev/null 2>&1 && command -v runuser >/dev/null 2>&1 \
  && command -v openssl >/dev/null 2>&1 && command -v curl >/dev/null 2>&1; then
  stub_install "$reinstall_root"
  check 'a retained uninstall record is passed as --reinstall' 0 grep -q -- '--reinstall' \
    "$reinstall_root/install-arguments"
  check 'the re-install still names the panel component' 0 grep -q -- '--panel' \
    "$reinstall_root/install-arguments"
  stub_install "$fresh_root"
  check 'a host without the record is not passed --reinstall' 1 grep -q -- '--reinstall' \
    "$fresh_root/install-arguments"
else
  printf 'skip - systemctl, runuser, openssl or curl is missing, re-install scenario not run\n'
fi


# The menu reads the host through systemctl, so it only runs where that exists; it
# never installs anything and choice 0 just leaves.
if command -v systemctl >/dev/null 2>&1 && command -v runuser >/dev/null 2>&1; then
  menu_root=$TOP/menu-root
  mkdir -p -- "$menu_root/var/lib/proxycontrol-install"
  printf 'proxycontrol-install-v2\npanel\n' > "$menu_root/var/lib/proxycontrol-install/panel.manifest"
  menu_run() {
    printf '0\n' | QUI_PANEL_INSTALL_TEST_ROOT="$menu_root" run_installer "$marker"
  }
  check 'installed host shows the menu and leaves on 0' 0 menu_run
  menu_output=$(menu_run 2>&1 || true)
  case $menu_output in
    *'更新到最新版本或指定版本'*'卸载'*) check 'menu offers update and uninstall' 0 true;;
    *) check 'menu offers update and uninstall' 1 true;;
  esac
else
  printf 'skip - systemctl or runuser is missing, menu scenario not run\n'
fi

# --- --update is for installed hosts only ----------------------------------------
# The flag is the menu's update without the menu, so a host without the panel has to
# be refused with a message that says so instead of installing anything.
empty_root=$TOP/not-installed
mkdir -p -- "$empty_root"
update_refused() { ( QUI_PANEL_INSTALL_TEST_ROOT="$empty_root" run_installer "$marker" --update ) 2>/dev/null; }
update_message() { QUI_PANEL_INSTALL_TEST_ROOT="$empty_root" run_installer "$marker" --update 2>&1 || true; }
if command -v systemctl >/dev/null 2>&1 && command -v runuser >/dev/null 2>&1 \
  && command -v openssl >/dev/null 2>&1; then
  check '--update on a host without the panel refused' 1 update_refused
  case $(update_message) in
    *'没有安装面板'*'--update'*) check '--update says the panel is not installed' 0 true;;
    *) check '--update says the panel is not installed' 1 true;;
  esac
else
  printf 'skip - systemctl, runuser or openssl is missing, --update scenario not run\n'
fi

# --- The panel update bridge install.sh publishes ---------------------------------
# The web UI cannot replace the panel itself, so install.sh publishes a root helper and
# the path+oneshot unit pair that runs it, and creates the request directory they share.
# Nothing here runs install.sh - that needs root and a real host - so the packaged files
# and the lines that publish them are checked instead.
install_script=$repo/deploy/linux/install.sh
updater_script=$repo/deploy/linux/update.sh
update_helper=$repo/deploy/linux/panel-update-request.sh
update_service=$repo/deploy/systemd/proxy-panel-update.service
update_path=$repo/deploy/systemd/proxy-panel-update.path
for file in "$install_script" "$updater_script" "$update_helper" "$update_service" "$update_path"; do
  check "packaged file exists: ${file##*/}" 0 test -f "$file"
done
has_text() { grep -q -F -- "$1" "$2"; }

# The path unit is the only thing that decides when the helper runs, and it has to name
# the request file the panel writes and the unit that runs the helper.
check 'path unit watches the panel request file' 0 has_text \
  'PathChanged=/var/lib/proxy-panel/panel-update/request.json' "$update_path"
check 'path unit arms the oneshot unit' 0 has_text 'Unit=proxy-panel-update.service' "$update_path"
check 'the oneshot unit runs the installed helper' 0 has_text \
  'ExecStart=/usr/local/lib/proxycontrol/panel-update-request' "$update_service"
check 'the oneshot unit is a root oneshot with a long timeout' 0 has_text 'TimeoutStartSec=1800' "$update_service"
check 'the oneshot unit is never rate-limited' 0 has_text 'StartLimitIntervalSec=0' "$update_service"
check 'the oneshot unit runs after the boot recovery' 0 has_text \
  'After=proxycontrol-update-recovery.service' "$update_service"
# qui-update stops the panel while the helper runs, so a dependency on the panel unit
# would cancel the update halfway.
for directive in 'PartOf=proxy-panel.service' 'BindsTo=proxy-panel.service' 'Requires=proxy-panel.service'; do
  check "the oneshot unit has no $directive" 1 has_text "$directive" "$update_service"
done

# install.sh: the helper and both units are published with the modes systemd expects, the
# request directory is created for the panel account, and the path unit is enabled and
# started while the oneshot unit is never started from there.
check 'install.sh publishes the helper' 0 has_text \
  'publish "$stage/panel-update-request" "$panel_update" root root 0755' "$install_script"
check 'install.sh publishes the service unit' 0 has_text \
  'publish "$stage/panel-update-service" "$panel_update_service" root root 0644' "$install_script"
check 'install.sh publishes the path unit' 0 has_text \
  'publish "$stage/panel-update-path" "$panel_update_path" root root 0644' "$install_script"
check 'install.sh creates the request directory' 0 has_text \
  'install -d -o "$account" -g "$account" -m 0700 "$data/panel-update"' "$install_script"
check 'install.sh enables and starts the path unit' 0 has_text \
  'systemctl enable "${panel_update_path##*/}"' "$install_script"
check 'install.sh starts the path unit' 0 has_text \
  'systemctl start "${panel_update_path##*/}"' "$install_script"
# Nothing in install.sh may start the oneshot unit: only a panel request may.
if grep -q -E 'systemctl (start|restart)[^|]*panel_update_service|systemctl start .*proxy-panel-update\.service' "$install_script"; then
  check 'install.sh never starts the oneshot unit' 1 true
else
  check 'install.sh never starts the oneshot unit' 0 true
fi

# qui-update snapshots exactly the files install.sh publishes for the panel, so a rollback
# puts the bridge back on a host that had it and removes it on a host that did not.
check 'update.sh snapshots the helper' 0 has_text \
  '"$root/usr/local/lib/proxycontrol/panel-update-request"' "$updater_script"
check 'update.sh snapshots the service unit' 0 has_text \
  '"$root/etc/systemd/system/proxy-panel-update.service"' "$updater_script"
check 'update.sh snapshots the path unit' 0 has_text \
  '"$root/etc/systemd/system/proxy-panel-update.path"' "$updater_script"

printf '\npanel-install-test: %s passed, %s failed\n' "$passes" "$failed"
((failed == 0))

