# ADR-096: Wiring Claude's Scalp-Session Trade Actions to Execute on OKX

**Date:** 2026-09-16
**Status:** Accepted
**Related:** `docs/adr/ADR-090-okx-live-execution.md` (entry execution, `OkxTradingClient`,
`LiveExecutionService`), `docs/adr/ADR-092-post-trade-claude-trigger.md` (outbox/claim idiom,
scalp session launch), `docs/adr/ADR-095-backend-hard-close-timeout.md` (the position-mutation
lock this ADR reuses; the ADR that first flagged this gap and deliberately deferred it),
`backend/.../service/bot/BotTradeActionService.java`, `backend/.../client/bot/OkxTradingClient.java`,
`backend/.../entity/BotTrade.java`, `infra/bot/scalp-session.sh`, `infra/bot/manage-prompt.md`,
`docs/architecture/diagrams/149-trade-action-execution.md` (UML, read before implementing),
`.claude/tasks/149-architect-output.md` (Backend build spec), `.claude/tasks/148-architect-output.md`
§9 (amendment — task 148 now depends on a class this ADR introduces).

## Context

ADR-095 investigated why `infra/bot` scripts behave the way they do and, alongside its own
primary finding (no backend time-based hard close ever existed), surfaced and explicitly deferred
a second, more severe gap: **`POST /bot/trades/{id}/actions` — the endpoint the host-side scalp
session (`infra/bot/scalp-session.sh`, running Claude) calls to post its `SET_SL`/`SET_TP`/
`TRAIL_SL`/`PARTIAL_CLOSE`/`FULL_CLOSE` decisions — only ever persists an audit row. It never
calls OKX.** Re-verified directly against the current source for this ADR, not re-trusted from
ADR-095's own account: `BotTradeActionService.create()` (lines 28-41 as of this writing) builds a
`BotTradeAction`, calls `repository.save(a)`, and returns — no `OkxTradingClient` call anywhere in
the class. Grepping every caller of `OkxTradingClient.closePositionMarket`/`placeStopLossAlgo`
confirms exactly one call site in the entire backend, `LiveExecutionService`, used only for the
initial entry and its SL-placement-failure emergency-close path.

Concretely, as of this ADR: the only things that can ever end or adjust an open `bot_trade`
position on the exchange are (a) the price-based exchange-side stop-loss placed once at entry,
(b) `BotHardCloseWatcherService` once ADR-095 ships (time-based, full-close only), or (c) a human
manually intervening on the OKX UI. A live 15-minute scalp session in which Claude decides
`TRAIL_SL`, `PARTIAL_CLOSE`, or `SET_TP` — the entire premise `infra/bot/manage-prompt.md` is
built around — currently does *nothing* on the exchange when it does so; the position is managed
exactly as if Claude had never been consulted.

**Verified state of ADR-095/task 148 at the time this ADR is written (2026-09-16):** its Flyway
migration (`V72__bot_hard_close_timeout.sql`) and its `infra/bot/*` doc corrections are already on
disk; `BotHardCloseWatcherService.java`, `BotHardCloseStatus.java`, and
`OkxTradingClient.cancelAlgoOrder` do **not** exist yet — task 148's Java-side implementation has
not landed. This matters directly for this ADR's design: `cancelAlgoOrder` and the
`hard_close_status` claim/release columns are **shared dependencies** this ADR also needs, not
independent inventions. Rather than re-specify `cancelAlgoOrder` a second time (which would either
silently duplicate it or conflict if task 148 lands first), this ADR treats it — and, newly, a
shared close-orchestration helper (`BotPositionCloseExecutor`, see Decision §2) — as built once by
whichever of task 148 / task 149 lands first in Backend's queue, and referenced, not
re-implemented, by the other. `.claude/tasks/148-architect-output.md` §9 amends task 148's build
spec accordingly (its close-sequence now delegates to `BotPositionCloseExecutor` instead of
inlining `cancelAlgoOrder`+`closePositionMarket`+`getPositionContracts` directly — a class task
148 hadn't yet had reason to need until this ADR introduced a second caller for the same
sequence).

Also newly verified while reading for this ADR: `OkxTradingClient` has **no partial-close
primitive** (`closePositionMarket` always flattens 100% of the open position) and **no
take-profit mechanism at all** (`placeStopLossAlgo` has no analogue for `tpTriggerPx`). `BotTrade`
has no field to remember an SL/TP's current trigger price (only the algo *id*), and no way to
represent a position that has been partially, but not fully, closed (`BotTradeStatus` is a plain
`OPEN`/`CLOSED` pair; `quantity` is set once at entry and never adjusted).

## Decision

### 1. Reuse ADR-095's `hard_close_status` claim as a general position-mutation lock — not a second lock

The concrete race this ADR must close: `TRAIL_SL`'s cancel-then-replace of `bot_trade.okx_algo_id`
can overlap in time with `BotHardCloseWatcherService`'s own `cancelAlgoOrder` call on the *same*
algo id, once that watcher exists (ADR-095, ~15-minute mark). Both are independent, real OKX
mutations against the same trade; only one may safely be in flight at a time, for *any* pair of
actions that touch the position or its algo orders — not just two competing full-closes.

`bot_trade.hard_close_status`/`hard_close_claimed_at` (V72, task 148) and their atomic
`claimHardClose`/`releaseHardCloseClaim` queries already implement exactly the primitive needed:
single atomic `UPDATE ... WHERE status='OPEN' AND hard_close_status='NONE'`, 1 row = claimed, 0 =
someone else already holds it. This ADR reuses those columns and queries **verbatim, unchanged**,
for all five action types — `FULL_CLOSE` claims it as a terminal transition (identical to
ADR-095's watcher — whichever of the two wins a given race is the only one that touches OKX, per
the brief's own framing of this question), and the four non-terminal types (`SET_SL`/`SET_TP`/
`TRAIL_SL`/`PARTIAL_CLOSE`) claim it, do their OKX work, and release it back to `NONE` themselves
on completion (success or failure) — the one behavioral generalization beyond task 148's original
two-state machine (which never had a caller that claims-and-releases for a non-terminal reason).
No new state, no new column, no schema change: `CLOSING` now simply means "some actor currently
holds exclusive rights to call OKX against this trade's position or algo orders," a strict
superset of task 148's original "the hard-close watcher is flattening this trade." The Javadoc on
`claimHardClose`/`releaseHardCloseClaim`/`BotHardCloseStatus` is widened to describe this (task 148
§9 amendment); the column name (`hard_close_status`) stays as-is — see Alternatives for the rename
considered and rejected.

`closePositionMarket`'s own idempotency (no-op if already flat) remains a safety net, but — per
the brief's own framing — is deliberately not relied on as a *substitute* for the claim: two
racing `cancelAlgoOrder` calls against an already-cancelled algo id is a real, distinct failure
mode (ADR-095's own Open Risk #1, unresolved) that idempotent position-closing alone does not
prevent. The claim prevents the race from happening at all, rather than hoping both sides handle a
raced outcome gracefully.

### 2. `FULL_CLOSE` shares a new `BotPositionCloseExecutor` with `BotHardCloseWatcherService` — not a third reimplementation

`FULL_CLOSE` (via this endpoint) and `BotHardCloseWatcherService`'s timeout close (ADR-095 §3) do
the *identical* 4-step OKX sequence: cancel SL algo (if present) → cancel TP algo (if present, new
in this ADR) → market-close → verify flat. A new, DB-write-free `BotPositionCloseExecutor.closeFully(BotTrade)`
holds this sequence exactly once; it throws on any failure or non-flat result and does nothing
else — no retry, no persistence, no audit row. Each caller keeps its own, genuinely different,
wrapping: `BotHardCloseWatcherService` retries forever on failure with no cap (ADR-095 §2, because
it is one of only two automated backstops that exist); this endpoint's `BotTradeActionExecutionService`
returns synchronously to a live, time-boxed `curl` call and does not retry internally at all (see
Decision §5) — those different failure-handling needs are exactly why the retry *wrapping* stays
separate while the OKX orchestration *core* does not. This is the DRY-vs-different-failure-needs
call the brief asked to be made explicitly: DRY wins for the mechanical OKX sequence (drift there
is a real-money bug), the two callers' retry shapes are kept independent because collapsing them
would either make the endpoint block/retry indefinitely (breaking Claude's ~60s beat loop) or make
the watcher give up after one attempt (defeating ADR-095's entire purpose).

Whichever of task 148 / task 149 Backend implements first builds `BotPositionCloseExecutor`; the
other's build spec already references it, not a re-derivation (task 148 §9 amendment).

### 3. `TRAIL_SL` and `SET_SL` are the same OKX-level operation; `SET_TP` mirrors it on a new, independent leg

Confirmed against `infra/bot/manage-prompt.md`'s actual contract: both `SET_SL` and `TRAIL_SL` are
"post a new stop price" decisions with no OKX-level distinction between "initial/corrective
placement" and "tightening an existing stop." OKX algo orders are not amendable in place (already
established as a known constraint by ADR-095's Consequences), so both resolve to the same
sequence: cancel the existing `okx_algo_id` (if present) → place a new standalone conditional SL
order at `req.price()`, sized to the *current* `bot_trade.quantity` in contracts → store the new
algo id and `sl_trigger_price`. The two enum values are kept distinct purely for audit-trail
readability (a human scanning `bot_trade_action` wants to see "tightened" vs. "(re)set"
separately) — not because the execution differs.

`SET_TP` is new: `OkxTradingClient.placeTakeProfitAlgo(instId, closeSide, szContracts, triggerPx)`,
mirroring `placeStopLossAlgo`'s exact shape (`ordType=conditional`, `tpTriggerPx`+`tpOrdPx=-1`
instead of `slTriggerPx`+`slOrdPx=-1`) via the same `/api/v5/trade/order-algo` endpoint, storing
its id in a new, independent `bot_trade.okx_tp_algo_id` column (mirrors `okx_algo_id`) and its
price in new `bot_trade.tp_trigger_price`. A standalone SL and a standalone TP are kept as **two
independent conditional orders**, not merged into one OKX bracket order — see Alternatives.

### 4. `PARTIAL_CLOSE`: server-computed percentage, `bot_trade.quantity` shrinks to the remaining size, existing SL/TP resized at the same price

`manage-prompt.md`'s own decision contract already hardcodes "PARTIAL_CLOSE (50%)" in prose — there
is no dynamic per-decision sizing contract anywhere in this system today. The close fraction is a
new config field, `BotExecutionProperties.partialClosePct` (default `0.5`), not the
client-submitted `quantity` — trusting a raw LLM-submitted `BigDecimal` to directly size a
real-money reduce-only order opens an unjustified trust boundary for no benefit over a config
constant that is already gated the same way every other trading number in this module is (ADR
amendment required to change). The submitted `quantity` field is still persisted verbatim on the
audit row unchanged — full transparency of what Claude asked for — it is simply never used to size
the actual order.

Execution: `closeQtyBtc = trade.quantity × partialClosePct`; convert to OKX contracts via the
existing `LiveExecutionService.contractsFor` conversion (DOWN-rounded to lotSz 0.1, extracted to a
shared static `OkxSizing.contractsFor` — see Decision §6); if the result rounds to less than one
lot, the action is treated as `SKIPPED` (nothing closed, lock released, no OKX call) rather than
silently closing 0 or erroring — a materially-undersized account should not be able to trigger a
confusing zero-effect order. Otherwise: `OkxTradingClient.closePositionPartialMarket(instId, side,
sz)` places a plain `reduceOnly` market order at the given, explicit `sz` (mirrors every other
client method's style — explicit contracts in, no fraction/percentage crosses the client
boundary); `bot_trade.quantity` is reduced by `closeQtyBtc` (the position's remaining open size,
matching the existing convention that `quantity` = currently-open size, already relied on by
`BotMapper`'s unrealized-PnL calc and `BotTradeService.close()`'s realized-PnL calc — no change to
either of those call sites' formulas is needed, only their input now correctly reflects a partial
exit); the PnL realized on the closed fraction is computed via the new shared `BotPnlCalculator`
(Decision §6) and added to a new accumulator, `bot_trade.realized_pnl_pln`.

**The existing SL/TP, if any, is always resized after a partial close, not left as-is or asserted
safe.** Whether OKX correctly honors a `reduceOnly` conditional order whose `sz` exceeds the
now-smaller open position is genuinely unverified (flagged, not guessed — see Open Risks). Rather
than bet real money on an unconfirmed exchange behavior, the design sidesteps the question
entirely: any partial close that finds `sl_trigger_price`/`tp_trigger_price` set triggers the exact
same cancel-and-replace sequence Decision §3 already defines, at the **same trigger price**, sized
to the **new, smaller** `bot_trade.quantity`. This costs 1-2 extra OKX calls per partial close and
is unconditionally correct regardless of how OKX would have behaved with the stale size.

### 5. Failure handling for a synchronous call from a live, time-boxed Claude session

Unlike ADR-095's watcher (unlimited retry, no caller waiting on a response), this endpoint returns
synchronously to a `curl` call issued directly by Claude inside its own ~60s-beat loop
(`scalp-session.sh` grants `--allowedTools "Bash(curl:*)"` — Claude runs `curl` itself and reads
its stdout as part of its own reasoning, it is not mediated by the shell script). A failed OKX call
here **cannot** retry internally without blocking that loop for an unbounded time, so it does not
retry at all: one claim, one attempt, one outcome, always returned within the existing 5-10s
`RestClient` timeout budget already configured on `OkxTradingClient`.

**The outcome is always HTTP 200 (for a validly-typed request against an existing trade), with
success/failure encoded in the response body via a new `executionStatus` field
(`EXECUTED`/`FAILED`/`SKIPPED`), not via the HTTP status line.** This is a direct consequence of
verifying how Claude actually consumes the response: it reads whatever `curl` prints to stdout,
which by default does **not** include the HTTP status line unless the command explicitly requests
it (`-w`). A 4xx/5xx status alone is not a reliable signal in this integration shape — a body field
Claude already parses (per `manage-prompt.md`'s own JSON-output contract) is. Malformed requests
(unknown `actionType`, trade not found) are the one exception and keep the existing 400/404 — those
are client-request problems, not exchange-execution outcomes, and are not expected from a
well-formed Claude decision in normal operation.

`FAILED` and `SKIPPED` both still persist the audit row (unchanged — "persist what was decided"),
with the execution outcome recorded separately on the same row (`execution_status`,
`execution_error`, `executed_quantity` — new `bot_trade_action` columns, V73) — matching the
"requested vs. executed" separation the brief suggested as more consistent with this codebase's
existing audit-trail philosophy than mapping an OKX failure to an HTTP error.

**Related finding, not fixed by this ADR:** `scalp-session.sh` fetches `bot_trade` state exactly
once, at session start (`trade_json=$(curl ... /bot/trades/${TRADE_ID})`, baked into the initial
prompt) — Claude's own per-beat loop re-invokes only `watch-market.sh` (price/volume data), never
re-fetches trade state. This means the action-response body is Claude's *only* feedback channel
for learning that the trade it thinks it is managing has already been closed or resized by another
actor (typically `BotHardCloseWatcherService` claiming the lock first). The response is widened
with two response-only, non-persisted fields — `tradeStatus`, `tradeRemainingQuantity` — read from
a fresh `BotTrade` fetch after every action, specifically so a `SKIPPED` outcome is self-explanatory
without Claude needing a second round-trip. A fuller fix (`watch-market.sh` also returning fresh
trade state every beat) is flagged as a follow-up, out of scope here — it is a host-script/prompt
concern orthogonal to wiring the backend execution path, and the response-body widening already
covers the load-bearing case (a `FULL_CLOSE`/lock-loss mid-session).

`infra/bot/manage-prompt.md` is corrected in this same change (matching ADR-095's own "correct the
live prompt now, flip it once shipped" precedent) to stop asserting "This is advisory only" (false
as of this ADR's design — once implemented, these actions execute for real) and to instruct Claude
to check `executionStatus`/`tradeStatus` in the response and treat a non-`EXECUTED` result as "did
not happen, re-assess next beat" rather than assuming success. Marked with a `CORRECTED` pointer to
this ADR for Backend to flip to present-tense-accurate once `BotTradeActionExecutionService`
actually ships, exactly as task 148 §7 already establishes as the pattern for this repo.

### 6. Two small, justified extractions (Rule of Three, not speculative)

- **`BotPnlCalculator.pnl(side, entryPrice, exitPrice, quantity, leverage)`** — the PnL formula
  (`diff = isLong ? exit-entry : entry-exit; pnl = diff × quantity × leverage`) is already
  duplicated in `BotTradeService.close()` and `BotMapper.toDto()`'s unrealized-PnL calc.
  `PARTIAL_CLOSE`'s realized-PnL booking is a genuine third call site — extracted to a small static
  utility rather than duplicated a third time, since a drifted copy of this exact formula is a
  real-money correctness bug, not a cosmetic one.
- **`OkxSizing.contractsFor(quantityBtc, ctVal, price)`** — extracted from
  `LiveExecutionService`'s existing private method (BTC → OKX contracts, DOWN-rounded to lotSz
  0.1, `null` if it rounds to less than one lot). `PARTIAL_CLOSE`, `SET_SL`/`TRAIL_SL`, and
  `SET_TP` all need this exact conversion. Same real-money-drift-risk justification as above.

`currentBtcPrice()` (a trivial 3-line lookup, already duplicated three times across
`LiveExecutionService`/`BotTradeService`/`BotScalpSessionService`, and explicitly left
un-extracted by task 148's own build spec) is **not** extracted here either — a fifth near-duplicate
follows the same, already-made call in this codebase; relitigating it mid-task is out of scope and
unjustified (no arithmetic to drift, unlike the two extractions above).

## Alternatives considered

| Alternative | Shape | Verdict | Why |
|---|---|---|---|
| **A. A second, independent lock column** (e.g. `action_execution_status` on `bot_trade`, separate from `hard_close_status`) | New column + new atomic claim/release queries, mirroring V72's shape a second time | **Rejected** | Would not actually prevent the concrete race (`TRAIL_SL` vs. the watcher's `cancelAlgoOrder` on the same algo id) unless the two locks were then cross-checked against each other anyway — at which point it is just a more complex, differently-named version of reusing the one lock that already exists and is already fully speced/tested (task 148). Two independently-evolving lock columns for one conceptual "who may currently call OKX for this trade" question is duplication with extra steps. |
| **B. Rename `hard_close_status` → a generic name** (e.g. `okx_mutation_status`) as part of this ADR | Column rename in V73, ripple through task 148's not-yet-implemented Java | **Rejected, for now** | The generalization is real and the name is slightly imprecise once non-terminal actions also claim it — but ADR-095 is `Accepted` and task 148's Java implementation may be actively in progress by another Backend agent in parallel with this design. Renaming a column another in-flight task depends on is a coordination risk for a cosmetic gain; the Javadoc widening in task 148 §9's amendment carries the semantic without a schema change. Revisit as a follow-up rename once both tasks have shipped and the coordination risk is gone. |
| **C. Combined OKX bracket order** (single `order-algo` call carrying both `slTriggerPx` and `tpTriggerPx`) instead of two independent standalone conditional orders | One OKX order instead of two | **Rejected** | `placeStopLossAlgo` already treats the SL as independently cancel-and-replaceable. Merging SL+TP into one order means adjusting either leg requires cancelling (and re-placing) *both* — a `TRAIL_SL` would silently drop the take-profit unless the replace call also re-specified it, and vice versa. Two independent orders is a simpler, more surprising-behavior-free mental model that matches the existing SL precedent exactly, at the cost of one extra cancel call inside `BotPositionCloseExecutor.closeFully` on a full close (negligible). |
| **D. Trust the client-submitted `quantity` for `PARTIAL_CLOSE` sizing** | No config field, `req.quantity()` drives the OKX order directly | **Rejected** | No part of this system today lets an LLM-submitted number directly size a real-money order without a server-side bound or veto (`LiveExecutionService`'s own entry sizing goes through `BotPositionSizingService`, never a raw client number). `manage-prompt.md`'s own contract only ever describes a fixed 50% — a config constant is simpler, matches that contract exactly, and is gated by the same ADR-amendment discipline as every other trading number in this module. |
| **E. Endpoint returns 4xx/5xx on an OKX execution failure** | Standard REST error-status mapping | **Rejected** | Verified (not assumed) that Claude reads the raw `curl` stdout inside its own tool-call loop, which does not surface the HTTP status line by default. A body-encoded `executionStatus` field is the reliable channel in this specific integration; relying on the status line risks a silent, unnoticed failure inside a live real-money session — the worse outcome. |
| **F. Retry the OKX call internally within the endpoint** (bounded retry/backoff before responding) | e.g. 2-3 attempts with short backoff before giving up | **Rejected** | Would extend the response latency unpredictably inside Claude's ~60s beat budget and duplicate retry logic `BotPositionCloseExecutor`'s two callers already handle differently at the *caller* level (the watcher retries forever on its own 5s cadence; this endpoint's "retry" is simply that Claude gets to decide again next beat). One attempt, fast, honest response is both simpler and — given the watcher exists as a backstop for the terminal case, and Claude's own next beat is the backstop for non-terminal cases — not a safety regression. |

## Consequences

- Backend implements exactly `.claude/tasks/149-architect-output.md` (Flyway `V73`, new
  `BotTradeActionExecutionService`, `BotPositionCloseExecutor`, `BotPnlCalculator`, `OkxSizing`,
  `OkxTradingClient.placeTakeProfitAlgo`/`closePositionPartialMarket`, `BotExecutionProperties.partialClosePct`,
  `BotActionExecutionStatus`, `BotTradeActionService.create()` restructured, tests). Any deviation
  in a number (`partialClosePct=0.5`) is an ADR amendment, same standing rule ADR-092/ADR-095
  already established for this module.
- `.claude/tasks/148-architect-output.md` §9 is amended: `BotHardCloseWatcherService`'s close
  sequence now delegates to `BotPositionCloseExecutor` instead of inlining
  `cancelAlgoOrder`+`closePositionMarket`+`getPositionContracts` directly. If task 148 has already
  been implemented by the time task 149 starts, `BotPositionCloseExecutor` is instead extracted
  from the already-shipped `BotHardCloseWatcherService.hardClose()` as a small refactor at the
  start of task 149, with `BotHardCloseWatcherService`'s own tests re-run unchanged (its external
  behavior does not change, only where the OKX sequence lives).
- `docs/architecture/06-data-flow.md` §14 documents this flow (the next section number after
  ADR-095's §13).
- `infra/bot/manage-prompt.md` prose corrected in this same change (Decision §5) — Backend does a
  second pass once the mechanism ships to flip the wording from "advisory only, not yet wired" to
  live, per this repo's "docs describe the system as it now IS" convention, matching task 148 §7's
  precedent exactly.
- Once this ships, exactly three things exist that can end/adjust an open `bot_trade` position on
  the exchange: (1) the price-based exchange-side SL/TP, (2) `BotHardCloseWatcherService`'s
  time-based full-close (ADR-095, once it ships), (3) Claude's own advisory decisions via this
  endpoint (this ADR) — plus, unchanged, (4) a human manually intervening. Before this ADR, (3) was
  a no-op.

## Open risks (not blocking, tracked)

1. **Shared with ADR-095 Open Risk #1** — the exact OKX `sCode`/error code for "algo order already
   cancelled/triggered/not found" on `POST /api/v5/trade/cancel-algos` remains unverified. This ADR
   makes that ambiguity matter in one more place: a `TRAIL_SL` or `SET_TP` cancel-then-replace
   racing the watcher's own cancel (Decision §1's lock closes the *window* for this race but a
   genuinely simultaneous claim attempt at the exact poll/request boundary is not literally
   impossible, only made very unlikely). Not re-verified independently here — same open item,
   same owner (whichever of task 148/149 reaches a demo-mode OKX session first).
2. **New: the actual JSON response shape of `POST /api/v5/trade/order-algo` when placing a
   take-profit leg (`tpTriggerPx`/`tpOrdPx`) is assumed, not verified.** `placeStopLossAlgo`'s
   existing code reads `data[0]` under the key `"stop-loss"` (`firstData(root, "stop-loss")`);
   `placeTakeProfitAlgo` is speced to read the analogous `"take-profit"` key by symmetry, but this
   has not been confirmed against real/demo OKX output. Backend must verify in demo mode before
   relying on it in production, same posture as Open Risk #1 — if the key differs, `firstData`
   will throw a clear `OkxClientException` (fail-closed, not silently wrong) rather than
   misparsing, so the failure mode if wrong is safe, just needs the fix once observed.
3. **Whether a `reduceOnly` conditional (SL/TP) order sized larger than the currently-open position
   would have behaved safely on OKX is not resolved — only sidestepped.** Decision §4's
   always-resize-after-partial-close design avoids ever finding out empirically. If a future change
   removes that resize step for any reason, this question becomes live and unanswered again.
4. **Dust-remainder edge case after `PARTIAL_CLOSE`:** if a subsequent `PARTIAL_CLOSE` (or a
   position that started small) leaves a remaining `bot_trade.quantity` below OKX's minimum lot
   size, no explicit guard exists in this design beyond `OkxSizing.contractsFor`'s existing
   "returns null / skip if it rounds to < 1 lot" behavior at the time of *that* close attempt. A
   position could in principle be left in a state where it can no longer be `PARTIAL_CLOSE`'d
   further, only `FULL_CLOSE`'d. Not solved here (YAGNI absent an observed real-world occurrence);
   flagged for awareness.
5. **`executed_quantity` for `PARTIAL_CLOSE` books the requested (pre-lot-rounding) BTC amount,
   not OKX's actual filled contracts converted back to BTC.** The discrepancy is bounded by one
   lot step (0.1 contracts) and always in the direction of slightly *understating* remaining open
   exposure in `bot_trade.quantity` — the safer direction if it ever matters — but is not
   reconciled against OKX's actual fill precisely. Accepted as bounded/negligible, not engineered
   around (YAGNI).
6. **No empirical data yet on how frequently a single scalp session issues `TRAIL_SL` in
   practice** — if it turns out to fire many times per 15-minute session, the cancel+replace
   OKX-call volume (and any associated rate-limit exposure, ADR-095 Open Risk #2) is higher than
   this design implicitly assumed when reasoning about call volume being "trivial." Not tuned
   against live data; watch for this once real sessions run against the shipped mechanism.
