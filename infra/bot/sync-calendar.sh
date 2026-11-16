#!/usr/bin/env bash
# infra/bot/sync-calendar.sh — calendar-only refresh, no Claude call, no cost.
# Keeps bot_calendar_event fresh independent of whether daily-plan.sh (which
# also re-syncs as its own first step, then spends a paid `claude -p` call)
# is scheduled. Run this on its own 05:50 cron entry per the architecture doc.
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." &>/dev/null && pwd)"
ENV_FILE="${REPO_ROOT}/infra/.env"
LOG_FILE="${SCRIPT_DIR}/sync-calendar.log"
log() { printf '%s [sync-calendar] %s\n' "$(date -Is)" "$*" >>"${LOG_FILE}"; }
[[ -f "${ENV_FILE}" ]] || { log "ERROR: missing ${ENV_FILE}"; exit 1; }
set -a; source "${ENV_FILE}"; set +a
TOKEN="${BOT_INGEST_TOKEN:-${NEWS_INGEST_TOKEN:-}}"
[[ -n "${TOKEN}" ]] || { log "ERROR: NEWS_INGEST_TOKEN required"; exit 1; }
API_BASE="${BOT_API_BASE_URL:-http://localhost/api}"
code=$(curl -sS -o /tmp/bot-sync-cron.$$.json -w '%{http_code}' -X POST -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/calendar/sync" || echo 000)
resp=$(cat /tmp/bot-sync-cron.$$.json 2>/dev/null); rm -f /tmp/bot-sync-cron.$$.json
log "HTTP ${code}: ${resp}"
