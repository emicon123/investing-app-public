# ADR-088 — Promote a Companion Row to Main (Swap Group Leadership)

**Date:** 2026-09-09
**Status:** Superseded by task 156 (Asset/Subasset model, deployed 2026-09-24) — companion columns and
`POST /positions/{id}/promote` no longer exist; leadership is `is_primary` flipped by
`POST /assets/{id}/primary`. See `docs/architecture/diagrams/156-asset-subasset-model.md` and
`02-domain-model.md` Decision 13. Kept as the record of the pre-156 design.
**Related:** `02-domain-model.md` §2.1 (companion-row CRUD, Decision 2/12/13),
`03-api-contract.md` (`POST /positions/{id}/promote`), `05-rebalancing-algorithm.md` §7.8/§8
(donor/substitution mechanics this must not break), `docs/architecture/diagrams/142-position-promote-companion.md`
(UML — read before implementing), ADR-060/061/063/065/075 (the donor/substitution machinery
being verified safe here), task `.claude/tasks/142-control-which-row-is-main.task.md`

## Context

The user asked to be able to control which row in a main/companion group is "main" — e.g.
swapping so `PL.ETNVIRBTCP` (the IKE/IKZE-eligible ETN) becomes the group's target-bearing row
and raw `BTC-USD` becomes its companion instead, without moving any units or recording a trade.
This is a pure relabeling: which physical row is authoritative for the group's
`target_allocation_pct`/`target_allocation_pct_risk_off` and its top-level `GET /positions`
listing.

No such operation exists today. `companion_parent_ticker`/`is_companion_row` are set once at
creation (`POST /positions`, or a Flyway seed) and, per `02-domain-model.md` §2.1, are documented
as immutable after that — "reassigning which parent it points at is a manual DB fix, not a UI
action." This ADR adds the first supported way to change them, but only through one atomic,
validated action that flips both rows in a group together — `PUT /positions/{id}` still cannot
touch these fields on its own.

**Why this is safe to build without touching the donor/substitution mechanics (§7.8/§8).**
Read `PositionService`, `MacroDeriskGate`, `PurchasePlanningService`, and
`RebalancingTransferService` end to end looking for anything that caches a
ticker→companion/parent relationship *across* requests, or that resolves a companion's parent by
any means other than a fresh DB read within the current request:

- `PositionService.listPositions`/`getPosition` re-fetch `companionsByParent` from
  `PositionRepository` on every call (batched, but always live) — §7.3's
  `effectiveTargetAllocationPct` interpolation and §7.6's zero-target sentinel both read
  `Position.getTargetAllocationPct()`/`getTargetAllocationPctRiskOff()` directly off freshly
  loaded rows, never a separate cached table.
- `MacroDeriskGate.computeOverrides` takes `List<Position>` as an argument and reads
  `p.getTargetAllocationPct()`/`getTargetAllocationPctRiskOff()` straight off each row — the
  risk-on/risk-off tables documented in §7.3.1 are *not* a hardcoded ticker→number map anywhere
  in code, they are exactly these two DB columns. A promote/demote's column swap is picked up
  automatically, with zero additional code to update.
- `PurchasePlanningService.findCryptoDonorCandidate` (§8.2) resolves each donor candidate's
  deviation via `deviationMap.get(p.getCompanionParentTicker())` — `deviationMap` and
  `allPositions` are both built fresh at the top of the same `computePlan` call
  (`positionRepository.findAll()`), so a promote committed before that call starts is fully
  reflected; there is no request-spanning cache to go stale.
- `PurchaseSessionService.completeSession` resolves every `PurchaseSessionItem` by a
  `(ticker, portfolio)` composite key, looked up fresh, and never reads `isCompanionRow` at
  completion time at all — that flag only matters for *planning-time* donor eligibility (§8.1
  steps 2/4), never for settling an already-planned item. A `PurchaseSessionItem` row stores its
  own concrete `ticker`, `plannedUnits`, `plannedValuePln` at planning time; promoting/demoting a
  row between a session's creation and its completion changes no data those fields depend on.
  **Consequence: promote needs no interlock with `purchase_sessions`.** A `PLANNED` session
  created before a promote, and completed after it, still applies unit deltas correctly — it
  simply won't reflect the new main/companion labeling in its own already-frozen item list,
  exactly as any other position edit mid-session (e.g. editing units by hand) already doesn't.
  If the user wants a promote reflected in an active plan, discarding and recreating the session
  is the existing, unchanged way to do that.

Conclusion: every consumer of `is_companion_row`/`companion_parent_ticker` re-derives it from the
DB per request. The only thing that needs to be correct is that the promote operation itself
commits a consistent state atomically — no downstream code needs to change or be made aware of
the new endpoint.

**One real constraint found while checking this:** `positions_companion_consistency` (V1) is a
plain, non-deferred `CHECK` — `(is_companion_row=false AND companion_parent_ticker IS NULL) OR
(is_companion_row=true AND companion_parent_ticker IS NOT NULL)` — evaluated at the end of each
statement, not deferred to `COMMIT`. Backend must set `isCompanionRow` and
`companionParentTicker` together on each affected entity before the transaction flushes (normal
JPA behavior when both are set on the same managed entity before `save`/flush), never as two
separate saves with a flush in between, or the intermediate state can violate the constraint.
There is **no** CHECK constraint tying `target_allocation_pct` nullability to `is_companion_row`
(only a 0–100 range CHECK, `positions_target_pct_range`/`positions_target_pct_risk_off_range`,
which allows NULL freely) — so no schema change is needed to allow a promoted row to carry a
non-null target or a demoted row to carry a null one.

## Decision

**New endpoint, one per app: `POST /positions/{id}/promote`** (root `pl.investing`,
`apps/portfolio-b` `pl.portfolio-b`, `apps/portfolio-c` `pl.portfolio-c` — all three; see
"Scope across apps" below). `{id}` is the numeric PK of the **companion row being promoted** —
reusing the same identifier `GET/PUT/DELETE /positions/{id}` already use, rather than a
`(portfolio, ticker)` composite path, since every position already has one.

No request body. Response: `200` with the existing `PositionSingleResponse` shape — `data` is
the promoted position (now `isCompanionRow=false`), with `subPositions` reflecting the new group
(the demoted old main now appears as a sub-row). No new DTOs, no OpenAPI schema additions beyond
the one new path — this reuses `Position`/`PositionSingleResponse` exactly as `GET /positions/{id}`
already returns them.

**Column changes, atomic, one `@Transactional` service method:**

| Row | `is_companion_row` | `companion_parent_ticker` | `target_allocation_pct` | `target_allocation_pct_risk_off` |
|---|---|---|---|---|
| Promoted (was companion) | `false` | `NULL` | ← old main's value | ← old main's value |
| Demoted (was main) | `true` | ← promoted row's `ticker` | `NULL` | `NULL` |
| Every *other* sibling companion in the group | unchanged | ← re-pointed to promoted row's `ticker` | unchanged | unchanged |

Nothing else moves: `unitsIke`/`unitsIkze`/`unitsRegularEur`/`unitsRegularUsd`/`unitsRegularPln`,
`totalSpentPln`, `avgBuyPrice`, `priceCurrency`, `purchaseCurrency`, `yahooTicker`, `assetClass`,
`exchange`, `exposure`, `companyBacked`, `name` all stay on whichever physical row already had
them. This is a **relabeling**, not a trade — no `asset_purchases` row is written, no units are
transferred, and the manual-sell-only rule is untouched (nothing here resembles a buy or sell).

**Validation, in order (all as `IllegalArgumentException` → existing `400 VALIDATION_ERROR`
handler, except the last):**

1. `{id}` must resolve to a position (`NoSuchElementException` → existing `404 NOT_FOUND`).
2. That position must have `isCompanionRow = true` — promoting an already-main row is rejected
   (nothing to do; the UI should not even offer the action on a main row).
3. `companionParentTicker` must be non-null (defensive — the create-flow's dropdown already
   guarantees this, but a promote endpoint should not assume upstream data is always clean).
4. `companionParentTicker` must resolve to an existing row with `isCompanionRow = false` in the
   **same `portfolio`** as the companion being promoted. A dangling pointer (parent renamed or
   deleted outside the normal cascade — theoretically possible only via direct DB edit, since
   `DELETE /positions/{id}` on a parent already cascades to its companions, per `02-domain-model.md`
   Decision 12) is rejected rather than silently promoting into an inconsistent group.
5. **After** applying the swap in-memory (before flush/commit), verify
   `Σ target_allocation_pct` across that portfolio's `isCompanionRow=false` rows is still exactly
   `100.0000`, and — **for MAIN only** — `Σ COALESCE(target_allocation_pct_risk_off,
   target_allocation_pct)` is too. This reuses the **existing** `RebalancingInvariantException`
   class and its existing `400 REBALANCING_INVARIANT_ERROR` handler (`GlobalExceptionHandler`) —
   no new exception type. This check is mathematically expected to always pass (the swap only
   relocates one row's already-summing pair of values to another row within the same portfolio;
   it recomputes nothing and redistributes nothing), so in practice it is a defensive assertion
   in the same spirit as the `DO $$ ... RAISE EXCEPTION` guard `V50`/`V51`/`V66` run after each
   target-table migration — a fail-loud tripwire against a future bug, not an expected runtime
   error path. RETIREMENT has no `target_allocation_pct_risk_off` concept (§7 of
   `05-rebalancing-algorithm.md` — macro de-risk is MAIN-only), so that half of the check is
   skipped there, matching how `V51`'s own guard is scoped `WHERE portfolio = 'MAIN'`.

**Reversible by construction.** Promoting B back immediately after promoting A returns the group
to its exact prior state (A demotes back to a companion of B, siblings re-point back to B) — a
mis-click has a one-click undo, which is worth calling out to Frontend-web as a reason a heavy
confirmation dialog is not required (a lightweight confirm is still reasonable UX, entirely
Frontend-web's call).

**No Flyway migration.** Confirmed against the schema (see "one real constraint" above): every
column this operation touches already exists and already permits the values this operation
writes. This is a plain runtime service-layer mutation via existing repository saves, exactly
like `PUT /positions/{id}` — not a one-off data migration.

**Scope across apps.** All three apps share the identical `is_companion_row`/
`companion_parent_ticker` model on their own `Position`/`PositionService`
(`pl.investing`/`pl.portfolio-b`/`pl.portfolio-c`, confirmed by reading all three), and the same
`packages/shared/ui/src/SubPositionRows.tsx` already renders every app's main/companion groups.
portfolio-b and portfolio-c have **no** `PurchasePlanningService`/`PurchaseSessionService` at all (confirmed
absent from both trees) — so for them this feature is *strictly simpler* than for root: there is
no donor/substitution mechanism to reason about, only the sibling-repoint + target-sum guard.
Per CLAUDE.md's unified-agent-ownership note and precedent (task 137 implemented
`deletePosition`'s cascade identically-but-independently in all three `PositionService` copies),
this ships as the same endpoint contract (`POST /positions/{id}/promote`, same response shape,
same validation order) implemented independently in each app's own backend, with the "Set as
main" UI control added once to the shared `SubPositionRows.tsx` and wired through each app's own
API client. RETIREMENT groups get the same control as MAIN, for the same reason RETIREMENT
already gets the same manual sell-assist UI as MAIN (§7.5) — the underlying model has no
portfolio distinction here, and the user's own framing ("especially in main portfolio") does not
rule RETIREMENT out, it only names the primary use case.

## Consequences

- One new endpoint per app (3 total), zero schema changes, zero changes to any existing
  rebalancing/planning/transfer code path — confirmed by the analysis above, not assumed.
- `PUT /positions/{id}` remains unable to touch `isCompanionRow`/`companionParentTicker`
  directly; `POST /positions/{id}/promote` is the only mutation path for these fields now, and
  it only ever flips them on a validated pair (plus re-pointing siblings), never on a single row
  in isolation — `02-domain-model.md` §2.1's "companion fields stay immutable after creation"
  statement is superseded by this ADR and corrected in place, not left to silently drift from
  what the code now does.
- Backend must remember the non-deferred-CHECK-constraint ordering constraint above
  (`is_companion_row` and `companion_parent_ticker` set together per entity before flush) — flagged
  explicitly so it isn't rediscovered the hard way via a `DataIntegrityViolationException` mid-swap.
- No interlock with `PLANNED` purchase sessions is added, by design (see Context) — a promote
  during an active session is safe but not retroactively reflected in that session's already-
  frozen item list; Frontend-web may optionally surface a soft warning toast when a `PLANNED`
  session exists, but nothing blocks on it (parallels how the app already treats any other
  position edit mid-session).
- Test coverage Backend should add per app: promote happy path (sums preserved, siblings
  re-pointed, computed fields sane for both rows post-swap); promote-of-already-main rejected;
  promote of a companion whose parent has since been deleted/renamed rejected; and, root only, a
  regression test that `PurchasePlanningService`'s crypto-donor selection (§8.2) still resolves
  the correct parent's `deviationPct` for a companion whose `companionParentTicker` was changed
  by a promote earlier in the same test — i.e. the donor lookup is genuinely re-derived per run,
  not accidentally memoized anywhere.

## Addendum, 2026-09-09 (same day) — cost-basis fields now move too, conditionally

Found live, same day, by the first real use of promote: the user relisted `QUTM.DE` under a
new ticker (`WQTM.DE`) by adding `WQTM.DE` as a companion the day before and promoting it to
main. `avgBuyPrice`/`totalSpentPln` are **not** part of the swap this ADR originally specified
("everything else stays put") — they stayed on the demoted `QUTM.DE` row, now a companion,
whose cost-basis fields `PositionService`'s `avgBuyPricePln`/`profitPct` computation never
reads (that computation only ever looks at the non-companion row). Net effect: the group's
displayed average price and profit% went blank post-promote, even though the money was never
lost — it isn't a display bug in the read path, it's that the promoted row (`WQTM.DE`, freshly
added, never independently traded) genuinely had `null` cost-basis fields of its own, and the
swap didn't move `QUTM.DE`'s real values over.

This is a legitimate, expected use of promote — not a misuse — so the swap now carries
`avgBuyPrice`/`totalSpentPln` too, but **only when the row being promoted has both fields
`null`** (no independent purchase history of its own under its current ticker). Real companion
rows that carry their own independently-tracked cost basis exist today (e.g. the ETN
companions under `BTC-USD`/`SOL-USD`/`XRP-USD`, which record real money spent on that specific
instrument, kept via `PositionDeltaApplicator`) — promoting one of those must never clobber its
own figure with the old main's. The condition is checked once, before either row is mutated, and
governs both sides symmetrically: if true, the promoted row inherits the old main's
`avgBuyPrice`/`totalSpentPln` and the demoted row's are nulled (mirroring how
`targetAllocationPct` already moves); if false, neither row's cost-basis fields are touched at
all, exactly as this ADR originally specified.

Implemented identically in all three apps' `PositionService.promotePosition`, with matching
test coverage (`promotePosition_companionWithNoCostBasis_inheritsOldMainsCostBasis`,
`promotePosition_companionWithOwnCostBasis_notOverwritten`). The live `QUTM.DE`/`WQTM.DE` group
was corrected directly (its promote had already run before this fix landed): `WQTM.DE` now
carries the avg-price/total-spent that were stranded on `QUTM.DE`.

A second, unrelated bug surfaced by the same incident: `WQTM.DE` had been added the day before
via root's `CreatePositionDialog.tsx` with `exposure = QUANTUM` — a deprecated enum value with
zero legitimate live rows (see `docs/architecture/05-rebalancing-algorithm.md`'s CASH-bucket
note on the same `ALTER TYPE ... DROP VALUE` limitation) that the dialog's `EXPOSURE_OPTIONS`
list nonetheless offered, labeled in a way that reads as a legitimate "quantum computing"
category. That same stale list was also missing `BONDS`/`EXPERIMENTS`/`EQUITY` entirely,
making it impossible to correctly classify most new positions. Fixed in the same session by
correcting `EXPOSURE_OPTIONS` to the five real, live exposure values and dropping `CASH`/
`QUANTUM`; `WQTM.DE`'s live row corrected to `EXPERIMENTS`, matching `QUTM.DE`'s original
classification. This bug is independent of the promote feature itself (it's in the "add a new
position" dialog) but was only noticed because of the same promote.
