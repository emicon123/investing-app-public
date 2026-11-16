# ADR-095: Backend-Side Hard-Close Timeout Enforcement for `bot_trade`

**Date:** 2026-09-16
**Status:** Accepted
**Related:** `docs/adr/ADR-090-okx-live-execution.md` (entry execution, exchange-side SL,
`LiveExecutionService`), `docs/adr/ADR-092-post-trade-claude-trigger.md` (outbox/claim idiom
this ADR reuses and deliberately diverges from), `backend/.../service/bot/LiveExecutionService.java`,
`backend/.../client/bot/OkxTradingClient.java`, `backend/.../service/bot/BotScalpSessionService.java`,
`infra/bot/scalp-session.sh`, `infra/bot/manage-prompt.md`,
`docs/architecture/diagrams/148-hard-close-timeout.md` (UML, read before implementing),
`.claude/tasks/148-architect-output.md` (Backend build spec)

## Context

While investigating why `infra/bot` scripts behave the way they do (prompted by discovering
`scalp-launcher.sh` — the ADR-092 host-side launcher — had never actually been installed as a
systemd service on the Pi host since ADR-092 shipped 2026-09-10, i.e. **zero scalp sessions have
ever launched automatically in production**), a second, independent, more fundamental gap was
found and verified:

**Claimed but nonexistent: "the executor's own hard 15-minute close."** `docs/adr/ADR-092-post-trade-claude-trigger.md`,
`infra/bot/manage-prompt.md` (fed directly into the live trading LLM's context at every scalp
session), and `infra/bot/scalp-session.sh` (comments) all repeatedly assert this as an existing,
independent safety backstop, separate from whether the Claude advisory session responds in time.
Grepping every `@Scheduled` method in the backend (`EventReleaseWatcherService` 1s tick —
calendar/entry watching only; `BotCalendarService` daily 4am cron; `PriceCacheService` 15-min
*cache* refresh, unrelated; `MacroDataScheduler`; `HistoriaScheduler`; `GusInflationSyncService`)
confirms **none of them touch `bot_trade` rows or close positions.** No such mechanism has ever
existed. The only thing that was ever real is: (a) `scalp-session.sh`'s own `timeout 900` — an
OS-level wrapper that kills the `claude -p` **process**, not the OKX position, when it fires, and
(b) the exchange-side stop-loss placed once at entry (`LiveExecutionService`, ADR-090) — price-based,
not time-based; a position can sit open indefinitely if price never reaches the SL trigger.

**A second, adjacent, more severe finding surfaced during this same investigation (verified by
grep, not assumed) that materially changes this ADR's risk framing, though fixing it is explicitly
out of this ADR's scope:** `POST /bot/trades/{id}/actions` (`BotTradeActionService.create`) —
the endpoint Claude's scalp session calls to post `HOLD`/`TRAIL_SL`/`PARTIAL_CLOSE`/`FULL_CLOSE`
decisions — only persists an audit row in `bot_trade_action`. It never calls OKX. Grepping every
caller of `OkxTradingClient.closePositionMarket`/`placeStopLossAlgo` shows exactly one call site
in the entire backend: `LiveExecutionService`, used only for the initial entry and the
SL-placement-failure emergency-close path. **There is currently no code path, anywhere, that
executes a `FULL_CLOSE` (or any other advisory action) on the exchange.** Combined with the first
finding, this means: as of this ADR, the *only* thing that can end an open `bot_trade` position on
OKX is either the price-based SL triggering, or a human manually intervening (OKX UI, or
`PATCH /bot/trades/{id}/close`, which itself only updates bookkeeping — it doesn't call OKX
either). This is tracked as a separate, follow-up architecture task (flagged via `spawn_task`
alongside this ADR, not built here) — but it means the mechanism this ADR builds is, for now, the
**only** automated time-based backstop that exists, not one of several. The design below (§2)
accounts for that directly: it fails open (retries) rather than failing closed, unlike ADR-092's
own claim idiom.

A third, smaller finding: `BotMapper.toDto(BotTrade, BigDecimal)` already computes
`timeToHardCloseSeconds = Math.max(0, 900 - heldSeconds)`, a hardcoded literal `900`, rendered
live in the `/bot` frontend page as `"Xm / 15m (Ym left)"` — a countdown implying an enforced
deadline that, until this ADR ships, enforces nothing. This ADR's task file wires that literal to
the new `BotExecutionProperties.hardCloseAfterSeconds` config field (§3) so the displayed number
and the enforced number can never drift apart, and so the countdown becomes true the moment this
ships.

## Decision

### 1. New, dedicated `@Scheduled` watcher — not folded into `EventReleaseWatcherService`

`BotHardCloseWatcherService`, `@Scheduled(fixedDelay = 5000)` (5s), polls for
`bot_trade` rows with `status='OPEN' AND hard_close_status='NONE' AND opened_at <= now -
hardCloseAfterSeconds` (new `BotExecutionProperties.hardCloseAfterSeconds`, default **900**,
matching `scalp-session.sh`'s existing `SCALP_HARD_TIMEOUT` and the frontend's existing `900`
literal).

- **5s poll, not `EventReleaseWatcherService`'s 1s tick:** correctness here does not require
  sub-second precision. The position is continuously protected by the price-based exchange-side
  SL for the entire hold — this job's only job is to bound the *worst-case overshoot* past the
  900s deadline to something small. 5s keeps that overshoot ≤5s typical, at negligible cost
  (`maxConcurrentTrades=1` bounds the candidate set to at most one row).
- **A separate class, not a call added inside `EventReleaseWatcherService.doTick()`:**
  `doTick()` early-returns `if (clusters.isEmpty()) return;` — folding hard-close monitoring in
  there would make position-timeout enforcement silently depend on today having a live calendar
  scenario cluster, which is wrong. A stuck `OPEN` row from a prior day (exactly today's real
  incident: `scalp-launcher.sh` silently never installed) must always be watched, independent of
  whether any calendar event exists today. This is not "a new thread pool" in the sense ADR-090
  rejected — it rides the same shared Spring `TaskScheduler` five other `@Scheduled` beans
  already use.
- **Runs unconditionally**, independent of `bot_control.trading_enabled` (same reasoning
  ADR-092 already established for scalp-session management: the kill switch gates *entries*, not
  the ability to exit an already-open position) and independent of whether
  `scalp-launcher.sh`/`scalp-session.sh` are installed, running, or have ever run at all — it
  reads only `bot_trade`/`bot_control` state via JPA, in-process, with zero dependency on any
  host-side process or the `claude` CLI.

### 2. Atomic claim guard, reusing ADR-092's idiom but deliberately failing OPEN, not closed

New columns (Flyway `V72`, next free — head is V71): `bot_trade.hard_close_status VARCHAR(16)
NOT NULL DEFAULT 'NONE' CHECK IN ('NONE','CLOSING')`, `hard_close_claimed_at TIMESTAMPTZ NULL`.
A new, isolated column pair rather than widening `BotTradeStatus` itself (which is read by
frontend filters and the `existsByStatus(OPEN)` entry-gate) — zero blast radius outside this
feature, same minimal-surface precedent `scalp_session_status` already set.

Claim: single atomic `UPDATE bot_trade SET hard_close_status='CLOSING', hard_close_claimed_at=NOW()
WHERE id=:id AND status='OPEN' AND hard_close_status='NONE'` — 1 row = claimed, 0 = someone/something
else already claimed it or it's already closed (skip silently, not an error; the `status='OPEN'`
guard means a concurrent human `PATCH /bot/trades/{id}/close` naturally wins any race at the DB
level with no special-casing needed).

**Divergence from ADR-092, explicit:** ADR-092's `scalp_session_status=RUNNING` is *never*
auto-reclaimed — fail-closed, by design, because a dead scalp session still leaves the SL (and,
after this ADR, this very job) as backstops. Here, on **any** OKX-call failure or a
still-not-flat result, the claim is released (`CLOSING→NONE` via an equally atomic guarded
`UPDATE`) so the **next 5s tick retries**, unconditionally, with no backoff and no retry cap.
This is necessary, not optional: per the Context's second finding, this job is — for now — the
*only* automated mechanism that can end an overdue position. Failing closed here would silently
convert "backstop" into "was a backstop for exactly one attempt." The accepted cost is a
possible stream of `ERROR` logs and `bot_trade_action` rows every 5s if OKX is down for an
extended period; given `maxConcurrentTrades=1`, this is bounded to a trivial call volume, and
loud/frequent visibility is the correct trade-off for an overdue, real-money, currently-unprotected-past-deadline
position.

### 3. What "close" means precisely

Order matters and is normative:

1. **Cancel the exchange-side SL algo order first**, via a new `OkxTradingClient.cancelAlgoOrder(instId,
   algoId)` (`POST /api/v5/trade/cancel-algos`), if `bot_trade.okx_algo_id` is non-null. This SL
   was placed as a **standalone conditional order** (`placeStopLossAlgo`, `order-algo` endpoint,
   no `attachAlgoOrds` linkage to the entry order) — verified from `OkxTradingClient`'s existing
   code, which never cancels it anywhere. A standalone conditional order is **not** automatically
   cancelled by a separate reduce-only market order flattening the position; if left in place, it
   remains live on OKX and could fire later — on an unrelated future price move, with no position
   left to "close" — and instead *open* an unintended new position in the opposite direction.
   Cancelling first, not after: if cancel fails and the market-close step is skipped, the row
   stays `OPEN` (visibly overdue, retried next tick — a bounded, monitored state). If the
   position were closed first and cancel then failed, the trade would show `CLOSED` (nobody looks
   again) while a dangling live order silently remains — the strictly worse failure mode.
2. **Market-close the position**: reuse the existing `OkxTradingClient.closePositionMarket(instId,
   side)` unchanged — already idempotent (no-ops if already flat).
3. **Verify flat**: reuse `getPositionContracts(instId) == 0`, matching the existing
   `emergencyClose` pattern.
4. **Persist the close**: reuse `BotTradeService.close(tradeId, new CloseBotTradeRequest(exitPrice,
   null, null))` unchanged — it already computes `pnlPln`/`pnlPct` from `entryPrice`/`quantity`/
   `leverage` when not supplied, and sets `status=CLOSED`, `closedAt=now`. `exitPrice` is the
   current cached mark price (`PriceCacheService`, same lookup `BotMapper`/`LiveExecutionService`
   already use for BTC-USD) — a best-effort approximation, same precision level the existing
   `emergencyClose` path already accepts (it uses `entryPrice` as its own approximation).
5. **Audit**: a `SYSTEM`-sourced `bot_trade_action` row, new `BotTradeActionType.HARD_CLOSE_TIMEOUT`,
   written directly via `BotTradeActionRepository` (bypassing the request-DTO/validation layer,
   matching `BotScalpSessionService`'s own precedent for system-originated rows) — on both success
   (`reasoning` prefixed `"HARD_CLOSE_TIMEOUT"`) and on a failed/retried attempt (prefixed
   `"HARD_CLOSE_TIMEOUT_RETRY"`), so a human watching the trade-history page sees a failed attempt
   immediately, not only in backend logs.

### 4. Failure modes

Any exception from `cancelAlgoOrder`/`closePositionMarket`/`getPositionContracts`, or a
still-nonzero position after market-close, is treated uniformly: log `ERROR` with full context
(tradeId, okxAlgoId, order/position state, error code/message), write the `RETRY`-prefixed audit
row, release the claim (`CLOSING→NONE`). No retry cap, no backoff — see §2's reasoning. This
differs from ADR-090's `FAILED`-with-`tradeId` entry-side handling (which gives up and hands off
to a human, because that failure means the trade row itself is untrustworthy) — here the trade
row *is* trustworthy (it's a known, real, still-open position), so continued automated retry is
both safe (idempotent primitives) and strictly better than giving up. If OKX failures persist
indefinitely, the only escalation is the existing established precedent for this whole module:
loud `ERROR` logs + an audit trail a human is expected to notice — no new alerting infrastructure
is introduced (none exists anywhere in this codebase today; out of scope, noted as an open risk).

### 5. Independence from the host-side Claude pipeline (the load-bearing requirement)

`BotHardCloseWatcherService` runs entirely in-process in the backend container. It has no
dependency on `scalp-launcher.sh`, `scalp-session.sh`, the `claude` CLI, or its host-side
subscription auth — it works identically whether the scalp session claimed the trade and is
mid-beat, errored out, was killed by its own `timeout 900`, or (today's real, verified incident)
`scalp-launcher.sh` was never installed as a systemd service at all since ADR-092 shipped. This is
the entire point: a true independent backstop cannot share a failure domain with the thing it is
backstopping.

### 6. Doc corrections shipped in this same change (not deferred)

`infra/bot/scalp-session.sh` and `infra/bot/manage-prompt.md` currently assert the false premise
in the present tense — including directly inside the prompt fed to the live trading LLM at every
scalp session. Since a live session could run before this ADR's mechanism is actually implemented
by Backend, the prose is corrected **now**, in this same change, to honestly describe *current*
reality (no automated time-based close exists yet; only the price-based SL protects an
unresponsive session past 15 minutes) with a pointer to this ADR and an explicit instruction for
Backend to flip the wording to present-tense-accurate once `BotHardCloseWatcherService` actually
ships (matching this repo's "living docs describe the system as it now IS" convention — never
describe a not-yet-shipped mechanism as real, in either direction).

`scalp-session.sh`'s own `timeout 900` **stays**, as defense-in-depth — but is recharacterized:
it is a process-level backstop only (kills a hung/stuck `claude -p` CLI process so it doesn't run
indefinitely burning API budget), never a position-closing backstop. It was never actually the
latter (see Context) — this ADR just stops the comments from claiming otherwise, and gives the
Pi host a real, independent reason `timeout 900` is still worth keeping even though it does
nothing for the position itself.

## Alternatives considered

| Alternative | Shape | Verdict | Why |
|---|---|---|---|
| **A. Host-side watchdog script** (a second `infra/bot/*.sh` cron/systemd loop polling `GET /bot/trades` and calling `PATCH /bot/trades/{id}/close` past 15 min) | Shell, host, mirrors `scalp-launcher.sh`'s own shape | **Rejected** | Directly contradicts §5, the load-bearing requirement: this ADR exists *because* the host-side pipeline (`scalp-launcher.sh`) was found silently absent in production for six days with nothing catching it. A watchdog that itself lives on the host inherits the exact same class of "was it ever installed, is it still running" failure this ADR is meant to close. It would also need its own OKX credentials on the host or a new backend endpoint to trigger the close — either widens the credential surface or re-invents an in-process job with extra hops. |
| **B. In-process `@Scheduled` job, backend-only (chosen)** | New `BotHardCloseWatcherService`, no new endpoint, no host dependency | **Chosen** | Runs inside the same container/process as `LiveExecutionService` and the OKX credentials it already holds; correctness does not depend on any other process, script, or host service being alive. Matches this module's own precedent of keeping trading-critical logic backend-internal (ADR-090's `LiveExecutionService`) while advisory/orchestration logic lives host-side (ADR-092). |
| **C. Extend `scalp-session.sh`'s `timeout 900` to itself call a close endpoint on expiry** (trap the `SIGTERM`/exit code and curl a close) | Shell, same process | **Rejected** | Still entirely dependent on the host-side script having been launched in the first place — does nothing for the exact incident that motivated this ADR (the launcher was never installed, so no `scalp-session.sh` process ever existed to time out). Also reintroduces the same "backend never spawns/depends on host process health" concern ADR-092 §Alternatives already rejected once for the entry-trigger direction; the reasoning applies symmetrically to the exit direction. |
| **D. Do nothing further; rely on the price-based SL alone** | No new mechanism | **Rejected** | This is the status quo the Context section shows is genuinely unprotected past 15 minutes if price never reaches the SL band — the exact gap this ADR exists to close, now confirmed live (not hypothetical) by the launcher-never-installed finding. |

## Consequences

- Backend implements exactly `.claude/tasks/148-architect-output.md` (Flyway `V72`, new
  `BotHardCloseWatcherService`, `OkxTradingClient.cancelAlgoOrder`, `BotExecutionProperties.hardCloseAfterSeconds`
  wired into `BotMapper`'s existing literal, `BotTradeActionType.HARD_CLOSE_TIMEOUT`, tests). Any
  deviation in the numbers (900s threshold, 5s poll) is an ADR amendment, not a tuning commit —
  these numbers move real money, same standing rule ADR-092 already established.
- `docs/architecture/06-data-flow.md` §11's specific false-premise bullet is corrected, and a new
  §13 documents this flow (bumping the existing BLS §12 down not required — §13 is simply the next
  number after the existing §12).
- `infra/bot/scalp-session.sh` and `infra/bot/manage-prompt.md` prose corrected in this same
  change (§6) — Backend does a second pass once the mechanism ships to flip the wording from
  "designed, not yet live" to "live," per this repo's "describe the system as it now IS" doc
  convention.
- **Explicitly out of scope, flagged separately (not built here):** wiring Claude's advisory
  actions (`SET_SL`/`SET_TP`/`TRAIL_SL`/`PARTIAL_CLOSE`/`FULL_CLOSE`) to actually execute on OKX.
  This is a materially larger, separate design problem (how does `TRAIL_SL` move the live SL
  trigger price on an exchange whose algo orders aren't natively amendable-in-place without
  cancel+replace; there is currently zero `SET_TP`/take-profit mechanism in `OkxTradingClient` at
  all) that deserves its own ADR and is not something this ADR's scope should absorb. Until that
  ships, `BotHardCloseWatcherService` is — along with the price-based SL — one of only **two**
  things that can ever end a bot-opened position automatically; a human/manual path is the third.

## Open risks (not blocking, tracked)

1. The exact OKX `sCode`/error code for "algo order already cancelled/triggered/not found" on
   `POST /api/v5/trade/cancel-algos` is unverified against live/demo OKX behavior — Backend must
   confirm this empirically (demo mode) before treating it as a benign "already gone" case rather
   than a hard failure; task 148 flags this explicitly rather than guessing a number.
2. No retry cap/backoff on OKX-call failure (§2/§4) is an accepted trade-off, not a verified-safe
   one — if OKX rate-limits repeated `cancel-algos`/`closePositionMarket` calls specifically (as
   opposed to the calendar-fetch 429s ADR-090/092 already observed from ForexFactory), that would
   need its own follow-up; no evidence either way yet.
3. `hardCloseAfterSeconds=900` and the poll `fixedDelay=5000` are reasoned defaults matching
   existing numbers already in `scalp-session.sh`/the frontend literal, not independently tuned
   against live trading data.
