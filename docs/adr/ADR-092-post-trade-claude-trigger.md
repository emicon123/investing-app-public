# ADR-092: Post-Trade Claude Trigger — Outbox Flag on `bot_trade` + Host-Side Launcher

**Date:** 2026-09-10
**Status:** Accepted
**Related:** `backend/.../service/bot/EventReleaseWatcherService.java`,
`backend/.../service/bot/LiveExecutionService.java` (`ExecutionResult(Outcome, tradeId)`),
`backend/src/main/resources/application.yml:77`
(`BotExecutionProperties.actualsRefreshSeconds`),
`BotCalendarSyncOrchestrator.syncAll()`, `docs/adr/ADR-090-okx-live-execution.md`
(watcher cadences, guards, failure modes), `docs/adr/ADR-089-bot-cron-get-machine-auth.md`
(`X-Service-Token` machine-auth pattern), `docs/adr/ADR-091-ppi-yoy-calendar-enrichment.md`
(TE sibling gate), `infra/bot/scalp-session.sh`, `infra/bot/manage-position.sh`,
`infra/bot/watch-market.sh`, `infra/bot/manage-prompt.md`,
`.claude/tasks/146-post-trade-claude-trigger.task.md`

## Context

User-required release-time flow (translated from Polish):

1. Data publishes at e.g. 14:30 → bot refreshes calendar actuals as fast as possible
   (target: catch within ~10 s; after ~30 s the price move is mostly done, pointless).
2. The algorithm opens an OKX position per the 06:00 morning plan.
3. **ONLY THEN** Claude is launched, already knowing which position is open —
   ideally it reads the position back itself to confirm.

Verified current state (used as-is, not re-discovered):

- `EventReleaseWatcherService`: `@Scheduled fixedDelay=1000` tick; `maybeRefresh()`
  re-polls actuals at most every `actualsRefreshSeconds` (= 20 s) and only when a
  scenario cluster is inside the watch window (`watch-before 30 s` / `watch-after 300 s`,
  `cluster-wait 20 s`). Refresh goes through `BotCalendarSyncOrchestrator.syncAll()` =
  FF JSON feeds (`thisweek`+`nextweek`, 2 HTTP fetches, HTTP-429 → `backoffUntil`
  Retry-After or +5 min default, watcher skips refresh while backed off) + TE HTML
  scrape (only on PPI days via the sibling gate, no backoff handling, fail-soft).
- `LiveExecutionService.execute()` returns `ExecutionResult(Outcome, tradeId)` with
  outcomes `PLACED / SKIPPED_PAUSED / SKIPPED / FAILED / EMERGENCY_CLOSED`. On `PLACED`:
  market order filled, exchange-side SL algo placed, trade row persisted. `FAILED` with
  non-null `tradeId` can mean filled-on-exchange-but-not-flat (human path).
- Post-trade Claude scripts **already exist** in `infra/bot/`: `scalp-session.sh <tradeId>`
  (continuous 15-min advisory session — reads the trade back via `GET /bot/trades/{id}`,
  which **is** the user's "read back to confirm" requirement already implemented; polls
  via `watch-market.sh` beats, posts to `/bot/trades/{id}/actions`; hard `timeout 900`),
  `manage-position.sh <tradeId>` (one-shot), `watch-market.sh`, `manage-prompt.md`.
  **But nothing ever invokes them** — the backend never triggers any of them after a
  `PLACED` outcome. This missing trigger is the gap this ADR closes.
- Safety invariants: kill switch `bot_control.trading_enabled`; the executor's own
  15-min hard close + the exchange-side SL are the real backstop; Claude is advisory
  only. Cost-free, RPi Docker host, Tailscale — no new SaaS, no new infra services.

Ordering constraint is load-bearing: Claude must never start before the position row
exists, and must always start from the persisted row (read-back-to-confirm), never from
an in-memory handoff that a restart could lose.

## Decision

### 1. Refresh cadence: `actualsRefreshSeconds` 20 s → **5 s** (single knob, this task)

> **Amendment 2026-09-29:** `refreshStartOffsetSeconds` 5 s → **15 s** (poll window is now
> `T+15 s … T+60 s`; `staleAfterSeconds` 45 s unchanged). Cause: at the 14:00 UTC JOLTS /
> CB Consumer Confidence release the Go watcher's syncs at T+6/T+11/T+17 s hit a 429 on
> `ff_calendar_thisweek.json`; the ~5 min backoff outlasted the window, no `actual` was ever
> caught and the (would-be LONG x4) cluster was missed. Polling before FF has posted the
> print only burned the rate budget. The 429 backoff itself is unchanged.

- 20 s is too slow for the stated budget: worst-case detect latency is one full refresh
  period **plus** FF's own upstream lag **plus** ~1 s tick/match. At 20 s the bot routinely
  catches the print at 15–25 s — inside the "mostly done by 30 s" tail, not the ~10 s target.
- 5 s gives ~2 refresh chances inside the first 10 s after FF posts the `actual`, i.e.
  realistic catch at ~5–8 s end-to-end (FF lag + ≤5 s poll + ~1–2 s match/execute per the
  "reacts in under 2 seconds" figure, which measures match+execute once `actual` is in the DB).
- Why not 1–2 s: that is the shape ADR-090 explicitly rejected — a `T−30 s → T+5 min`
  window at 1 s is ~330 outbound calls per event, and FF already 429'd manual probing
  with `Retry-After ≈ 5 min`. 5 s costs ≈66 `syncAll()` calls × 2 FF fetches ≈ 132 hits
  per worst-case window: 4× today's load, still ~5× under the naive 1 s poll. Accepted.
- Interaction with the 429→5 min-backoff trap (the real risk of going faster): unchanged
  mechanics — while `backoffUntil` is in the future the watcher **skips the outbound
  refresh but keeps evaluating DB state every 1 s**. So a backoff delays the *arrival* of
  the `actual`, never the *reaction* once it is there. Aggressive polling buys a faster
  catch in the common case at the price of a higher chance of being blind exactly inside
  one window in the rare case FF rate-limits us. That trade is accepted because the
  alternative (20 s) is slow in *every* window. If 429s prove chronic in production, the
  follow-up is split throttles (FF fast 5 s, TE slow 30 s / once-per-cluster) and/or the
  BLS/FRED actuals fallback already documented in ADR-091 §6 — **explicitly not this task**.
  TE needs no throttle change now: the sibling gate already limits it to ~12 PPI days/year,
  and it stays fail-soft with no backoff handling.

### 2. Trigger: outbox flag on `bot_trade` + host-side launcher (no backend spawn)

Backend sets the intent; the host launches Claude. Two halves:

**a) Outbox columns on `bot_trade` (Flyway `V70`, next free — head is `V69`):**

```sql
ALTER TABLE bot_trade ADD COLUMN scalp_session_status VARCHAR(16) NOT NULL DEFAULT 'NONE'
  CHECK (scalp_session_status IN ('NONE','PENDING','RUNNING','DONE'));
ALTER TABLE bot_trade ADD COLUMN scalp_session_claimed_at TIMESTAMPTZ NULL;
CREATE INDEX idx_bot_trade_scalp_pending ON bot_trade(id) WHERE scalp_session_status = 'PENDING';
```

- `LiveExecutionService` sets `scalp_session_status='PENDING'` **in the same transaction
  that persists the trade**, and only on the `PLACED` path (see matrix). Every other
  outcome leaves `NONE`.
- Claim protocol (atomic, restart-safe): launcher calls
  `POST /bot/trades/{id}/scalp-claim` → `UPDATE bot_trade SET
  scalp_session_status='RUNNING', scalp_session_claimed_at=NOW()
  WHERE id=? AND scalp_session_status='PENDING'`; 200 = claimed, 409 = already
  claimed/finished (someone else won). On session exit the launcher calls
  `POST /bot/trades/{id}/scalp-finish` → `STATUS='DONE'` + a `SYSTEM` row in
  `bot_trade_action` (`SCALP_SESSION_STARTED`/`SCALP_SESSION_FINISHED` with exit code)
  so the audit trail lives where all other trade decisions already live.
- A row stuck in `RUNNING` (launcher died mid-session) is **never auto-reclaimed** —
  fail-closed, no double session. Human resets to `PENDING` manually if a relaunch is
  genuinely wanted. This plus `maxConcurrentTrades=1` gives at-most-one scalp session
  per trade across any combination of backend/launcher restarts.

**b) Host-side launcher `infra/bot/scalp-launcher.sh` (new, host, not Docker):**

- Persistent loop (systemd user service, `Restart=always`), poll every **10 s**:
  `GET /bot/trades/pending-scalp` (or equivalent filter on existing list) with
  `X-Service-Token` → for each `PENDING` id, attempt claim → on 200, launch detached
  (`setsid ./scalp-session.sh <tradeId> &`) so the 15-min session never blocks the loop.
- Double single-flight: global `flock` on the launcher itself (no two launchers) +
  per-trade atomic DB claim (no two sessions for one trade). Launcher logs to
  `scalp-launcher.log`.
- Host-side is not incidental: the `claude` CLI subscription auth lives on the Pi host
  (user `wojtekrpi`), exactly like the `infra/news/news-research.sh` precedent (§06-data-flow
  §6: "the only flow where the trigger lives OUTSIDE Docker"). The backend container has
  no `claude` binary, no subscription auth, and must never grow either.
- Kill-switch semantics: the switch gates **entries**, not management. A trade that is
  already `OPEN` keeps its scalp session even if `trading_enabled` is flipped off
  afterwards — stopping advisory management of a live position would strictly reduce safety.
- `manage-position.sh` (one-shot) stays a manual/human tool; the automatic path always
  launches `scalp-session.sh` (continuous 15-min), never the one-shot.

End-to-end latency after the fill: launcher poll ≤10 s + `scalp-session.sh`'s opening
`GET /bot/trades/{id}` read-back (the user's confirm step, already in the script). Total
Claude-knows-position ≈ fill + ~10–15 s worst case — fine, because the position is
already open and protected; Claude manages the 15-min drift, not the entry.

### 3. Outcome → trigger matrix (normative)

| `Outcome` | `tradeId` | Launch scalp? | What happens |
|---|---|---|---|
| `PLACED` | non-null | **YES** | Same-tx `scalp_session_status='PENDING'`. Launcher claims → `RUNNING` → `scalp-session.sh`. |
| `SKIPPED_PAUSED` | any | NO | Kill switch did its job; signals recorded `executed=false`. No session — nothing to manage. |
| `SKIPPED` (HOLD net, guards, sizing veto, leverage-cap veto) | any | NO | No position opened by definition. |
| `FAILED` | null | NO | No position (rejected/error pre-fill). |
| `FAILED` | non-null (filled-but-not-flat) | **NO auto** | Trade row is untrustworthy by definition; a Claude session reading it back would confirm a lie. `ERROR` log with order/position ids; human path only (`manage-position.sh` by hand once reconciled). |
| `EMERGENCY_CLOSED` | any | NO | SL placement failed → position already market-closed and verified flat. Nothing to manage; log only. |
| `DUPLICATE_TRADE_WINDOW`-after-fill (filled on exchange, row skipped via 409 guard) | n/a (no row) | **NO auto** | Live position with **no trade row** — `scalp-session.sh <tradeId>` cannot address it (it requires a `tradeId`). Human flattens via OKX UI, then reconciles. A future orphan-position endpoint is explicitly out of scope (see task). |

Rationale for the three NO-auto rows: auto-management is only safe when there is exactly
one trustworthy row to attach the session to. `FAILED`-with-`tradeId` has a row in an
unknown state; the duplicate-window case has a position with no row at all. Both need a
human to establish ground truth first — launching Claude onto either would spend the
15-min advisory budget managing a fiction.

### 4. Staleness gate: no trade if the data is older than 30 s (normative)

User rule: if the `actual` is not caught within 30 s of `eventTime`, the price move is
already gone — the bot must **not open the position at all**, and (with no `PLACED`
outcome) Claude never launches. Neither the current watcher nor §3 above implements
this: today a feed delay of minutes still executes, up to `watch-after 300 s`.

- New knob `app.bot.execution.stale-after-seconds`, default **30**, on
  `BotExecutionProperties` + `application.yml` (same shape as the other watcher knobs).
- Check placement: in `EventReleaseWatcherService.evaluateCluster`, immediately before
  `executionService.execute()` — i.e. after the net `HOLD` and null-confidence exits —
  if `now.isAfter(cluster.eventTime().plusSeconds(staleAfterSeconds))`: add the cluster
  to `resolvedClusters`, drop its `clusterFirstActualAt` entry, log WARN
  (`STALE_SKIP eventTime=… age=…s`), and return **without creating signals, without
  executing, without any `PENDING` flag**. No `PLACED` ⇒ §3 yields no Claude session
  automatically. Applies to every event type (PPI m/m, CPI, NFP, PPI y/y alike).
- Accepted tension: `cluster-wait 20 s` runs from the *first* actual, so a multi-event
  cluster whose legs trickle in can legitimately cross 30 s from `eventTime` and be
  skipped even though each leg was "fresh". This is conscious, not a bug — by the user's
  premise the joint move is over anyway. Single-event clusters evaluate immediately once
  `actual` posts, so for them the gate bites only on genuinely late feeds.

## Alternatives considered

| Alternative | Shape | Verdict | Why |
|---|---|---|---|
| **A. Backend spawns `claude` (sync or async fire-and-forget)** | `LiveExecutionService` does `Runtime.exec("scalp-session.sh <id>")` after `PLACED` (even via virtual thread / `CompletableFuture.runAsync`) | **Rejected** | Three independent kills: (1) topology — backend runs in Docker, `claude` CLI + subscription auth live on the Pi host; the container would need a host-exec backdoor (docker socket, ssh) — a new privileged channel for a real-money path. (2) coupling — process-spawn latency/failure modes (`IOException`, zombie processes, heap-thread exhaustion) leak into the trading path even when "async"; a spawn stall must never delay the SL-placement/emergency-close sequence. (3) restart safety — an in-memory "launched" set dies with the JVM; after a crash the backend cannot tell launched from never-launched without the DB flag anyway, at which point this alternative has just re-invented the outbox badly. The sync variant additionally violates the explicit design constraint outright. |
| **B. Outbox flag + host launcher (chosen)** | DB `PENDING` flag set same-tx with the trade; host loop polls, atomically claims, launches detached | **Chosen** | Matches the repo's own precedent (news flow trigger lives on the host, backend is store-and-serve); zero new privileges (launcher reuses `X-Service-Token` GET/POST like `sync-calendar.sh`); restart-safe by construction (DB is the source of truth, claim is atomic); adds exactly one tiny migration + two small endpoints + one shell loop. Slower than spawn by one poll period (≤10 s) — irrelevant since the position is already protected and Claude is advisory. |
| **C. Cron-scan heuristic (no schema change)** | Host cron scans `GET /bot/trades` for `OPEN` rows opened in last N minutes with no session log line, launches for those | **Rejected** | No explicit intent bit means the heuristic *is* the trigger: restart vs. already-ran vs. never-started are indistinguishable without log-scraping across machines (backend log vs. host log). Time-window heuristics (`opened_at > now-15m`) misfire on clock skew/restarts (relaunch a finished trade's session) or miss (backend down >N min at fill → trade ages out of the window → never managed). The partial index on `PENDING` is cheaper than any scan, and the `SYSTEM` action rows give a queryable audit trail instead of `grep` across two hosts. Saving one migration is not worth a trigger that cannot state its own intent. |

Cron-at-1-min granularity was additionally rejected as the launcher cadence for any
alternative: it adds 0–60 s before Claude even starts — a large fraction of the 15-min
scalp window — which is why the launcher is a 10 s systemd loop, not a crontab line.

## Consequences

- Backend implements exactly the task file (V70 migration, same-tx `PENDING` on `PLACED`
  only, claim/finish endpoints on the `X-Service-Token` chain per ADR-089's
  conditional-routing shape, `actualsRefreshSeconds` default 20→5, `staleAfterSeconds`
  default 30 with `STALE_SKIP` semantics). Any deviation in the numbers (5 s, 30 s,
  10 s launcher poll, `PENDING/RUNNING/DONE`) is an ADR amendment, not a
  tuning commit — these numbers move real money.
- `docs/architecture/06-data-flow.md` §11 documents the new post-trade section; existing
  sections stay intact.
- `scalp-session.sh` itself is unchanged (its opening `GET /bot/trades/{id}` **is** the
  read-back-to-confirm step — no new "pass the position to Claude" plumbing needed).
- Follow-ups explicitly deferred (not this task): split FF-fast/TE-slow throttles;
  BLS/FRED actuals fallback (ADR-091 §6); orphan-position (duplicate-window) endpoint;
  launcher heartbeat/reclaim for `RUNNING`-stuck rows.

## Open risks (not blocking, tracked)

1. 5 s polling may still 429 inside a window despite the gate (FF limit behavior was
   probed once, informally) — mitigated by unchanged backoff + DB-poll separation; the
   fallback is a second actuals source, not a faster poll.
2. FF upstream lag (time from real BLS release to FF `actual` appearing) is unmeasured and
   dominates the budget — if it routinely exceeds ~10 s, no poll cadence recovers the
   10 s target and the entry edge must be re-examined with live data.
3. Launcher host-service installation (`systemctl --user enable`) is a manual Pi step
   outside `docker compose` — must be verified on the real host, not just in CI.

## Amendment 2026-09-11 — `stale-after-seconds` 30 s → 45 s

User decision, ahead of today's 12:30 UTC USD CPI cluster (the first live event since this
ADR's watcher/execution/trigger code went live): widen the staleness gate (§4) from 30 s to
45 s. Rationale volunteered was headroom against open risk #2 above — FF's own upstream lag
is still unmeasured on a real release, so 30 s left little margin if that lag turns out to be
larger than assumed. No other number in this ADR or ADR-090 changes; `actuals-refresh-seconds`
stays 5 s. `BotExecutionProperties.staleAfterSeconds` / `application.yml` updated accordingly.
