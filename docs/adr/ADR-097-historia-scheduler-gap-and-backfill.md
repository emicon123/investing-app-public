# ADR-097: Historia/Bot Scheduler Gap (2026-09-18) — Root Cause, Fix, Backfill Mechanism

**Date:** 2026-09-19
**Status:** Accepted
**Related:** `.claude/tasks/153-historia-scheduler-gap-incident.task.md` (full incident diagnosis,
already verified by the orchestrator directly against the running system/DB before this ADR was
written), `.claude/tasks/go-rewrite-phase-9-retire-java.task.md` (the task whose own gate this
incident jumped ahead of), `docs/adr/ADR-094-go-backend-rewrite.md` (the strangler-fig migration
this incident is a burn-in issue within), `docs/architecture/13-go-backend-target-architecture.md`
§7 (Phase 3/5 rows, updated alongside this ADR), commit `16e3433` (the container-stop that caused
this), `backend-go/internal/historia/{scheduler,service}.go` and the sibling apps' equivalents.

## Context

The 23:00 Europe/Warsaw historia-snapshot cron and the bot watcher/purger cron were both migrated
to Go behind a shadow gate — `HISTORIA_SCHEDULER_ENABLED` / `BOT_SCHEDULER_ENABLED`, both default
`false` — so that during the strangler-fig cutover, Java remained the single writer for these two
scheduled jobs until the flag was deliberately flipped. The intended sequencing, per
`go-rewrite-phase-9-retire-java.task.md`, was: flip both flags to `true` (Go becomes the writer),
confirm it runs cleanly, *then* stop the Java containers as part of Phase 9 ("Retire Java").

Commit `16e3433` (2026-09-18 09:00, "chore(infra): stop all 3 Java backend containers...") did the
container-stop half of that sequence on its own, without flipping either flag first. Phase 9's own
task file was — and, at the time this incident was found, still is being corrected to reflect —
marked "Not started," meaning the container-stop happened outside that task's own Architect→DevOps
gate. Net effect: from 2026-09-18 09:00 onward, no backend fired the 23:00 historia snapshot for
any of the 3 apps' portfolios (Java: stopped; Go: gated off). Confirmed directly against
`shared-infra-db-shared-1`: no 2026-09-18 row in any app's `historia` table; last row everywhere is
2026-09-17. `BOT_SCHEDULER_ENABLED` has the identical gap (root app only), with no observed real
harm since `bot_trade` was empty at the time — same bug, closed at the same time as a matter of
consistency, not because it caused damage.

## Decision

### 1. Flip both flags now, no double-write risk

`HISTORIA_SCHEDULER_ENABLED=true` and `BOT_SCHEDULER_ENABLED=true` (root only for the latter) are
added to all 3 apps' `backend-go*` compose env. Because the Java containers are already stopped,
there is zero double-write window to worry about — the usual shadow-gate risk (Go ON + Java ON
double-firing) cannot occur; Java physically cannot fire. DevOps executes this (compose edit +
restart), not this ADR.

### 2. Backfill mechanism: extend `SnapshotPortfolio` with an explicit date parameter, reusing all existing computation logic — not a raw SQL script

`historia.Service.SnapshotPortfolio` (root: `backend-go/internal/historia/service.go:165`;
portfolio-b/portfolio-c: `Snapshotter.SnapshotPortfolio` in each app's `internal/historia/snapshot.go`) always
resolves `today := warsawDate(s.now())` internally and upserts against that date — it has no way to
target an arbitrary past date as written.

**Chosen approach:** split the existing method into an unexported/parameterized core that takes an
explicit target date, and keep the existing public method as a 1-line wrapper over it — zero
behavior change to the live scheduled path:

```go
// existing body, with `today` replaced by the `asOf` parameter throughout
func (s *Service) snapshotPortfolioAsOf(ctx context.Context, portfolio string, asOf time.Time) error { ... }

// unchanged call sites (scheduler, any other caller) — behavior identical to today
func (s *Service) SnapshotPortfolio(ctx context.Context, portfolio string) error {
    return s.snapshotPortfolioAsOf(ctx, portfolio, warsawDate(s.now()))
}

// new: the one-off backfill entry point
func (s *Service) BackfillSnapshot(ctx context.Context, portfolio string, date time.Time) error {
    return s.snapshotPortfolioAsOf(ctx, portfolio, dateOnly(date))
}
```

(Portfolio B's `Snapshotter.SnapshotPortfolio(ctx)` has no `portfolio` parameter — same split, no
portfolio arg. Portfolio C mirrors root's shape.)

`BackfillSnapshot` reuses `computeValues`/`computeValuesOrZero` (current `positions` +
`price_cache`), `totalDepositsPln`, and `xirrOrNull` completely unchanged — the same values the
live 23:00 job would have computed, just upserted under an explicit `snapshot_date` instead of
"today." This was chosen over the two alternatives considered:

- **A raw one-off SQL/script backfill** (compute values outside Go, `INSERT` directly) — rejected:
  bypasses the FX conversion, XIRR calc, and JSONB encoding logic that already exists and is
  tested; a hand-rolled duplicate of that logic for a one-time write risks a silent numeric drift
  bug in a money-adjacent table, for no benefit over reusing the real path.
- **Reusing portfolio-b/portfolio-c's existing `internal/admin` position-values backfill
  (`AdminController`'s `POST /admin/history/backfill-position-values` precedent)** — investigated
  and rejected as a direct fit: that mechanism only fills the `position_values` JSONB breakdown on
  **existing** rows whose top-level `portfolio_value` is already populated (it rewinds units
  through purchase lots/transfers and prices them off historical Yahoo OHLC, then scales the
  breakdown to match the stored total). This incident's rows don't exist at all — there is no
  `portfolio_value` to scale against. The two problems are shaped differently; only the "add a date
  parameter to the existing snapshot computation" approach fits a **missing row**, not an
  **incomplete row**.

**Known, accepted limitation:** `BackfillSnapshot` prices off the *current* `price_cache`, not a
historical close for 2026-09-18 specifically — the live 23:00 job has never done point-in-time
historical pricing either (it always upserts against whatever `price_cache` holds at fire time), so
this is consistent with the job's existing behavior, not a new inaccuracy introduced by backfilling
late. The resulting `portfolio_value` reflects prices as of whenever the backfill actually runs
(the morning of 2026-09-19), not the true 2026-09-18 23:00 close. For a single missed day on a
personal chart/history feed (not a tax or ledger figure), this is accepted as good enough —
building historical OHLC-based point-in-time reconstruction (mirroring portfolio-b/portfolio-c's
position-values backfill) was considered and rejected as disproportionate engineering for a
one-day gap, per YAGNI.

**Wiring:** one admin-only HTTP entry point per app, called manually once (not registered in the
public OpenAPI contract, not linked from any UI — same convention as portfolio-b/portfolio-c's existing
`/admin/history/backfill-position-values`, which is likewise undocumented ops tooling):

- Root: `POST /admin/historia/backfill-snapshot`, body `{"date":"2026-09-18"}` — no portfolio
  field; loops internally over `trackedPortfolios` (`MAIN`, `RETIREMENT`), one call backfills both.
  Root has no existing `internal/admin` package (portfolio-b/portfolio-c do); rather than introduce one for a
  single method, this endpoint is added as a new handler method directly on the existing
  `historia.Handler`/`historia.Service`, wired next to `GET /api/history` in `main.go` — consistent
  with root's own package layout, YAGNI against standing up empty new scaffolding.
- Portfolio C: same shape as root, looping over `VARIABLE`/`FIXED`. Added to the **existing**
  `internal/admin` package/handler (same file that already serves
  `/admin/history/backfill-position-values`) as a second method — no new package needed.
- Portfolio B: `POST /admin/historia/backfill-snapshot`, body `{"date":"2026-09-18"}`, single portfolio,
  no loop. Same existing `internal/admin` package as portfolio-c's.

Called directly against each container (`docker compose exec`/container-internal port, e.g.
`backend-go:8091`), **not** through Nginx/Tailscale — this is a one-shot internal ops action, not a
public route, so no Nginx location block is added for it (keeps the public attack surface
unchanged and stays within this ADR's own scope: routing/location blocks are DevOps/Nginx's
concern, not touched here).

### 3. Mandatory pre-write sanity check: current position state must equal 2026-09-18 EOD state

`BackfillSnapshot` reads **current** `positions` rows (today's units), which is only a valid proxy
for 2026-09-18's end-of-day state if nothing changed those units between 2026-09-18 21:00 UTC (23:00
Warsaw) and now. **Backend-Go must verify this before calling `BackfillSnapshot` for any
portfolio**, by querying each app's purchase/deposit/transfer write tables
(root: `purchase_sessions`/`purchase_session_items`/`deposits`/`binance_transfers`; portfolio-b/portfolio-c:
`purchase_lots`/`deposits`/`position_transfers` — exact table set per app, confirmed against each
app's own Flyway history) for any row whose timestamp column falls in that window. If the check
comes back empty for a given app, proceed with `BackfillSnapshot` using current state. **If it
finds any row in that window, stop and do not write a backfilled snapshot for that app** — ask the
user directly, or reconcile manually — rather than attempting an automatic point-in-time rewind
(the same YAGNI reasoning as the pricing limitation above: building unit-rewind machinery is only
justified if the check actually finds a conflict, not speculatively).

## Consequences

- Backend-Go implements `BackfillSnapshot`/`snapshotPortfolioAsOf` in all 3 apps' historia
  packages, plus the one admin endpoint per app described above, per the exact shapes in Decision
  §2. Any deviation is an ADR amendment, same standing rule this repo's other ADRs already
  establish.
- Backend-Go runs the Decision §3 trade-window check before writing any backfilled row, for every
  app, and reports the result (clean / found conflicting rows) before proceeding.
- DevOps flips `HISTORIA_SCHEDULER_ENABLED=true` (all 3 apps) and `BOT_SCHEDULER_ENABLED=true`
  (root only) in compose, restarts the 3 `backend-go*` containers, and verifies each logs the
  scheduler as enabled at startup.
- `docs/architecture/13-go-backend-target-architecture.md` §7's Phase 3 and Phase 5 rows are
  updated in this same change to describe the scheduler write-path as GoOwned, current fact (not a
  pointer back to this ADR) — per this repo's documentation convention of keeping architecture docs
  self-sufficient.
- `infra/nginx/nginx.conf`'s four stale "scheduler stays OFF on Go" comment blocks (root historia,
  root bot, portfolio-b historia, portfolio-c historia) are corrected in this same change to state the current,
  post-flip reality.
- `go-rewrite-phase-9-retire-java.task.md` gets a short status note recording that its
  container-stop sub-step already happened out of band (commit `16e3433`) ahead of this task's own
  gate, and that this incident is the documented fallout — not a rewrite of that task file.
- This ADR's own reasoning (why the position-values-backfill precedent didn't fit, why a date
  parameter beats a raw script, the accepted pricing/trade-window limitations) has lasting reference
  value beyond the one-line "GoOwned" fact now in doc 13 §7, so it is kept standalone rather than
  fully folded in and deleted.
