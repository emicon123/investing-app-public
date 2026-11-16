#!/usr/bin/env bash
# On-demand second run: after position opened, bot calls this with tradeId and current market snapshot.
# Usage: ./manage-position.sh <tradeId> [price] [volume] [funding]
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." &>/dev/null && pwd)"
ENV_FILE="${REPO_ROOT}/infra/.env"
PROMPT_FILE="${SCRIPT_DIR}/manage-prompt.md"
LOG_FILE="${SCRIPT_DIR}/manage.log"
CLAUDE_BIN="${CLAUDE_BIN:-$HOME/.local/bin/claude}"

TRADE_ID="${1:-}"
[[ -n "${TRADE_ID}" ]] || { echo "usage: $0 <tradeId>" >&2; exit 1; }

[[ -f "${ENV_FILE}" ]] || { echo "missing ${ENV_FILE}" >&2; exit 1; }
[[ -f "${PROMPT_FILE}" ]] || { echo "missing ${PROMPT_FILE}" >&2; exit 1; }
set -a; source "${ENV_FILE}"; set +a
TOKEN="${BOT_INGEST_TOKEN:-${NEWS_INGEST_TOKEN:-}}"
[[ -n "${TOKEN}" ]] || { echo "BOT_INGEST_TOKEN required" >&2; exit 1; }
API_BASE="${BOT_API_BASE_URL:-http://localhost/api}"

trade_json=$(curl -sS -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/trades/${TRADE_ID}" 2>/dev/null || echo "{}")
ticks_json=$(curl -sS -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/trades/${TRADE_ID}/ticks" 2>/dev/null || echo '{"data":[]}')
stats_json=$(curl -sS -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/stats" 2>/dev/null || echo '{}')
prompt_template="$(cat "${PROMPT_FILE}")"
full_prompt="${prompt_template}
TRADE: ${trade_json}
TICKS: ${ticks_json}
TRADE_ID: ${TRADE_ID}
riskContext (best-effort, account-level, not strictly today-only): ${stats_json}"

claude_raw="$("${CLAUDE_BIN}" -p "${full_prompt}" --output-format json --allowedTools "WebSearch" --disallowedTools "Bash,Edit,Write")" || exit 1
result=$(python3 -c 'import json,sys; obj=json.loads(sys.argv[1]); print(obj.get("result",""))' "${claude_raw}")
# expect JSON like {"actionType":"SET_SL","price":...,"reasoning":"..."}
action_json=$(python3 -c '
import re,sys,json
t=sys.stdin.read().strip()
import re
t=re.sub(r"^```(?:json)?\s*","",t)
t=re.sub(r"\s*```$","",t)
s=t.find("{")
e=t.rfind("}")
if s!=-1 and e!=-1: t=t[s:e+1]
sys.stdout.write(t)
' <<<"${result}")

echo "${action_json}" | python3 -m json.tool >/dev/null || { echo "invalid action JSON: ${action_json}" >&2; exit 1; }
# POST to actions endpoint
curl -sS -X POST -H "X-Service-Token: ${TOKEN}" -H "Content-Type: application/json" --data "${action_json}" "${API_BASE}/bot/trades/${TRADE_ID}/actions" | cat
echo "" | tee -a "${LOG_FILE}"
