#!/usr/bin/env bash
set -euo pipefail

if [[ ${OSTYPE:-} == msys* || ${OSTYPE:-} == cygwin* || ${OS:-} == Windows_NT ]]; then
  printf 'SKIP agent-web-upgrade-test: Windows shell\n'
  exit 0
fi

script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
service_script=$script_dir/../service.sh
source "$service_script"

root=$(mktemp -d "${TMPDIR:-/tmp}/qui-agent-web-upgrade.XXXXXXXX")
trap 'rm -rf -- "$root"' EXIT
QUI_AGENT_DATA=$root/data
export QUI_AGENT_DATA
mkdir -p "$QUI_AGENT_DATA/identity"
init_active=1
init_result=0
healthy=1
started=0
stopped=0

service_is_active() { ((init_active)); }
service_stop() { stopped=$((stopped + 1)); init_active=0; }
service_start() { started=$((started + 1)); init_active=1; return 0; }
service_healthy() { ((healthy)); }
chown() { return 0; }
runuser() {
  if ((init_result)); then
    # An interrupted initializer leaves its pending marker and partial paths.
    mkdir -p "$QUI_AGENT_DATA/releases/agent-installed"
    : > "$QUI_AGENT_DATA/identity/agent-supervision-init.pending"
    return "$init_result"
  fi
  mkdir -p "$QUI_AGENT_DATA/identity" "$QUI_AGENT_DATA/agent-release-history" "$QUI_AGENT_DATA/releases/agent-installed"
  : > "$QUI_AGENT_DATA/identity/agent-supervisor.enc"
  return 0
}

baseline=$QUI_AGENT_DATA/identity/agent-storage-contract.enc
sleeps=0
sleep_hook=
# Each poll counts as one second; the hook lets a scenario change state mid-wait.
sleep() { sleeps=$((sleeps + 1)); [[ -z $sleep_hook ]] || "$sleep_hook"; }
create_baseline() { ((sleeps < 3)) || : > "$baseline"; }
deactivate_service() { init_active=0; }

# The Agent has normally written its storage baseline by the time it is enabled.
reset() {
  rm -rf -- "$QUI_AGENT_DATA"
  mkdir -p "$QUI_AGENT_DATA/identity"
  : > "$baseline"
  init_active=1; init_result=0; healthy=1; started=0; stopped=0; sleeps=0; sleep_hook=
  unset QUI_WEB_UPGRADE_WAIT
}
fail() { printf 'not ok: %s\n' "$1" >&2; exit 1; }
has_backup() { compgen -G "$QUI_AGENT_DATA/web-upgrade-failed-*/*" >/dev/null; }

# Already supervised: nothing is restarted.
: > "$QUI_AGENT_DATA/identity/agent-supervisor.enc"
agent_enable_web_upgrade >/dev/null
[[ $started == 0 && $stopped == 0 ]] || fail 'already supervised node was restarted'

# Inactive services are left alone.
reset; init_active=0
agent_enable_web_upgrade >/dev/null
[[ $started == 0 && $stopped == 0 ]] || fail 'inactive service was touched'

# Successful initialization stops then starts the service once.
reset
agent_enable_web_upgrade >/dev/null
[[ $stopped == 1 && $started == 1 && -f $QUI_AGENT_DATA/identity/agent-supervisor.enc ]] \
  || fail 'successful initialization did not restart exactly once'

# Initialization failure moves the partial state aside and starts standalone mode.
reset; init_result=1
if agent_enable_web_upgrade >/dev/null 2>&1; then fail 'initialization failure reported success'; fi
[[ $started == 1 ]] || fail 'standalone service was not started after initialization failure'
has_backup || fail 'initialization failure left no backup'
[[ ! -e $QUI_AGENT_DATA/identity/agent-supervision-init.pending && ! -e $QUI_AGENT_DATA/releases/agent-installed ]] \
  || fail 'partial state was not moved aside'

# A pending marker next to a supervision marker is not treated as enabled.
reset
: > "$QUI_AGENT_DATA/identity/agent-supervisor.enc"
: > "$QUI_AGENT_DATA/identity/agent-supervision-init.pending"
agent_enable_web_upgrade >/dev/null 2>&1 || true
[[ $stopped -ge 1 ]] || fail 'pending marker was treated as enabled'

# A health failure after initialization follows the same rollback path.
reset; healthy=0
if agent_enable_web_upgrade >/dev/null 2>&1; then fail 'health failure reported success'; fi
[[ $started == 2 ]] || fail 'standalone service was not restarted after health failure'
has_backup || fail 'health failure left no backup'
[[ ! -e $QUI_AGENT_DATA/identity/agent-supervisor.enc ]] || fail 'supervision marker survived a health failure'

# The baseline appears after a few polls: enabling waits, and only then stops the service.
reset; rm -f -- "$baseline"; sleep_hook=create_baseline
agent_enable_web_upgrade >/dev/null
[[ $sleeps == 3 && $stopped == 1 && $started == 1 && -f $QUI_AGENT_DATA/identity/agent-supervisor.enc ]] \
  || fail 'did not wait for the storage baseline before stopping the service'

# The baseline never appears: give up without touching the running service.
reset; rm -f -- "$baseline"; QUI_WEB_UPGRADE_WAIT=5
if agent_enable_web_upgrade >/dev/null 2>&1; then fail 'missing baseline reported success'; fi
[[ $sleeps == 5 && $stopped == 0 && $started == 0 ]] || fail 'service was touched while the baseline was missing'
has_backup && fail 'missing baseline created a failure backup'
[[ ! -e $QUI_AGENT_DATA/identity/agent-supervisor.enc ]] || fail 'initialization ran without a baseline'

# The service stops while waiting: return without stopping or initializing.
reset; rm -f -- "$baseline"; sleep_hook=deactivate_service
agent_enable_web_upgrade >/dev/null 2>&1 || fail 'service that stopped while waiting was treated as a failure'
[[ $stopped == 0 && $started == 0 && ! -e $QUI_AGENT_DATA/identity/agent-supervisor.enc ]] \
  || fail 'inactive service was initialized after waiting'

printf 'PASS agent-web-upgrade-test\n'
