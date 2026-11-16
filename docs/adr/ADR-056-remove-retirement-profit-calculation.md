# ADR-056: Remove Profit (Zysk) Calculation for RETIREMENT Positions

**Date:** 2026-07-23
**Status:** Superseded by ADR-104 (2026-10-18)

## Context

The "Zysk" (profit %) column on the `/retirement` page shows wrong/misleading values. `computed.profitPct` and `computed.avgBuyPricePln` on a `Position` are derived in `PositionService.computeFields()` (`backend/src/main/java/pl/investing/service/PositionService.java`, ~lines 313–330) from `position.getTotalSpentPln()` — a running "total spent" cost-basis figure that gets updated on buys/sells via `PositionDeltaApplicator.updateTotalSpentPln()`.

This cost-basis tracking is unreliable specifically for RETIREMENT positions: IKE/IKZE sub-account splits, manual "Sprzedaj" sells, and historical ETL-migrated data all interact with `totalSpentPln` in ways that don't hold up for RETIREMENT's account structure, producing wrong `profitPct`/`avgBuyPricePln` numbers. The same computation is **not** unreliable for MAIN — MAIN's profit display is correct today and must keep working unchanged.

Fixing the underlying cost-basis tracking for RETIREMENT (properly attributing `totalSpentPln` across IKE/IKZE splits, sells, and migrated rows) is a larger undertaking than the immediate bug warrants. The decision below is a deliberate stop-gap: remove the misleading display rather than ship a partial or unverified fix to the tracking itself.

## Decision

1. **Backend:** `computed.profitPct` and `computed.avgBuyPricePln` are always `null` for any `Position` where `portfolio == RETIREMENT`, regardless of whether `totalSpentPln`/`avgBuyPrice` are set on the row. This is a portfolio-scoped null-out added to `PositionService.computeFields()`, applied after the existing computation (or short-circuiting it) for RETIREMENT rows only. MAIN positions are entirely unaffected — `profitPct`/`avgBuyPricePln` continue to compute exactly as before (`valuePln / totalSpentPln - 1` and `avgBuyPrice × fxRate`, respectively).
2. **Frontend:** `PositionsTable` (shared between `/portfolio` and `/retirement`) gains a new prop that hides the "Zysk %" column specifically on the `/retirement` page, while it stays visible on `/portfolio` (MAIN).
3. **No API/DTO shape change.** Both `profitPct` and `avgBuyPricePln` were already declared `nullable: true` in the OpenAPI schema (`docs/openapi/components/schemas/positions.yaml`) and in the shared-ts Zod schema (`shared-ts/src/schemas/positions.ts`) before this change — RETIREMENT positions simply now consistently fall into the already-documented null case. This is a behavior/semantics change, not a contract change.
4. **`totalSpentPln` keeps being tracked and stored in the DB for RETIREMENT** exactly as before (`PositionDeltaApplicator.updateTotalSpentPln()` is untouched) — only the derived, surfaced `profitPct`/`avgBuyPricePln` fields are nulled out at the API layer. This is intentionally **not** a fix to the underlying cost-basis tracking; it is a removal of a derived display that cannot currently be trusted for RETIREMENT.

## Consequences

- The `/retirement` page loses its profit-percentage display entirely (both the raw field and the UI column). Users lose the ability to see per-position profit% for retirement holdings until/unless someone revisits RETIREMENT cost-basis tracking properly (IKE/IKZE-split-aware, sell-aware, migration-aware attribution). This ADR does not propose or schedule that follow-up work — it only removes the misleading number.
- `/portfolio` (MAIN) is completely unaffected: `profitPct` and `avgBuyPricePln` keep computing and displaying exactly as they do today.
- `totalSpentPln` and `avgBuyPrice` remain live, mutable, stored columns on RETIREMENT rows (still updated by buys/sells and still editable via `PUT /positions/{id}`) — they are just no longer surfaced as a computed profit figure. A future fix to the tracking would not require a schema change, only a change to how these already-stored values are used at the API layer.
- No Flyway migration, no schema change, no OpenAPI contract change — this ADR is a documentation-plus-behavior record for a change entirely inside `PositionService` (backend) and `PositionsTable` (frontend).

## Amendment (2026-09-25, task 161)

The formulas quoted in Decision 1 (`valuePln / totalSpentPln - 1`, `avgBuyPrice × fxRate`) describe the pre-Asset (pre-156) Java implementation and are no longer current. Today `profitPct` is the asset-level `Σvalue / Σcost − 1` and `avgBuyPricePln` is the asset's entry price `pricePln(primary) / (1 + profitPct)`, both derived from the recorded `total_spent_pln` sum, never from `avg_buy_price` (see `docs/architecture/02-domain-model.md` §2.1, "Asset-level cost, profit and entry price"). The RETIREMENT null-out itself is unchanged and lives in the Go `computeFields`; it is lifted only once RETIREMENT's owner-reviewed cost baselines exist (task 161, decision D5).

## Superseded (2026-10-18)

That condition was met on 2026-10-18. Flyway V129–V147 rebuilt the whole RETIREMENT ledger, a cost run
replayed it through the unmodified `costbasis.PlanTrades`, and `V148__ret_cost_run_entry_prices_167.sql`
wrote the cost of all 15 RETIREMENT rows (task 167, M59/M60). **ADR-104** removes the RETIREMENT
null-out and the `/retirement` page's `showProfit={false}`, so RETIREMENT computes profit and entry price
exactly like MAIN. The text above is kept as the historical record.
