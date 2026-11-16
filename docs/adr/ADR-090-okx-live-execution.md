# ADR-090: OKX Live Execution — Automated Entries, Exchange-Side Risk, Position Sizing

**Date:** 2026-09-10
**Status:** Accepted
**Related:** `.claude/tasks/141-okx-live-execution.task.md` (design draft this ADR rules on),
`.claude/tasks/bot-okx-tracking.task.md` (tracking/planning layer — out of scope here),
`docs/adr/ADR-089-bot-cron-get-machine-auth.md` (machine-auth pattern, kill-switch auth scoping),
`docs/architecture/01-system-context.md`, `03-api-contract.md`, `06-data-flow.md`,
`infra/bot/prompt.md` (scenario ladder + `surpriseMin`/`surpriseMax`/`confidence` contract),
`backend/.../service/bot/` (`BotTradeService`, `BotSignalService`, `BotPlanService`,
`ForexFactorySyncService`), `V63`/`V64`/`V65` (bot schema baseline, leverage cap 1–10),
`infra/.env.example` (OKX keys already present), `infra/compose.yml` (OKX keys NOT forwarded —
gap closed here)

## Context

The bot module's tracking/planning layer exists (calendar sync 05:50, 06:00 Claude daily plan,
signal/trade/action APIs, `/bot` UI). The missing piece is live execution: turning a matched
release-time scenario into a real OKX order with no human in the loop (explicit user decision,
2026-09-04). This is the highest-risk code in the module (real leveraged capital, no manual gate),
so the draft in task 141 needed an architect ruling on five points before Backend builds: numeric
parsing rules, watcher threading vs. Fair Economy rate limits, cluster-netting confirmation, OKX
client scope, and sizing numbers with a liquidation-safety proof. This ADR is that ruling.

Fixed inputs (user decisions, not re-litigated): fully automated entries; exchange-side algo SL
placed at entry, and **if the SL call fails after a filled entry the position is closed
immediately**; confidence-scaled sizing with bounded account risk (numbers below are this ADR's,
not the user's); kill switch stays manual-only; credentials live in `infra/.env`, never in code.

## Decision

### 1. `NumericEventValueParser` — unit-preserving, fail-closed to HOLD

`bot_calendar_event.forecast`/`actual` are display-unit text (`"213K"`, `"0.3%"`, `"-"`).
`prompt.md` requires Claude to author `surpriseMin`/`surpriseMax` in **those same display units**
(NFP in thousands → `213`, not `213000`; CPI in percent → `0.3`, not `0.003`). The parser must
therefore be **unit-preserving, not SI-expanding**: its only job is to make
`surprise = parse(actual) − parse(forecast)` comparable to the scenario bounds.

Rules (in order):

1. Trim; `null`/blank/`"-"`/`"n/a"` (case-insensitive) → empty.
2. Strip thousands commas (`"1,234"` → `1234`). Strip a single leading `+`.
3. Strip exactly one known trailing suffix, case-insensitive: `K`, `M`, `B`, `%`. **Do not
   multiply** — `"213K"` → `213.0`, `"0.3%"` → `0.3`. (`%` events therefore compute surprise in
   percentage *points*, which is what the prompt's ladders assume.)
4. Parse remainder as decimal (sign + digits + optional dot). Anything left over
   (`"$1.2M"`, `"50.2 (rev)"`, `"TBD"`) → empty. One exception: strip a single leading `$` before
   step 3 (some feeds prefix dollar figures); anything else non-numeric → empty.
5. Return `Optional<BigDecimal>`. **Empty on either side → surprise undefined → no scenario
   matches → record a HOLD signal** (`surpriseDesc` explains the unparseable print) and place no
   order. A parse failure is an expected data gap, never an exception and never a new HTTP 422 —
   §7 explains why no public API surface changes for this.

Matcher boundary rule (deterministic, documented for Backend): iterate the event's scenarios in
fixed ladder order (`EXTREME_MISS` → `EXTREME_BEAT`); first scenario with
`(min == null || surprise >= min) && (max == null || surprise <= max)` wins. Shared contiguous
boundaries (e.g. `-100` as both `EXTREME_MISS.max` and `MISS.min`) therefore always resolve to the
more extreme ladder rung — a deliberate, stable tie-break, not an accident.

### 2. `EventReleaseWatcherService` — one `@Scheduled` 1-second DB tick, outbound refresh gated

Three shapes were considered: (a) per-event dedicated threads/executors, (b) a 1-second poll of
the Fair Economy feed inside each watch window, (c) a 1-second poll of the **local DB** with a
separately-gated outbound refresh. **Decided: (c).**

- (a) rejected: events are sparse (a handful per day, known since 06:00), all clusters in one tick
  evaluate in microseconds, and sequential evaluation *is* the max-1-concurrent enforcement. New
  thread pools on a 768 MB Pi for no concurrency gain is speculative complexity.
- (b) rejected outright: 1-second outbound polling will get rate-limited. Proven fact, not theory
  — manual testing (a few rapid requests) already earned a `429` with `Retry-After ≈ 5 min` from
  `nfs.faireconomy.media`. A `T−30s → T+5min` window at 1 s is ~330 outbound calls per event.
- (c) decided: a single `@Scheduled(fixedDelay = 1000ms)` tick on the existing scheduler
  (`@EnableScheduling` already on `InvestingAppApplication`; no new pool — same shape as
  `HistoriaScheduler`/`MacroDataScheduler`). Each tick:
  1. Loads today's plan scenarios (plan_date = today, Europe/Warsaw) whose `eventTime` is inside
     an active watch window (defaults: `watch-before = 30 s`, `watch-after = 300 s`).
  2. If any window is active **and** the last outbound refresh is older than
     `actuals-refresh = 20 s` **and** no refresh is currently in flight (`AtomicBoolean` gate, so
     a slow 10 s upstream call never piles ticks), calls `ForexFactorySyncService.sync()` to pull
     fresh `actual`s into `bot_calendar_event`. Honors `429`/`Retry-After` with backoff (default
     5 min when the header is absent); while backing off, the tick keeps evaluating DB state.
     Worst case ≈ 2 feed calls / 20 s ≈ 33 calls per window — two orders of magnitude under (b),
     and every match decision reads DB state, so match latency stays ~1 s regardless of refresh
     cadence.
  3. Evaluates matches from DB state (§1), resolves clusters (§3), and hands net decisions to the
     execution path (§5–§6), which reuses `BotTradeService`/`BotSignalService` so tracking rows
     stay the single source of truth.

Restart/idempotency: ticks ignore anything with `eventTime < now − watch-after` (a restart never
resurrects a stale window); in-memory resolved-cluster set is a fast path only — the DB guards in
§6 (`OPEN` exists check, 60 s duplicate guard, per-event `executed` flags) are the real
exactly-once enforcement and survive restarts.

### 3. Same-timestamp clustering — confirm the draft, two adjustments

The draft's netting logic is confirmed (it matches the verified real data: Core PPI + PPI +
Claims genuinely share 12:30 UTC; `prompt.md` already scores each event independently with its own
ladder/leverage/confidence). Adjustments:

- **Grouping key is exact `event_time` equality** (`Instant`). `prompt.md` requires `eventTime` to
  match the calendar exactly, so no tolerance window is needed — and a tolerance would risk
  merging genuinely distinct releases. `BotPlanService` should log a warning at ingest if two
  scenarios share an `eventName`+`eventTime` but different bounds (authoring smell, not a reject).
- **Numbers fixed:** cluster wait cap `20 s` past the first posted `actual` (inside the draft's
  15–30 s); missing sibling contributes `0`, logged as a data gap, never guessed. Signed leverage
  (`LONG → +lev`, `SHORT → −lev`, `HOLD`/no-match → `0`), summed; `|sum| < 1` → HOLD (near-cancel
  isn't worth a leftover); else `netAction = sum > 0 ? LONG : SHORT`,
  `netLeverage = min(10, round(|sum|))` — the V65 1–10 cap applies to net leverage exactly as to
  single-event leverage. Net confidence = mean of contributing non-zero confidences (simplest
  defensible choice; revisit only with live data, not on principle).

Record shape (this is the one place the draft under-specified): **one `bot_signal` per evaluated
event** (decision = its own per-event match, `executed = true` only on contributors when an order
is actually placed), **exactly one `bot_trade`** for the net position with `signal_id = NULL` and
`calendar_event_id = NULL` (both already nullable — a cluster trade belongs to no single event)
and a machine-readable audit line in `notes`
(e.g. `cluster 2026-09-10T12:30:00Z net LONG x2 [PPI MISS LONG x5, Claims MISS SHORT x3];
confidence 0.45; signals [12,13,14]`). A single-event "cluster" runs the identical code path —
no special-casing. No schema change is needed for clustering itself (confirmed as the draft
claimed); the only new `bot_trade` columns are the sizing/Algo audit trail in §7.

The 60-second duplicate guard is confirmed as **defense-in-depth, not the primary mechanism** —
it must never fire if netting is correct; if it ever does, it logs at ERROR (with the cluster
audit) so the misfire is investigated, not silently absorbed.

### 4. `OkxTradingClient` — minimal signed surface, signer separated, demo-first

Scope (OKX API v5, instrument `BTC-USDT-SWAP`, one-way/net mode — no `posSide` complexity, correct
because §6 caps at 1 concurrent trade):

| Call | Endpoint | Purpose |
|---|---|---|
| equity | `GET /api/v5/account/balance` | USDT equity → sizing input |
| leverage | `POST /api/v5/account/set-leverage` (`instId`, `lever`, `mgnMode=isolated`) | set net leverage pre-entry |
| entry | `POST /api/v5/trade/order` (market, `tdMode=isolated`, `side buy/sell`, `sz` in contracts) | market entry |
| stop | `POST /api/v5/trade/order-algo` (conditional SL, market close on trigger) | exchange-side protection at entry |
| verify | `GET /api/v5/account/positions` | confirm fill / confirm flat after emergency close |

Isolated (not cross) margin is deliberate: worst case is bounded by posted margin, matching the
`marginAllocatedPln` mental model in §5. HMAC-SHA256 signing per OKX v5 (`timestamp + method +
requestPath + body`, Base64; `OK-ACCESS-KEY`/`OK-ACCESS-SIGN`/`OK-ACCESS-TIMESTAMP`/
`OK-ACCESS-PASSPHRASE` headers) lives in a standalone `OkxSigner` pure-function component
(unit-testable with no HTTP); the client follows the `BinanceClient` shape (`RestClient`,
5 s connect / 10 s read timeouts) with per-request signed headers. Base URL defaults to
`https://eea.okx.com` (EEA accounts get `50119 API key doesn't exist` on `www.okx.com` —
verified live 2026-09-10; override via `OKX_BASE_URL`) but is constructor-injected (the `ForexFactorySyncServiceIT` WireMock +
`@DynamicPropertySource` URL-override pattern must work for this client — that is how it gets
tested). `OKX_DEMO_MODE=true` sends `x-simulated-trading: 1`; the property **defaults to `true`**
so a missing env var can never default the backend to live trading.

Open verification items for Backend (not assumed here): contracts-to-BTC conversion (`sz` needs
`ctVal` from `GET /api/v5/public/instruments` — do not hardcode the contract size); exact algo-SL
field set (`slTriggerPx`/`slOrdPx="-1"` semantics) verified in demo; whether demo trading needs
separately-created demo API keys (if so, document the key-creation step in the handoff file, the
code path is unchanged — same three env vars).

### 5. `BotPositionSizingService` — formula accepted, joint constraint nailed down

Formula (accepted with the draft's constants as starting values, all `@ConfigurationProperties`,
all tunable without a deploy):

```
confidenceMultiplier(c) = clamp(c / 0.5, 0.5, 1.5)      // 0.5→1.0x; 1.0→1.5x; 0.3→0.6x
margin = equity × baseRiskPct(1%) × confidenceMultiplier(confidence)
margin = clamp(margin, equity × minRiskPct(0.3%), equity × maxRiskPct(3%))
quantity = margin / entryPrice                            // BTC; matches close()'s
                                                          // pnl = priceDelta × quantity × leverage
```

Sizing works natively in account currency (USDT); `margin_allocated_pln` is converted at order
time via `FxRateCacheService.getRateToPln(USD)` purely as an audit column. (Pre-existing debt,
flagged not fixed: `BotTradeService.close()` labels quote-currency PnL as `pnl_pln` with no FX
leg — sizing must not compound this by inventing its own second conversion; one conversion, at
entry, into the audit column only.)

Liquidation-safety joint rule (the draft's open item — decided): the entry SL is placed at a
fixed adverse distance `stopDistancePct` (default `1.0%`, configurable), and sizing + SL must
**jointly** satisfy `leverage × stopDistancePct ≤ 30%` (configurable `leverage-loss-cap-pct`).
Resolution order on violation is fixed: **reduce leverage first** (down to a floor of 1); if still
violated, **skip the trade** (log + HOLD signal). Never widen the stop, never add margin, to
satisfy the constraint. Sanity proof at defaults: max case `3% equity × 10x × 1% stop` loses
`0.3%` of equity if the SL fills, and `10 × 1% = 10% ≤ 30%` — the cap is a tripwire that never
binds at defaults, exactly as a safety invariant should. (For scale: at 10x isolated, OKX
liquidation sits ≈10% adverse — the 1% SL is an order of magnitude inside it.)

### 6. One choke point: guards live in `BotTradeService.create()`, kill switch reused as-is

- **Max 1 concurrent open trade (MVP)** and the **60 s duplicate guard** are enforced inside
  `BotTradeService.create()` — the single choke point every entry path (watcher-internal and any
  future external `POST /bot/trades`) funnels through — not in the watcher. Both refusals are
  `409 Conflict` with distinct messages (new conditions on an existing status, see §7). The
  watcher treats a guard refusal as skip + log (ERROR for the duplicate guard, WARN for
  max-concurrent), never a retry.
- **Kill switch reused, not bypassed, not extended:** the watcher checks
  `bot_control.trading_enabled` (same `assertTradingEnabled` semantics) before sizing; a paused
  switch means signals recorded with `executed=false` and no order — identical to today's
  `POST /bot/trades` behavior. No auto-pause threshold (deferred per user decision; needs live
  track record to size).
- **Failure modes (fail-closed, in order):** entry rejected/error → no trade, signal stays
  `executed=false`. Entry filled but SL-algo call fails → **market-close the position immediately
  via the client, verify flat via position query, persist the trade as `CLOSED` with `notes`
  recording `SL_PLACE_FAILED_EMERGENCY_CLOSED`** (the user's explicit decision — an unprotected
  leveraged position must never survive Pi-side code). Emergency-close failure itself → log at
  ERROR with order/position ids; nothing further code can do (human Kill-switch + OKX UI path).

### 7. Schema (V69), API delta (none on the wire), config

- **Flyway `V69__bot_live_execution.sql`** (next free — head is V68): nullable
  `surprise_min`/`surprise_max NUMERIC(18,4)` + nullable `confidence NUMERIC(3,2)
  CHECK (0..1)` on `bot_plan_scenario` (nullability is load-bearing: HOLD legs carry null
  leverage/confidence per the prompt contract; outermost ladder bounds are null); nullable
  `okx_algo_id VARCHAR(64)`, `margin_allocated_pln NUMERIC(18,4)`,
  `sizing_confidence NUMERIC(3,2)` on `bot_trade` (names confirmed as proposed — `okx_algo_id`
  parallels the existing `okx_order_id`; `margin_allocated_pln` matches the `pnl_pln` naming
  convention); one new index `idx_bot_scenario_event_time ON bot_plan_scenario(event_time)`
  (the watcher's hot query — the only existing scenario index is on `plan_id`); one composite
  `idx_bot_trade_status_opened ON bot_trade(status, opened_at DESC)` for the two guards (keeps
  them index-only; existing single-column indexes stay). Compatible with V63 baseline + V65
  1–10 caps — no constraint touched.
- **No `docs/openapi.yaml` / `components/` edit in this change** — stated explicitly per the
  task: the watcher is internal (in-process service calls, no HTTP to self, hence no new paths),
  and the entire bot module still has zero OpenAPI paths (pre-existing gap, ADR-089 §Consequences
  — filling it is a separate pass, not this one). What *does* change is documented as snippets in
  `.claude/tasks/141-architect-output.md`: ingest/scenario/trade DTO field additions, the two new
  `409` conditions on `POST /bot/trades`, and ingest `422` cases (directional scenario without
  `confidence`/valid `leverage`, `surpriseMin > surpriseMax`, HOLD with non-null leverage).
- **Config gap found and closed here:** `infra/.env.example` already defines
  `OKX_API_KEY`/`SECRET`/`PASSPHRASE` + `OKX_DEMO_MODE=true`, but `infra/compose.yml` never
  forwards any of them into the backend container — live execution would boot with empty
  credentials regardless of `.env`. Backend adds `app.okx.*` + `app.bot.execution.*`
  `@ConfigurationProperties` (exact key list in the handoff file) and DevOps adds the four
  `OKX_*` passthrough lines to compose.

## Consequences

- Backend implements against `.claude/tasks/141-architect-output.md` (exact SQL, DTO deltas,
  file-by-file checklist) with no re-design; any deviation from the numbers above (risk pcts,
  stop distance, 30% cap, 20 s/60 s/1 s cadences) is a conscious ADR amendment, not a tuning
  commit — these numbers move real money.
- **Demo-mode gate (hard requirement):** the full cycle (plan → release → match → entry → SL →
  verify) runs against `OKX_DEMO_MODE=true` first, including at least one real calendar-release
  cycle, before the first `false` flip. The flip itself is env-only + backend restart, no code.
- Test plan (Testcontainers PG per CLAUDE.md, WireMock per `ForexFactorySyncServiceIT`):
  pure-unit `NumericEventValueParser` table tests and matcher/netting/sizing tests (Backend must
  extract `matchSurprise`/`netCluster`/sizing math as side-effect-free functions — untestable
  private tangle is a review failure); Testcontainers ITs for ingest validation, single-match
  execution, cluster single-order, kill-switch skip, guard 409s, SL-fail emergency close (stubbed
  `OkxTradingClient`); WireMock ITs for signing headers, endpoint shapes, error mapping.
- Frontend: no required change for MVP (new columns are additive; `shared-ts` `bot.ts` gains
  optional fields when convenient). Kill-switch UI already shipped.
- Docs: `03-api-contract.md` / `04-db-schema.md` bot sections remain unwritten (ADR-089's
  deferred gap grows by exactly the §7 delta — acceptable, tracked once, not twice).

## Amendment 2026-09-10 — coin-margined instrument (BTC+USDC account, no USDT)

User decision: the account holds BTC + USDC only, no USDT. Rulings adjusted, nothing else
in this ADR changes:

- Instrument is `BTC-USD-SWAP` (coin-margined, margined/settled in BTC, 1 contract = 100 USD
  notional), not `BTC-USDT-SWAP`. All five client calls keep their shapes, only `instId`
  changes; `ctVal` is still read live from `GET /api/v5/public/instruments`, never hardcoded.
- Equity input is unchanged code (`getEquityUsdt()` reads `totalEq` first — portfolio USD value,
  holding-agnostic, works with zero USDT). Sizing math is unchanged (USD-native).
- Contract conversion is notional-based in lotSz steps: `contracts = floor(qtyBtc × price / ctVal, 1dp)`,
  skip when `< 0.1` (never round up; `minSz`/`lotSz` 0.1 verified live on `BTC-USD-SWAP` 2026-09-10).
  Consequence: below ~$10 net notional (margin × leverage) no order is possible.
- Isolated margin on this instrument posts BTC; USDC counts toward `totalEq` but does not
  collateralize the isolated position — keep spare BTC for margin.
- Algo-SL field set on coin-margined swaps is must-verify-live (demo gate could not run —
  key 50119, see task log). First live fill must be minimum size with SL-fill watched.
- Test debt (user-deferred): `OkxTradingClientIT` / `BotLiveExecutionIT` still stub
  `BTC-USDT-SWAP` / `ctVal 0.01` — update stubs to `BTC-USD-SWAP` / live `ctVal` before the
  next full test run; do not run green on stale stubs and call it verified.

## Open risks (not blocking, tracked)

1. Intraday Fair Economy refresh may still 429 despite the 20 s gate (limit behavior was probed
   once, informally) — mitigated by backoff + DB-poll separation; if chronic, the fallback is a
   second actuals source (BLS/FRED vintage), not a faster poll.
2. `prompt.md` unit discipline: a Claude ladder authored in expanded units (e.g. `213000` for NFP)
   silently never matches. Mitigated by the prompt's worked examples; Backend adds an ingest-time
   WARN when a ladder's total span exceeds 100× the calendar print's magnitude (log-only, never a
   reject — §2's rationale).
3. OKX demo-vs-live key separation and `ctVal` (see §4) — must-verify items in the checklist, demo
   gate catches both.
4. `pnl_pln` quote-currency labeling (§5) — pre-existing, untouched, must not spread into sizing.

## Amendment 2026-11-09 — execution safety and the notional question (ADR-114, task 191)

- **§6 failure modes and guard placement are superseded by ADR-114.** The max-1-concurrent check,
  the 60 s duplicate window and the kill switch now run *before* the entry order. After the fill
  the order of work is protect (SL) → record (one transaction) → alert, under a context detached
  from shutdown. A write-ahead `bot_execution` row with a deterministic `clOrdId` makes each
  cluster at most one order across restarts and lets an unknown outcome be looked up. The
  emergency-close rule itself is unchanged. Normative matrix:
  `docs/architecture/diagrams/191-bot-live-execution-fix.md` §3.3.
- **§4 client:** context on every call, typed bodies and responses, `OutcomeUnknown` error
  classification. Stop-loss `reduceOnly`/`algoClOrdId` and entry `expTime` only after a demo
  check (ADR-114 §5).
- **§5 / the 2026-09-10 amendment contradict each other on notional.** The formula
  `contracts = floor(qtyBtc × price / ctVal)` sends **margin** of notional; the sentence "net
  notional (margin × leverage)" and §5's risk proof assume **margin × leverage**; `close()` books
  P&L for margin × leverage. The code follows the formula. Which one is intended is an open
  owner decision (task 191 Q1; recommended: keep the orders, fix the books). Until it is
  answered, neither formula changes.
