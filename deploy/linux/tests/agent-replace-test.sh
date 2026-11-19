#!/usr/bin/env bash
# Run the packaged stop/drain/detach sequence against a private fake host.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd -P)
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT
sed -e "s|/var/lib/proxy-agent|$test_root/var/lib/proxy-agent|g" \
    -e "s|/run/proxy-agent|$test_root/run/proxy-agent|g" \
    -e "s|/etc/proxy-agent|$test_root/etc/proxy-agent|g" \
    -e 's|/proc/\$pid/status|$test_root/tracer-status|g' \
    "$here/../agent-replace.sh" > "$test_root/replacement.sh"
. "$test_root/replacement.sh"
fail(){ echo "$*" >&2; exit 1; }
service_main_pid(){ echo 4321; }
service_is_active(){ return 1; }
verify_replacement_signature(){ [[ $scenario != bad-signature ]]; }
chown(){ :; }
install(){ cp -- "${@: -2:1}" "${@: -1}"; }
systemctl(){
  if [[ $1 == stop ]]; then echo stop >> "$test_root/ops"; [[ $scenario != stop-fails ]]; return; fi
  case $4 in
    ExecMainPID) echo 4321;;
    ExecMainCode) echo 1;;
    ExecMainStatus) if [[ $scenario == final-sample-fails ]]; then echo 1; else echo 0; fi;;
    Result) echo success;;
    *) return 1;;
  esac
}
strace(){
  local output
  while (($#)); do case $1 in -o) output=$2;shift 2;; *)shift;; esac; done
  echo "TracerPid: $BASHPID" > "$test_root/tracer-status"
  while [[ ! -e $test_root/stopped ]]; do sleep .02; done
  if [[ $scenario == final-sample-fails ]]; then echo '+++ exited with 1 +++'; else echo '+++ exited with 0 +++'; fi > "$output"
}
rc-service(){
  echo stop >> "$test_root/ops"
  touch "$test_root/stopped"
  [[ $scenario != stop-fails ]]
}
runuser(){
  if [[ ${!#} == --check-config ]]; then [[ $scenario != custom-ca ]]; return; fi
  echo drain >> "$test_root/ops"
  if [[ $scenario == no-ack ]]; then return 1; fi
  printf 'old-agent-drained\n'
  local receipt
  read -r receipt
  [[ $receipt == detached && ! -e $data ]]
}
for init_system in systemd openrc; do
  for scenario in bad-signature custom-path custom-ca stop-fails final-sample-fails no-ack confirmed; do
    data=$test_root/var/lib/proxy-agent
    mkdir -p "$data/identity" "$data/spool" "$test_root/etc/proxy-agent/"{tls,certs} "$test_root/run"
    printf 'data_directory: %s\n' "$data" > "$test_root/etc/proxy-agent/config.yaml"
    touch "$test_root/etc/proxy-agent/tls/ca.crt"
    if [[ $scenario == custom-path ]]; then echo 'data_directory: /another/node' > "$test_root/etc/proxy-agent/config.yaml"; fi
    printf 'identity' > "$data/identity/client.crt"
    printf 'original traffic' > "$data/spool/queue.db"
    printf 'inbound certificate' > "$test_root/etc/proxy-agent/certs/site.crt"
    printf 'panel.example.com:18880\n' > "$test_root/replacement-endpoint"
    touch "$test_root/replacement-ca.crt"
    printf 'input' > "$test_root/input"
    : > "$test_root/ops"
    : > "$test_root/tracer-status"
    rm -f -- "$test_root/stopped"
    # A separate shell keeps errexit active within the production function.
    set +e
    (set -e; replace_installed_agent "$test_root/input" "$test_root/input" "$test_root/input" 1.7.3 amd64)
    result=$?
    set -e
    if [[ $scenario == confirmed ]]; then
      [[ $result == 0 && ! -e $data && $(cat "$test_root/ops") == $'stop\ndrain' ]]
    else
      [[ $result != 0 && $(cat "$data/spool/queue.db") == 'original traffic' && $(cat "$data/identity/client.crt") == 'identity' ]]
      case $scenario in
        bad-signature|custom-path|custom-ca) [[ ! -s $test_root/ops ]];;
        stop-fails|final-sample-fails) [[ $(cat "$test_root/ops") == stop ]];;
        no-ack) [[ $(cat "$test_root/ops") == $'stop\ndrain' ]];;
      esac
    fi
    [[ $(cat "$test_root/etc/proxy-agent/certs/site.crt") == 'inbound certificate' ]]
    printf 'ok - %s %s\n' "$init_system" "$scenario"
  done
done

# From the confirmed, erased state above, exercise the actual enrollment
# portion twice. A lost enrollment response must preserve this CSR's key.
data=$test_root/var/lib/proxy-agent
mkdir -p "$data/identity" "$test_root/bundle/deploy/linux"
source_dir=$test_root/bundle
export data
sed -n '/^data_directory=\/var\/lib\/proxy-agent$/,/^# The init-system library comes/ p' "$here/../node-install.sh" |
  sed "s|/var/lib/proxy-agent|$data|g" > "$test_root/enrollment.sh"
cat > "$source_dir/deploy/linux/enroll.sh" <<'ENROLL'
#!/usr/bin/env bash
set -eu
if [[ ! -e $data/enrollment-attempt.json ]]; then
  digest=$(tr -d '\n' < "$2" | sha256sum)
  printf '{"version":1,"tokenSha256":"%s","controlSha256":"control"}' "${digest%% *}" > "$data/enrollment-attempt.json"
  printf 'original key' > "$data/identity/client.key"
  exit 1
fi
[[ $(cat "$data/identity/client.key") == 'original key' ]]
printf 'same-node cert' > "$data/identity/client.crt"
ENROLL
chmod +x "$source_dir/deploy/linux/enroll.sh"
token=unused_or_replayable_recovery_token
replace_agent=true
set +e
(set -e; . "$test_root/enrollment.sh") 2>"$test_root/enroll-error"
result=$?
set -e
[[ $result != 0 && -f $data/identity/client.key && ! -e $data/identity/client.crt ]]
grep -Fq -- '去掉命令的 --replace-agent' "$test_root/enroll-error"
replace_agent=false
(set -e; . "$test_root/enrollment.sh")
[[ $(cat "$data/identity/client.crt") == 'same-node cert' && $(cat "$data/identity/client.key") == 'original key' ]]
printf 'ok - resume original recovery enrollment after confirmed cleanup and lost response\n'
