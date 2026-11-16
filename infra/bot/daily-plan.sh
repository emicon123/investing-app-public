#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." &>/dev/null && pwd)"
ENV_FILE="${REPO_ROOT}/infra/.env"
PROMPT_FILE="${SCRIPT_DIR}/prompt.md"
LOG_FILE="${SCRIPT_DIR}/daily-plan.log"
CLAUDE_BIN="${CLAUDE_BIN:-$HOME/.local/bin/claude}"
log() { printf '%s [bot-daily-plan] %s\n' "$(date -Is)" "$*" >>"${LOG_FILE}"; }
fail() { log "ERROR: $1"; echo "bot daily-plan: $1" >&2; exit 1; }
[[ -f "${ENV_FILE}" ]] || fail "missing env file: ${ENV_FILE}"
[[ -f "${PROMPT_FILE}" ]] || fail "missing prompt: ${PROMPT_FILE}"
set -a; source "${ENV_FILE}"; set +a
[[ -n "${BOT_INGEST_TOKEN:-}" ]] || [[ -n "${NEWS_INGEST_TOKEN:-}" ]] || fail "BOT_INGEST_TOKEN or NEWS_INGEST_TOKEN required"
TOKEN="${BOT_INGEST_TOKEN:-${NEWS_INGEST_TOKEN}}"
API_BASE="${BOT_API_BASE_URL:-${NEWS_API_BASE_URL:-http://localhost/api}}"
command -v curl >/dev/null 2>&1 || fail "curl required"
command -v python3 >/dev/null 2>&1 || fail "python3 required"
[[ -x "${CLAUDE_BIN}" ]] || fail "claude not found at ${CLAUDE_BIN}"
log "=== run start ==="
sync_code=$(curl -sS -o /tmp/bot-sync.$$.json -w '%{http_code}' -X POST -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/calendar/sync" || echo 000)
log "calendar sync HTTP ${sync_code}"
rm -f /tmp/bot-sync.$$.json
today=$(TZ=Europe/Warsaw date +%F)
week_to=$(TZ=Europe/Warsaw date -d "+7 days" +%F 2>/dev/null || date -v+7d +%F 2>/dev/null || echo "$today")

# Skip the paid `claude -p` call entirely on days with no HIGH/MEDIUM-impact USD event
# today — the whole point of this bot is trading specific Fed-moving releases,
# not producing a daily essay. Most days have nothing CPI/NFP/FOMC/PCE-shaped
# scheduled. bot_daily_plan.status='EMPTY' records that we checked and found
# nothing, distinct from the cron simply never having run.
today_high_usd_json=$(curl -sS -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/calendar?from=${today}&to=${today}&impact=HIGH,MEDIUM&currency=USD&size=50" || echo '{"page":{"total":0}}')
today_event_count=$(python3 -c 'import json,sys; obj=json.loads(sys.argv[1]); print(obj.get("page",{}).get("total",0))' "${today_high_usd_json}" 2>/dev/null || echo 0)
if [[ "${today_event_count}" -eq 0 ]]; then
  log "no HIGH/MEDIUM-impact USD events today (${today}) — skipping claude -p, posting EMPTY plan"
  empty_body=$(python3 -c 'import json,sys; print(json.dumps({"planDate": sys.argv[1], "status": "EMPTY", "summary": "No HIGH/MEDIUM-impact USD events today.", "scenarios": []}))' "${today}")
  code=$(curl -sS -o /tmp/bot-ingest.$$.json -w '%{http_code}' -X POST -H "X-Service-Token: ${TOKEN}" -H "Content-Type: application/json" --data "${empty_body}" "${API_BASE}/bot/plans/ingest" || echo 000)
  resp=$(cat /tmp/bot-ingest.$$.json 2>/dev/null); rm -f /tmp/bot-ingest.$$.json
  log "EMPTY plan ingest HTTP ${code}: ${resp}"
  log "=== run complete (no-op day) ==="
  exit 0
fi
log "${today_event_count} HIGH/MEDIUM-impact USD event(s) today — proceeding with claude -p"

cal_json=$(curl -sS -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/calendar?from=${today}&to=${week_to}&size=50" || echo '{"data":[]}')
macro_json=$(curl -sS -H "X-Service-Token: ${TOKEN}" "${API_BASE}/market-data/macro-state" || echo '{}')
prompt_template="$(cat "${PROMPT_FILE}")"
full_prompt="${prompt_template}
CALENDAR: ${cal_json}
MACRO_STATE: ${macro_json}
TODAY: ${today}"
log "invoking claude -p for ${today}"
claude_raw="$("${CLAUDE_BIN}" -p "${full_prompt}" --output-format json --allowedTools "WebSearch,WebFetch" --disallowedTools "Bash,Edit,Write")" || fail "claude -p failed"
is_error=$(python3 -c 'import json,sys; obj=json.loads(sys.argv[1]); print("true" if obj.get("is_error") else "false")' "${claude_raw}")
[[ "${is_error}" == "true" ]] && fail "claude error: ${claude_raw:0:800}"
result_text=$(python3 -c 'import json,sys; obj=json.loads(sys.argv[1]); print(obj.get("result",""))' "${claude_raw}")
plan_json=$(python3 -c '
import re,sys,json
text=sys.stdin.read().strip()
text=re.sub(r"^```(?:json)?\s*","",text)
text=re.sub(r"\s*```$","",text)
text=text.strip()
s=text.find("{")
e=text.rfind("}")
if s!=-1 and e!=-1:
    text=text[s:e+1]
sys.stdout.write(text)
' <<<"${result_text}")
echo "${plan_json}" | python3 -m json.tool >/dev/null 2>&1 || fail "invalid JSON from claude: ${plan_json:0:800}"
plan_date=$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read()).get("planDate",""))' <<<"${plan_json}")
[[ -z "${plan_date}" ]] && plan_date="${today}"
ingest_body=$(python3 -c 'import json,sys; obj=json.loads(sys.argv[1]); obj["planDate"]=sys.argv[2]; print(json.dumps(obj))' "${plan_json}" "${plan_date}")
log "posting plan for ${plan_date}"
code=$(curl -sS -o /tmp/bot-ingest.$$.json -w '%{http_code}' -X POST -H "X-Service-Token: ${TOKEN}" -H "Content-Type: application/json" --data "${ingest_body}" "${API_BASE}/bot/plans/ingest" || echo 000)
resp=$(cat /tmp/bot-ingest.$$.json); rm -f /tmp/bot-ingest.$$.json
if [[ "${code}" -lt 200 || "${code}" -ge 300 ]]; then fail "POST /bot/plans/ingest HTTP ${code}: ${resp}"; fi
log "ingest ok HTTP ${code}: ${resp}"
log "=== run complete ==="
