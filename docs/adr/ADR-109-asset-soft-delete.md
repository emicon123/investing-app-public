# ADR-109: Soft Delete for Assets and Subassets (All Three Apps)

**Date:** 2026-11-04
**Status:** Accepted (task 173). Design: `docs/architecture/diagrams/173-asset-soft-delete.md`.
**Scope:** main, portfolio-b, portfolio-c. Migrations main V209, portfolio-b V20, portfolio-c V14.

## Context

A hard `DELETE` of an asset fails in portfolio-b with a 500 (`position_transfers_ticker_fk`, SQLSTATE 23503):
VVMX, asset 22, has 0 units after a transfer. `purchase_lots_ticker_fk` and portfolio-c's
`*_ticker_portfolio_fk` block the same way. Main has no such FKs, so there a delete silently orphans the
history's ticker references. The owner's rules: deleting never removes history, and history never blocks a
delete. The deleted asset only disappears from the views of current holdings.

## Decision

1. `assets.deleted_at` and `positions.deleted_at` (`TIMESTAMPTZ NULL`, `NULL` = live) in all three apps.
   `DELETE /assets/{id}` and `DELETE /positions/{id}` set it. No row in any table is hard-deleted any more.
2. An asset delete cascades the soft delete to its live subassets in one statement. A subasset delete keeps
   today's rules (primary with live siblings → 409; last live subasset → its asset too). No new units guard.
3. History is never deleted or filtered: transfers, purchase lots and history, purchase sessions, historia
   snapshots, trade chart, offline tools. Every current-holdings read is live-only.
4. Re-adding a deleted ticker (`POST /positions`) **restores the old row** (same `id`, units, cost, history).
   Unique keys keep covering deleted rows, so the restore is found by the natural key.
5. The existing history FKs stay. Main gets **no** new FKs: its history rows carry no `exchange` and include
   tickers hard-deleted earlier, so there is no FK target without rewriting history.
6. The delete/restore decision is a pure kernel in `packages/common/go/common-go/assetlifecycle`. SQL and
   field mapping stay per app.

## Alternatives rejected

- **Archive tables:** they break the history FKs and make restore a copy-back.
- **`ON DELETE SET NULL` or dropping the FKs:** history would lose its link (owner rule 1).
- **A `live_positions` view:** sqlc would generate a separate row type and break the shared decoders.
- **Opt-in filtering (add `deleted_at IS NULL` only where needed):** a new reader would see deleted rows by
  default. Live-only by default, with explicit `…IncludingDeleted` history queries, fails safe.

## Consequences

- Every `SELECT *` model gains `DeletedAt`. The sqlc output and unit-test fakes change in all three apps.
- A deleted ticker's `price_cache` row goes stale. History views that value lots at the current price show
  that last price.
- Restore keeps old optional settings (targets, Yahoo ticker, …) unless the create request sends new ones.
  Other deleted subassets of a restored asset stay deleted until each is re-added.
