#!/usr/bin/env bash
# infra/bot/watch-market.sh — deterministic OKX market poller. NOT an LLM.
#
# Samples public OKX market data every ${INTERVAL}s for ${DURATION}s (default
# 60s / 5s = 12 samples — one "beat"), prints a compact JSON summary to
# stdout, and best-effort posts the raw samples to POST /bot/trades/{id}/ticks
# for storage (mini-chart on the trade detail drawer, ADR-note: bot module).
#
# Why this exists: Claude should not itself be the thing doing a tight 5s
# network-polling loop (unreliable timing inside a single model turn, wastes
# tokens/latency per tick, and gives an LLM unnecessary raw Bash+network
# access during a live leveraged position). Instead, the deterministic parts
# (timing, HTTP, arithmetic) live here; Claude's job (see manage-prompt.md /
# scalp-session.sh) is to call this script once per "beat", read back ~60s of
# aggregated market data, and decide HOLD/TRAIL_SL/PARTIAL_CLOSE/FULL_CLOSE —
# then call this script again if it wants to keep watching. One script call
# per beat, not one curl per tick.
#
# Usage: ./watch-market.sh <tradeId> [durationSeconds=60] [intervalSeconds=5]
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." &>/dev/null && pwd)"
ENV_FILE="${REPO_ROOT}/infra/.env"

TRADE_ID="${1:-}"
DURATION="${2:-60}"
INTERVAL="${3:-5}"
[[ -n "${TRADE_ID}" ]] || { echo "usage: $0 <tradeId> [durationSeconds] [intervalSeconds]" >&2; exit 1; }

INST_ID="BTC-USD-SWAP"
command -v curl >/dev/null 2>&1 || { echo "curl required" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "python3 required" >&2; exit 1; }

[[ -f "${ENV_FILE}" ]] && { set -a; source "${ENV_FILE}"; set +a; }
TOKEN="${BOT_INGEST_TOKEN:-${NEWS_INGEST_TOKEN:-}}"
API_BASE="${BOT_API_BASE_URL:-http://localhost/api}"

samples_file="$(mktemp)"
trap 'rm -f "${samples_file}"' EXIT
echo "[]" >"${samples_file}"

# Slow-moving fields (funding rate, long/short ratio, open interest) — fetch
# once at the start of the window, not every 5s; OKX rate limits and these
# don't meaningfully change within 60s.
funding_json=$(curl -sS --max-time 4 "https://eea.okx.com/api/v5/public/funding-rate?instId=${INST_ID}" 2>/dev/null || echo '{}')
ratio_json=$(curl -sS --max-time 4 "https://eea.okx.com/api/v5/rubik/stat/contracts/longShortAccountRatio?ccy=BTC&period=5m" 2>/dev/null || echo '{}')
oi_json=$(curl -sS --max-time 4 "https://eea.okx.com/api/v5/public/open-interest?instId=${INST_ID}" 2>/dev/null || echo '{}')

iterations=$(( DURATION / INTERVAL ))
[[ "${iterations}" -lt 1 ]] && iterations=1

for ((i = 0; i < iterations; i++)); do
  ticker_json=$(curl -sS --max-time 4 "https://eea.okx.com/api/v5/market/ticker?instId=${INST_ID}" 2>/dev/null || echo '{}')
  books_json=$(curl -sS --max-time 4 "https://eea.okx.com/api/v5/market/books?instId=${INST_ID}&sz=5" 2>/dev/null || echo '{}')
  python3 - "${samples_file}" "${ticker_json}" "${books_json}" <<'PYEOF'
import json, sys, time
samples_file, ticker_raw, books_raw = sys.argv[1], sys.argv[2], sys.argv[3]

def safe(raw):
    try:
        return json.loads(raw)
    except Exception:
        return {}

ticker = safe(ticker_raw)
books = safe(books_raw)

price = vol24h = None
try:
    d = ticker.get("data", [{}])[0]
    price = float(d.get("last")) if d.get("last") else None
    vol24h = float(d.get("volCcy24h")) if d.get("volCcy24h") else None
except Exception:
    pass

ob_imbalance = None
try:
    b = books.get("data", [{}])[0]
    bid_sz = sum(float(x[1]) for x in b.get("bids", []))
    ask_sz = sum(float(x[1]) for x in b.get("asks", []))
    if bid_sz + ask_sz > 0:
        ob_imbalance = round((bid_sz - ask_sz) / (bid_sz + ask_sz), 4)
except Exception:
    pass

with open(samples_file) as f:
    samples = json.load(f)
samples.append({"t": int(time.time()), "price": price, "vol24h": vol24h, "obImbalance": ob_imbalance})
with open(samples_file, "w") as f:
    json.dump(samples, f)
PYEOF
  if [[ $((i + 1)) -lt "${iterations}" ]]; then
    sleep "${INTERVAL}"
  fi
done

summary_json=$(python3 - "${samples_file}" "${funding_json}" "${ratio_json}" "${oi_json}" "${TRADE_ID}" "${DURATION}" <<'PYEOF'
import json, sys

samples_file, funding_raw, ratio_raw, oi_raw, trade_id, duration = sys.argv[1:7]

def safe(raw):
    try:
        return json.loads(raw)
    except Exception:
        return {}

with open(samples_file) as f:
    samples = json.load(f)

prices = [s["price"] for s in samples if s.get("price") is not None]
vols = [s["vol24h"] for s in samples if s.get("vol24h") is not None]
obs = [s["obImbalance"] for s in samples if s.get("obImbalance") is not None]

price_delta = round(prices[-1] - prices[0], 2) if len(prices) >= 2 else None
price_delta_pct = round((prices[-1] / prices[0] - 1) * 100, 4) if len(prices) >= 2 and prices[0] else None
price_range = round(max(prices) - min(prices), 2) if prices else None

volume_trend = "unknown"
if len(vols) >= 4:
    half = len(vols) // 2
    first_avg = sum(vols[:half]) / half
    second_avg = sum(vols[half:]) / (len(vols) - half)
    if first_avg > 0:
        change = (second_avg - first_avg) / first_avg
        volume_trend = "rising" if change > 0.05 else "falling" if change < -0.05 else "flat"

funding = safe(funding_raw)
ratio = safe(ratio_raw)
oi = safe(oi_raw)

def first(d, default=None):
    try:
        return d.get("data", [default])[0]
    except Exception:
        return default

funding_rate = None
try:
    funding_rate = float(first(funding, {}).get("fundingRate"))
except Exception:
    pass

long_short_ratio = None
try:
    r = first(ratio, [None, None])
    long_short_ratio = float(r[1]) if r and len(r) > 1 else None
except Exception:
    pass

open_interest = None
try:
    open_interest = float(first(oi, {}).get("oi"))
except Exception:
    pass

out = {
    "tradeId": int(trade_id),
    "windowSeconds": int(duration),
    "sampleCount": len(samples),
    "samples": samples,
    "trend": {
        "priceDelta": price_delta,
        "priceDeltaPct": price_delta_pct,
        "priceRange": price_range,
        "volumeTrend": volume_trend,
        "avgObImbalance": round(sum(obs) / len(obs), 4) if obs else None,
    },
    "fundingRate": funding_rate,
    "longShortRatio": long_short_ratio,
    "openInterest": open_interest,
    "note": "cvd/atr1m are NOT computed here (need trade-tape/candle history beyond this script's scope) — treat trend.priceRange as an ATR proxy and reason about volume/price/orderbook trend qualitatively.",
}
print(json.dumps(out))
PYEOF
)

echo "${summary_json}"

# Best-effort: also persist raw samples as ticks (mini-chart / audit trail).
# Never let a ticks-storage failure break the watch call itself.
if [[ -n "${TOKEN}" ]]; then
  ticks_body=$(python3 - "${samples_file}" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    samples = json.load(f)
ticks = [{"price": s["price"], "volume": s.get("vol24h"), "obImbalance": s.get("obImbalance")} for s in samples if s.get("price") is not None]
print(json.dumps({"ticks": ticks}))
PYEOF
  )
  curl -sS --max-time 5 -o /dev/null -X POST -H "X-Service-Token: ${TOKEN}" -H "Content-Type: application/json" \
    --data "${ticks_body}" "${API_BASE}/bot/trades/${TRADE_ID}/ticks" || true
fi
