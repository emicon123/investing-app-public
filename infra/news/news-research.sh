#!/usr/bin/env bash
# infra/news/news-research.sh
#
# Nightly host-cron job (ADR-041), run at 06:15 Europe/Warsaw: researches today's news for
# each MAIN-portfolio asset with its own distinguishable, individually-researchable
# narrative (companyBacked = true, ADR-064,
# semantics broadened by ADR-079 — excludes only cash/FX rows and basket/index/commodity
# ETF vehicles; INCLUDES leaderless protocol-level assets like BTC-USD) via headless
# `claude -p --allowedTools WebSearch`, then pushes the structured result into the
# backend's machine-only ingest endpoint. Each asset in the fetched list also carries
# recentHistory (ADR-079) so the prompt can avoid re-reporting an ongoing story — this
# script forwards that field through unchanged, no reshaping needed. Running at 06:15
# (rather than 00:00, or the previous 23:00) keeps the research and the stamped `newsDate`
# safely clear of the midnight boundary, and — running in the early morning rather than
# late at night — captures the full prior day's developments, including the US market
# close and any overnight/Asian-session news, instead of potentially running before the
# US session has even finished for the day.
#
# Runs directly on the Raspberry Pi host (NOT in Docker) — the `claude` CLI lives on
# the host. See docs/adr/ADR-041-daily-asset-news-host-cron-claude-cli.md.
#
# Phone notification (Web Push, ADR-108; replaces ntfy/ADR-107): every run that got as far as
# loading infra/.env ends with exactly ONE call to the backend's machine-only
# POST ${API_BASE}/news/notifications (X-Service-Token) carrying a structured outcome —
# READY (with itemCount) / NO_NEWS / NO_ASSETS / FAILED — and the run date; an EXIT trap covers
# unexpected `set -e` aborts. The backend owns the Polish copy and fans the message out to every
# subscribed browser. Best-effort: a failed call (backend down, push not configured → 503) is
# logged and swallowed and never changes the exit code. The body holds only the outcome, a count
# and the run date — failure details stay in news-research.log.
#
# Usage: ./news-research.sh   (intended to be invoked by cron; safe to run manually)

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." &>/dev/null && pwd)"
ENV_FILE="${REPO_ROOT}/infra/.env"
PROMPT_FILE="${SCRIPT_DIR}/prompt.md"
LOG_FILE="${SCRIPT_DIR}/news-research.log"
CLAUDE_BIN="${CLAUDE_BIN:-$HOME/.local/bin/claude}"

log() {
  printf '%s [news-research] %s\n' "$(date -Is)" "$*" >>"${LOG_FILE}"
}

# Notification state (ADR-108, carried over from ADR-107), so a run notifies at most once:
#   idle  = infra/.env not loaded yet, the token and API base are unknown, nothing may be sent
#   armed = infra/.env loaded, the run's one notification is still owed
#   sent  = the run's notification call was made (or skipped, see notify)
notify_state="idle"

# notify <READY|NO_NEWS|NO_ASSETS|FAILED> [itemCount]
# Best-effort report of the run's outcome to POST ${API_BASE}/news/notifications; the backend
# maps it to the Polish copy and pushes it to every subscribed device (ADR-108 §2, §5). Marks the
# run as notified, never aborts the script and never changes its exit code, so every command
# that can fail is guarded. Retries ride out a backend restart during a deploy. Neither the
# token, the request headers nor the response body is written to the log — only the HTTP status
# and the delivery counts.
notify() {
  local outcome="$1" count="${2:-}"
  local body="" response="" http_code="" counts="" curl_rc=0 result
  notify_state="sent"

  if [[ -z "${NEWS_INGEST_TOKEN:-}" ]]; then
    result="push notify skipped (NEWS_INGEST_TOKEN not set)"
  elif ! body="$(python3 -c '
import json, sys
outcome, news_date, count = sys.argv[1:4]
body = {"outcome": outcome, "newsDate": news_date}
if count:
    body["itemCount"] = int(count)
json.dump(body, sys.stdout)
' "${outcome}" "${news_date}" "${count}" 2>/dev/null)"; then
    result="push notify failed (could not build the JSON body)"
  else
    response="$(curl -sS --max-time 30 --retry 2 --retry-delay 5 --retry-connrefused \
      -w '\n%{http_code}' \
      -X POST \
      -H "X-Service-Token: ${NEWS_INGEST_TOKEN}" \
      -H "Content-Type: application/json" \
      --data "${body}" \
      "${API_BASE}/news/notifications" 2>/dev/null)" || curl_rc=$?
    http_code="${response##*$'\n'}"
    if ((curl_rc != 0)); then
      result="push notify failed (${outcome}, curl exit ${curl_rc})"
    elif [[ "${http_code}" == "503" ]]; then
      result="push not configured (${outcome}, HTTP 503) — no notification sent"
    elif [[ "${http_code}" != 2[0-9][0-9] ]]; then
      result="push notify failed (${outcome}, HTTP ${http_code})"
    else
      counts="$(python3 -c '
import json, sys
d = json.loads(sys.stdin.read()).get("data") or {}
print(" ".join(f"{k}={int(d.get(k, 0))}" for k in ("attempted", "delivered", "pruned", "failed")))
' <<<"${response%$'\n'*}" 2>/dev/null)" || counts="report unparseable"
      result="push notify sent (${outcome}, HTTP ${http_code}, ${counts})"
    fi
  fi

  log "${result}" || true
  return 0
}

# The one failure notification. Does nothing before infra/.env is loaded (token unknown) or
# once the run has notified. Deliberately carries no failure detail — the backend sends only the
# fixed FAILED copy; the detail stays in the log.
notify_failure() {
  [[ "${notify_state}" == "armed" ]] || return 0
  notify FAILED
}

fail() {
  local msg="$1"
  log "ERROR: ${msg}"
  echo "news-research.sh: ${msg}" >&2
  notify_failure
  exit 1
}

# EXIT trap: catches an unexpected `set -e` abort (e.g. a python step crashing) that never went
# through fail(). Guarded and ended with `exit "$rc"` so the trap can't alter the exit code.
on_exit() {
  local rc=$?
  if ((rc != 0)); then
    notify_failure || true
  fi
  exit "${rc}"
}
trap on_exit EXIT

[[ -f "${ENV_FILE}" ]] || fail "missing env file: ${ENV_FILE} (copy infra/.env.example to infra/.env)"

# Source infra/.env to get NEWS_INGEST_TOKEN (and any base-URL-ish var). set -a exports
# every var defined while sourcing, so they're available to subprocesses too.
set -a
# shellcheck disable=SC1090
source "${ENV_FILE}"
set +a

# The host script talks to the API through the same nginx that serves the app
# (port 80 on localhost) — not through the Tailscale hostname, since the script runs
# on the Pi itself. This mirrors how infra/.env documents the API base path (/api).
# Set before the run is armed: the notification call needs it on every exit path.
API_BASE="${NEWS_API_BASE_URL:-http://localhost/api}"

# The run date (Europe/Warsaw), computed once here: every notification carries it, and step 4
# stamps it on each item as newsDate. From here on the token and API base are known, so every
# exit path owes the owner exactly one notification (ADR-108 §2).
news_date="$(TZ=Europe/Warsaw date +%F)"
notify_state="armed"

[[ -f "${PROMPT_FILE}" ]] || fail "missing prompt template: ${PROMPT_FILE}"
[[ -n "${NEWS_INGEST_TOKEN:-}" ]] || fail "NEWS_INGEST_TOKEN is not set in ${ENV_FILE}"

command -v curl >/dev/null 2>&1 || fail "curl is required but not found on PATH"
command -v python3 >/dev/null 2>&1 || fail "python3 is required but not found on PATH"
[[ -x "${CLAUDE_BIN}" ]] || fail "claude CLI not found or not executable at ${CLAUDE_BIN}"

log "=== run start ==="

# ── 1. Fetch the list of assets to research ─────────────────────────────────────
log "fetching asset list from ${API_BASE}/news/assets"

assets_http_code=$(curl -sS -o /tmp/news-research-assets.$$.json -w '%{http_code}' \
  -H "X-Service-Token: ${NEWS_INGEST_TOKEN}" \
  "${API_BASE}/news/assets") || fail "curl GET /news/assets failed (network error)"

assets_body="$(cat /tmp/news-research-assets.$$.json)"
rm -f /tmp/news-research-assets.$$.json

if [[ "${assets_http_code}" != "200" ]]; then
  fail "GET /news/assets returned HTTP ${assets_http_code}: ${assets_body}"
fi

# Extract the `data` array (list of {ticker, assetName}) from the envelope.
assets_json="$(python3 -c '
import json, sys
try:
    body = json.load(sys.stdin)
except json.JSONDecodeError as e:
    sys.stderr.write(f"invalid JSON from /news/assets: {e}\n")
    sys.exit(1)
data = body.get("data")
if not isinstance(data, list):
    sys.stderr.write("response missing top-level \"data\" array\n")
    sys.exit(1)
json.dump(data, sys.stdout)
' <<<"${assets_body}")" || fail "could not parse /news/assets response as the expected envelope"

asset_count="$(python3 -c 'import json,sys; print(len(json.load(sys.stdin)))' <<<"${assets_json}")"
log "fetched ${asset_count} asset(s) to research"

if [[ "${asset_count}" == "0" ]]; then
  log "no assets to research — exiting cleanly"
  notify NO_ASSETS
  exit 0
fi

# ── 2. Build the prompt and invoke claude -p ─────────────────────────────────────
prompt_template="$(cat "${PROMPT_FILE}")"
full_prompt="${prompt_template}
${assets_json}"

log "invoking claude -p (WebSearch only) for ${asset_count} asset(s)"

claude_raw="$("${CLAUDE_BIN}" -p "${full_prompt}" --output-format json --allowedTools "WebSearch")" \
  || fail "claude -p invocation failed (non-zero exit)"

# ── 3. Extract .result and validate it's the expected JSON array ────────────────
is_error="$(python3 -c '
import json, sys
try:
    obj = json.loads(sys.argv[1])
except json.JSONDecodeError as e:
    print("true")
    sys.exit(0)
print("true" if obj.get("is_error") else "false")
' "${claude_raw}")"

if [[ "${is_error}" == "true" ]]; then
  fail "claude -p returned an error or unparseable wrapper JSON: ${claude_raw:0:500}"
fi

result_text="$(python3 -c '
import json, sys
obj = json.loads(sys.argv[1])
sys.stdout.write(obj.get("result", ""))
' "${claude_raw}")" || fail "could not extract .result field from claude -p JSON wrapper"

# Strip markdown code fences if the model added them despite instructions
# (handles ```json ... ``` or plain ``` ... ```), then — since the model
# occasionally prepends/appends a stray sentence of commentary despite the
# prompt's "output ONLY a raw JSON array" instruction (observed 2026-08-24 and
# 2026-09-04) — extract the outermost [...] array by bracket-depth matching
# from the first "[" rather than requiring the entire trimmed output to be
# pure JSON. Falls back to the fence-stripped text unchanged if no "["
# is found, so the existing validation step still reports a clear error.
news_items_json="$(python3 -c '
import re, sys

text = sys.stdin.read().strip()
text = re.sub(r"^```(?:json)?\s*", "", text)
text = re.sub(r"\s*```$", "", text)
text = text.strip()

start = text.find("[")
if start != -1:
    depth = 0
    in_string = False
    escape = False
    end = None
    for i in range(start, len(text)):
        c = text[i]
        if in_string:
            if escape:
                escape = False
            elif c == "\\":
                escape = True
            elif c == "\"":
                in_string = False
        else:
            if c == "\"":
                in_string = True
            elif c == "[":
                depth += 1
            elif c == "]":
                depth -= 1
                if depth == 0:
                    end = i
                    break
    if end is not None:
        text = text[start:end + 1]

sys.stdout.write(text.strip())
' <<<"${result_text}")"

# Validate it parses as a JSON array, and that each element has the required fields.
validation_error="$(python3 -c '
import json, sys
required = {"ticker", "assetName", "headline", "summary", "sentiment", "sources"}
try:
    items = json.loads(sys.argv[1])
except json.JSONDecodeError as e:
    print(f"not valid JSON: {e}")
    sys.exit(0)
if not isinstance(items, list):
    print("top-level JSON is not an array")
    sys.exit(0)
for i, item in enumerate(items):
    if not isinstance(item, dict):
        print(f"item {i} is not an object")
        sys.exit(0)
    missing = required - item.keys()
    if missing:
        print(f"item {i} missing required field(s): {sorted(missing)}")
        sys.exit(0)
print("")
' "${news_items_json}")"

if [[ -n "${validation_error}" ]]; then
  # Log the full raw result (not just a snippet) — a truncated failure log isn't
  # recoverable later (hit 2026-09-04: a 1000-char cap cut off mid-URL and dropped
  # a whole item, making manual backfill from the log impossible).
  fail "claude -p result failed JSON validation: ${validation_error} — raw result: ${news_items_json}"
fi

item_count="$(python3 -c 'import json,sys; print(len(json.loads(sys.argv[1])))' "${news_items_json}")"
log "claude returned ${item_count} valid news item(s)"

if [[ "${item_count}" == "0" ]]; then
  log "no notable news today — nothing to ingest, exiting cleanly"
  notify NO_NEWS
  exit 0
fi

# ── 4. Wrap in IngestNewsRequest shape and POST to /news/ingest ─────────────────
# IngestNewsItem requires `newsDate` (the date the research covers — the host-script
# run date, Europe/Warsaw, per ADR-041 / openapi.yaml). The prompt doesn't ask Claude
# for this field, so stamp it on every item here rather than relying on the model
# (news_date was computed once, right after infra/.env was sourced).
ingest_body="$(python3 -c '
import json, sys
items = json.loads(sys.argv[1])
news_date = sys.argv[2]
for item in items:
    item["newsDate"] = news_date
json.dump({"items": items}, sys.stdout)
' "${news_items_json}" "${news_date}")"

log "posting ${item_count} item(s) to ${API_BASE}/news/ingest"

ingest_http_code=$(curl -sS -o /tmp/news-research-ingest.$$.json -w '%{http_code}' \
  -X POST \
  -H "X-Service-Token: ${NEWS_INGEST_TOKEN}" \
  -H "Content-Type: application/json" \
  --data "${ingest_body}" \
  "${API_BASE}/news/ingest") || fail "curl POST /news/ingest failed (network error)"

ingest_response="$(cat /tmp/news-research-ingest.$$.json)"
rm -f /tmp/news-research-ingest.$$.json

if [[ "${ingest_http_code}" -lt 200 || "${ingest_http_code}" -ge 300 ]]; then
  fail "POST /news/ingest returned HTTP ${ingest_http_code}: ${ingest_response}"
fi

log "ingest succeeded: HTTP ${ingest_http_code}, response: ${ingest_response}"
notify READY "${item_count}"
log "=== run complete ==="
