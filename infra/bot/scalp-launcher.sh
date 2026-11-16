#!/usr/bin/env bash
# infra/bot/scalp-launcher.sh — host-side post-trade launcher (ADR-092, task 146 §5).
#
# The backend never spawns Claude: on Outcome.PLACED it arms an outbox flag
# (bot_trade.scalp_session_status='PENDING', same tx as the PLACED audit). This loop
# polls GET /bot/trades/pending-scalp every ~10 s with X-Service-Token (same env/curl
# pattern as sync-calendar.sh), atomically claims each id (POST .../scalp-claim:
# 200 = won, 409 = someone else won), and launches ./scalp-session.sh <id> DETACHED
# so the 15-min session never blocks the loop. When the session exits, the wrapper
# posts POST .../scalp-finish with the exit code.
#
# Single-flight is two layers: global `flock -n` (no two launchers) + the atomic DB
# claim (no two sessions for one trade). A RUNNING-stuck row is never auto-reclaimed.
# manage-position.sh is intentionally never called from here — it stays manual-only.
#
# NOT a Docker service, NOT cron (1-min granularity rejected by ADR-092): runs as a
# systemd user service, see infra/bot/scalp-launcher.service.
#
# Usage:
#   ./scalp-launcher.sh --once      single poll pass (verification / tests)
#   ./scalp-launcher.sh --loop      daemon loop (systemd ExecStart)
#   ./scalp-launcher.sh --dry-run   single pass, real GET+claim, no session launch,
#                                   no finish post (CI ordering proof, no claude call).
#                                   ONLY against a stubbed API: on a real backend a
#                                   dry-run claim would strand the row in RUNNING.
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." &>/dev/null && pwd)"
ENV_FILE="${REPO_ROOT}/infra/.env"
LOG_FILE="${SCALP_LAUNCHER_LOG:-${SCRIPT_DIR}/scalp-launcher.log}"
SESSION_LOG="${SCALP_SESSION_LOG:-${SCRIPT_DIR}/scalp-session.log}"
LOCK_FILE="${SCALP_LAUNCHER_LOCK:-${SCRIPT_DIR}/scalp-launcher.lock}"
SCALP_SESSION_BIN="${SCALP_SESSION_BIN:-${SCRIPT_DIR}/scalp-session.sh}"
POLL_SECONDS="${SCALP_POLL_SECONDS:-10}"

MODE="once"
for arg in "$@"; do
  case "${arg}" in
    --loop) MODE="loop" ;;
    --once) MODE="once" ;;
    --dry-run) MODE="dry-run" ;;
    *) echo "usage: $0 [--once|--loop|--dry-run]" >&2; exit 2 ;;
  esac
done

log() { printf '%s [scalp-launcher] %s\n' "$(date -Is)" "$*" >>"${LOG_FILE}"; }

[[ -f "${ENV_FILE}" ]] || { echo "missing ${ENV_FILE}" >&2; exit 1; }
set -a; source "${ENV_FILE}"; set +a
TOKEN="${BOT_INGEST_TOKEN:-${NEWS_INGEST_TOKEN:-}}"
[[ -n "${TOKEN}" ]] || { echo "BOT_INGEST_TOKEN (or NEWS_INGEST_TOKEN) required" >&2; exit 1; }
API_BASE="${BOT_API_BASE_URL:-http://localhost/api}"

# Global single-flight: fail fast if another launcher holds the lock.
exec 200>"${LOCK_FILE}"
flock -n 200 || { log "another launcher holds ${LOCK_FILE}, exiting"; exit 1; }

api_get_pending() {
  curl -sS --max-time 10 -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/trades/pending-scalp"
}

api_claim() { # $1 = trade id -> prints HTTP code
  curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST \
    -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/trades/$1/scalp-claim"
}

api_finish() { # $1 = trade id, $2 = exit code -> prints HTTP code
  curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST \
    -H "X-Service-Token: ${TOKEN}" -H "Content-Type: application/json" \
    --data "{\"exitCode\": $2}" "${API_BASE}/bot/trades/$1/scalp-finish"
}

launch_session() { # $1 = trade id — detached; loop never blocks on the 15-min session
  local id="$1"
  (
    # setsid(1) waits for the session and returns its exit code, in a new session so a
    # launcher restart can never signal the running scalp session.
    # (Plain assignments: this subshell is not a function, `local` is illegal here.
    # The `||` guard is load-bearing: under inherited `set -e` a non-zero session
    # exit would otherwise abort the subshell before the finish post.)
    code=0
    setsid "${SCALP_SESSION_BIN}" "${id}" >>"${SESSION_LOG}" 2>&1 || code=$?
    http=$(api_finish "${id}" "${code}" || echo 000)
    log "trade ${id}: session exited code=${code}, scalp-finish HTTP ${http}"
  ) &
  log "trade ${id}: claimed, session launched (pid $!)"
}

poll_once() {
  local body ids id code
  body="$(api_get_pending)" || { log "ERROR: pending-scalp GET failed"; return 0; }
  ids="$(printf '%s' "${body}" | python3 -c '
import json,sys
try:
    obj = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for row in obj.get("data", []) or []:
    print(row.get("id"))
' || true)"
  [[ -n "${ids}" ]] || return 0
  while IFS= read -r id; do
    [[ -n "${id}" ]] || continue
    if [[ "${MODE}" == "dry-run" ]]; then
      code="$(api_claim "${id}" || echo 000)"
      if [[ "${code}" == "200" ]]; then
        log "DRY_RUN trade ${id}: claimed, would launch ${SCALP_SESSION_BIN} ${id} (no launch, no finish)"
      elif [[ "${code}" == "409" ]]; then
        log "DRY_RUN trade ${id}: already claimed (409), skipping"
      else
        log "DRY_RUN trade ${id}: claim HTTP ${code}"
      fi
      continue
    fi
    code="$(api_claim "${id}" || echo 000)"
    if [[ "${code}" == "200" ]]; then
      launch_session "${id}"
    elif [[ "${code}" == "409" ]]; then
      log "trade ${id}: already claimed/finished (409, someone else won), skipping"
    else
      log "ERROR: trade ${id}: claim HTTP ${code}"
    fi
  done <<<"${ids}"
}

log "start mode=${MODE} api=${API_BASE} session_bin=${SCALP_SESSION_BIN}"
if [[ "${MODE}" == "loop" ]]; then
  while true; do
    poll_once
    sleep "${POLL_SECONDS}"
  done
else
  poll_once
fi
log "end mode=${MODE}"
exit 0
