#!/usr/bin/env bash
# infra/bot/scalp-session.sh — continuous scalp-management session for an open trade.
#
# CORRECTED 2026-09-04: Claude does NOT do the 5s OKX polling itself anymore
# (the original "Wariant A" design — Claude in a raw `while sleep 5; do curl;
# done` loop with WebFetch/WebSearch access — is replaced). Timing and network
# I/O are deterministic (watch-market.sh); Claude's job is to call that script
# once per ~60s "beat", read back the aggregated window, decide
# HOLD/TRAIL_SL/PARTIAL_CLOSE/FULL_CLOSE, post the decision, and call the
# script again if it wants to keep watching ("stay in touch"/"hear the beat").
#
# Safety: the whole invocation is wrapped in `timeout` below as a
# defense-in-depth backstop against a hung/stuck Claude CLI process — it only
# kills this script's own `claude -p` process, it does NOT touch the OKX
# position. This is advisory management only. CORRECTED 2026-09-16 (see
# docs/adr/ADR-095-backend-hard-close-timeout.md): as of this writing there is
# NO automated time-based close of the position itself — the backend's own
# BotHardCloseWatcherService is designed (ADR-095) but not yet implemented.
# Until it ships, the only continuous, independent backstop is the
# exchange-side stop-loss placed at entry (price-based, not time-based); an
# unresponsive/erroring/never-launched session leaves the position open,
# protected only by that SL, indefinitely past 15 minutes. Once
# BotHardCloseWatcherService ships, update this comment to describe it as
# live, per this repo's "docs describe the system as it now IS" convention.
#
# Usage: ./scalp-session.sh <tradeId>
set -euo pipefail
TRADE_ID="${1:-}"; [[ -n "${TRADE_ID}" ]] || { echo "usage: $0 <tradeId>" >&2; exit 1; }
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." &>/dev/null && pwd)"
ENV_FILE="${REPO_ROOT}/infra/.env"
PROMPT_FILE="${SCRIPT_DIR}/manage-prompt.md"
LOG_FILE="${SCRIPT_DIR}/scalp-session.log"
CLAUDE_BIN="${CLAUDE_BIN:-$HOME/.local/bin/claude}"
HARD_TIMEOUT="${SCALP_HARD_TIMEOUT:-900}"  # 15m — kills this script's own claude CLI process
                                            # only; NOT a position-close backstop (see ADR-095;
                                            # the real one is backend BotHardCloseWatcherService,
                                            # designed but not yet shipped as of 2026-09-16)

log() { printf '%s [scalp-session] %s\n' "$(date -Is)" "$*" >>"${LOG_FILE}"; }
[[ -f "${ENV_FILE}" ]] || { echo "missing ${ENV_FILE}" >&2; exit 1; }
[[ -f "${PROMPT_FILE}" ]] || { echo "missing ${PROMPT_FILE}" >&2; exit 1; }
[[ -x "${CLAUDE_BIN}" ]] || { echo "claude not found at ${CLAUDE_BIN}" >&2; exit 1; }
set -a; source "${ENV_FILE}"; set +a
TOKEN="${BOT_INGEST_TOKEN:-${NEWS_INGEST_TOKEN:-}}"
API_BASE="${BOT_API_BASE_URL:-http://localhost/api}"

trade_json=$(curl -sS --max-time 5 -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/trades/${TRADE_ID}" 2>/dev/null || echo '{}')
stats_json=$(curl -sS --max-time 5 -H "X-Service-Token: ${TOKEN}" "${API_BASE}/bot/stats" 2>/dev/null || echo '{}')

prompt_template="$(cat "${PROMPT_FILE}")"
full_prompt="${prompt_template}

TRADE_ID: ${TRADE_ID}
TRADE: ${trade_json}
riskContext (best-effort, account-level, not strictly today-only): ${stats_json}

You have access to exactly one tool for market data: Bash, restricted to running
\`${SCRIPT_DIR}/watch-market.sh ${TRADE_ID} 60 5\` (one call = one ~60s beat, blocks
until it returns a JSON window) and \`curl\` to POST your decision to
\`${API_BASE}/bot/trades/${TRADE_ID}/actions\` with header
\`X-Service-Token: ${TOKEN}\`. Do not call any other URL yourself — all market data
comes from watch-market.sh's output, not from you fetching it directly.

Loop: call watch-market.sh, read the window, decide, POST the action. If your
decision is HOLD, call watch-market.sh again for the next beat — that is the
entire mechanism for 'staying in touch', you do not sleep or loop in any other
way. Stop and make your final decision FULL_CLOSE no later than beat ~14
(≈14 minutes elapsed since this session started) — a hard process timeout also
enforces this independently of you."

log "=== scalp session start for trade ${TRADE_ID} (hard timeout ${HARD_TIMEOUT}s) ==="
set +e
timeout "${HARD_TIMEOUT}" "${CLAUDE_BIN}" -p "${full_prompt}" --allowedTools "Bash(${SCRIPT_DIR}/watch-market.sh:*),Bash(curl:*)" --disallowedTools "WebSearch,WebFetch,Edit,Write" --output-format text >>"${LOG_FILE}" 2>&1
exit_code=$?
set -e
if [[ "${exit_code}" -eq 124 ]]; then
  log "WARNING: hard timeout (${HARD_TIMEOUT}s) killed the session before Claude reached FULL_CLOSE — this trade may still be open and unmanaged by this script. As of 2026-09-16 the only automated protection left is the entry-time exchange-side stop-loss; a backend time-based hard-close is designed (ADR-095) but not yet shipped. Until it ships, treat an overdue open trade found via this log as needing manual review (OKX UI / PATCH /bot/trades/{id}/close), not as auto-handled."
else
  log "session ended, exit_code=${exit_code}"
fi
log "=== scalp session end for trade ${TRADE_ID} ==="
exit 0
