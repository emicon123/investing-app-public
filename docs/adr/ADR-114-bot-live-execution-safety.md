# ADR-114: Bot Live-Execution Safety: Commit Phase, Write-Ahead Journal, Route-Declared Auth

**Date:** 2026-11-09
**Status:** Accepted (design, task 191). F5.1 (order size vs P&L model) is **Proposed**, pending the
owner's answer in `.claude/tasks/191-bot-live-execution-fix.task.md` §Owner questions.
**Design:** `docs/architecture/diagrams/191-bot-live-execution-fix.md` (D191).
**Amends:** ADR-090 §4 (client) and §6 (failure modes, guard placement); ADR-110 Decision 6 (bot
goroutines outside the lifecycle Group, reversed); ADR-089 (same credentials per route, new mechanism).
**Inputs:** task-174 review F1.1, F5.1, F4.5, F1.14, F5.4, F2.9 and the bot parts of F1.5/F4.4
(`.claude/tasks/done/174-go-backend-review-REPORT.md`); task 186 §A.

## Context

Live OKX execution is armed on a real account (`BOT_LIVE_EXECUTION_ENABLED=true`,
`OKX_DEMO_MODE=false`, coin-margined `BTC-USD-SWAP`, isolated margin, net mode). The owner skipped
the bot in the task-174 programme and has now asked for it to be fixed. Re-reading the code
(`internal/bot/trade/execution.go`) confirmed the review's F1.1 and found more paths into the same
outcome. **Once the entry market order fills, the position has no stop-loss and no DB row if any
of these happen:**

1. SIGTERM (every deploy) cancels the watcher's context between the fill and `trades.Create`.
   The OKX calls take no context, so the order completes, and the DB insert fails at once.
2. Any DB error at `trades.Create`.
3. **Any guard refusal at `trades.Create`** (not named in the review). The max-1-concurrent check,
   the 60 s duplicate window and the kill switch all live in `Service.Create`, which runs *after*
   `PlaceMarketOrder`. Nothing closes a `bot_trade` row when the exchange-side stop fires
   (ADR-095/096 are not built), so a stale `OPEN` row from an earlier trade is enough. The next
   qualifying release then fills a real order and gets `409 MAX_CONCURRENT_TRADE`: no SL, no row.
4. A 15 s client timeout on the entry call while OKX accepted the order. There is no `clOrdId`, so
   nothing can find out afterwards.
5. A process kill (OOM under `mem_limit: 64m`, power loss) anywhere between fill and SL.

There is a second, independent exchange-side hazard. The SL is a standalone conditional order
without `reduceOnly`. OKX docs, `cxlOnClosePos`: "if it is not [associated with the position], the
TP/SL order will not be affected when the position is fully closed". So if the position is closed
any other way (the owner in the OKX UI, or the emergency close), the SL stays pending. If it
triggers later, it opens a new, unprotected position in the opposite direction.

## Decision

### 1. Two phases around one point of no return

`LiveExecutionService.Execute` is split into:

- **Prepare** (cancellable, under the watcher's context). Kill switch, the entry guards (moved here
  from `Create`: OPEN trade exists, duplicate window), price, equity, sizing, set-leverage, `ctVal`,
  and from step 3 on: *exchange flat*, *no pending conditional algo order*, and the write-ahead
  journal row. Any failure, or a cancelled context, ends with no order placed.
- **Commit** (detached: `context.WithoutCancel` + a 45 s budget). This is entry, then protect
  (SL), then record (one transaction), then alert. Protection never waits on the database: the SL
  is placed before anything is persisted. If the SL is refused, the position is closed (ADR-090
  rule, unchanged). The record step is retried within the budget. A failure after a fill always
  sends a Web Push alert (ADR-108) and logs at ERROR with every OKX id.

`Service.Create` keeps its guards for `POST /bot/trades`. The commit phase records through a new
guard-free `Service.RecordEntry`, because refusing to *record* a position that already exists only
hides it.

### 2. Exactly-once per cluster: deterministic `clOrdId` + write-ahead journal

A new table `bot_execution` (one row per cluster attempt, `UNIQUE(cl_ord_id)`) is written **before**
the entry order. `cl_ord_id = "bot" + clusterEventTime(UTC, yyyyMMddHHmmss) + "e"`. Effects:

- A watcher restart inside the release window cannot place a second order for the same cluster:
  the insert conflicts and the attempt is skipped. Today only in-memory state and the DB guards
  prevent this, and the DB guards miss the F1.1 orphan.
- An unknown entry outcome (timeout, 5xx, unparseable reply) is resolved by
  `GET /api/v5/trade/order?instId&clOrdId` (OKX `51603` = "Order does not exist"), then by the
  position size if the lookup fails. Prepare guarantees that the exchange was flat before the
  entry, so a non-zero position afterwards can only be this order.
- A hard crash leaves a durable `INTENT` or `PROTECTED` row. On startup the watcher runs a
  **read-only** preflight: it reports equity, position and pending algos, resolves stale rows with
  read-only lookups, and pushes one summary. It never places or cancels orders on its own.

The journal is the record of an unknown outcome for human follow-up. No API is added: the alert
carries the ids, and the existing `POST /bot/trades` can record an orphan by hand.

### 3. Watcher under the lifecycle Group

`trade.StartWatcher` and `calendar.StartScheduler` run as `lifecycle.Group` workers ("bot-watcher",
"bot-calendar-purge") with a per-tick `lifecycle.Guard`. Main's `lifecycle.Run` gets
`WorkerTimeout: 60s`, and compose `stop_grace_period` for `backend-go` goes 30 s → 90 s. A
shutdown therefore waits for an in-flight commit phase, which is bounded at 45 s plus a 10 s alert
step. The redundant `inFlight` CAS is deleted (F1.14).

### 4. OKX client: typed, context-aware, outcome-classifying

Every `OKXClient` method takes a `context.Context`. Request bodies are typed structs passed through
`json.Marshal`, never `%q`. Responses decode into typed envelopes, and per-order `sCode`/`sMsg` are
surfaced. Errors carry `OutcomeUnknown`: true for transport errors, timeouts, HTTP 5xx and
unparseable bodies on a write; false for an OKX rejection, which means definitely not executed. The
User-Agent becomes honest only after a public-endpoint check shows Cloudflare accepts it.

### 5. Stop-loss hardening (gated on a demo-account check)

The SL gains `reduceOnly: true` and an `algoClOrdId`, plus `cxlOnClosePos: true` where the
account mode supports it. The entry order gains an `expTime` header. These change the field set
on real-money calls, so they ship only after the OKX demo account confirms them on `BTC-USD-SWAP`,
or after an explicit owner decision for a minimum-size live check. Until then, prepare refuses to
trade while any conditional algo order is pending, which is the guard against the stale-SL hazard.

### 6. Route-declared auth (F2.9)

The path-prefix dispatcher `smartAuth` is replaced by a middleware that looks up the matched chi
route pattern (`chi.RouteContext(r.Context()).RoutePattern()`, available because inline-group
middlewares run after routing) in a table built once at startup from the embedded spec
(`openapi.GetSwagger()`): `security` absent → bearer, `[]` → public, `[serviceTokenAuth]` → service,
`[bearerAuth, serviceTokenAuth]` → either. Routes missing from the spec (the hand-mounted admin
backfill) default to bearer. The credential for all 83 operations is unchanged. The spec was corrected
first (`GET /bot/trades/pending-scalp` was served as dual but specified as service-only).

### 7. F5.1: order notional vs the P&L model (Proposed, owner decision)

The code places orders worth **margin**: `contracts = qty × price / ctVal`, with `qty = margin /
price`. The P&L model books **margin × leverage**: `Δprice × qty × leverage`. ADR-090 §5's risk
proof assumes margin × leverage. **Recommended option A:** keep the exchange orders as they are
(no change in real exposure) and correct the books: no `× leverage` in close/unrealised P&L, and
`margin_allocated_pln` = notional ÷ leverage, which is what OKX actually locks. Option B (orders ×
leverage) raises real exposure up to 10× and needs an explicit owner yes. No other step depends on
this answer.

## Alternatives rejected

- **Intent as a `bot_trade` row with a new status:** every reader (stats, UI, enrichment, OpenAPI
  `BotTradeStatus`) would have to learn a state that means "maybe no position". A separate journal
  keeps `bot_trade` = "positions that exist".
- **An automatic reconciler** that protects or flattens orphans on startup: it would act on stale
  prices and could fight a human who is already handling it in the OKX UI. It stays report-only,
  and a human decides.
- **Persist first, then SL** (the current order, with retries): protection would still depend on
  the DB being up.
- **Rely on OKX `clOrdId` uniqueness for exactly-once:** OKX enforces it only among *pending*
  orders, and a filled market order frees the id.
- **A circuit breaker / Command objects for OKX calls:** one call path, a handful of calls a day.
  Bounded retries inside the commit budget are enough.
- **A Strategy seam for the F5.1 sizing options:** one formula after the owner decides, and no
  runtime switch.
- **Deriving auth from oapi-codegen security scopes:** the generator (v2.8, this config) emits no
  scope context values, and enabling them changes generated code for all handlers. The route-pattern
  lookup needs no codegen change.

## Consequences

- No deploy, DB error, guard refusal or crash can leave a filled position without at least an
  exchange-side SL attempt, an alert, and (DB permitting) a durable record.
- Prepare adds two read-only OKX calls per entry (position, pending algos), about 0.2–0.5 s of
  latency on a reaction that today takes about 5–8 s.
- The bot refuses to trade while the account has any `BTC-USD-SWAP` position or pending conditional
  order it did not just create. That assumes the bot is the only actor on that instrument (owner
  question Q3).
- One new table (`bot_execution`, Flyway `V211` at the time of writing), no OpenAPI path change.
- Shutdown may take up to about 75 s when, and only when, an execution is in flight.
