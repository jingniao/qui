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

reset() {
  rm -rf -- "$QUI_AGENT_DATA"
  mkdir -p "$QUI_AGENT_DATA/identity"
  init_active=1; init_result=0; healthy=1; started=0; stopped=0
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

printf 'PASS agent-web-upgrade-test\n'
