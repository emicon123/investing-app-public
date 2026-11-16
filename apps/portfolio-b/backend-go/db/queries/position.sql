-- Phase 8 position queries — owns `positions` (task 151 diagram §1 write-ownership cluster)
-- plus the one write into `purchase_lots` PurchaseConfirmService pairs with a delta write, and
-- `position_transfers`' own writes (positiontransfer.sql). ~handful of rows total (single
-- portfolio, no portfolio column) — every read here is a full-table SELECT; filtering/grouping
-- happens in Go, matching PositionService's own load-all-then-filter shape in Java.
--
-- Task 156 (Asset / Subasset model): positions rows are subassets grouped by asset_id
-- (assets.sql owns the group root). is_companion_row / companion_parent_ticker /
-- target_allocation_pct are gone (dropped V19 — targets live only on assets); leadership is
-- positions.is_primary (partial UNIQUE per asset) and group reads join assets in Go.
--
-- Two genuinely different write paths on the SAME table, per architect-output §2's root
-- precedent: UpdatePositionDelta (delta-apply, shared by internal/rebalancing's confirm path
-- and internal/positiontransfer) vs. UpdatePositionFields (PositionsController's own direct
-- field edits) vs. ClearAssetPrimary/SetSubassetPrimary (assets.sql — the set-primary flip).
-- Never merge these.

-- Task 173 (ADR-109) soft delete: the existing read names are LIVE-ONLY (deleted_at IS NULL);
-- history readers opt in to …IncludingDeleted, the restore path to …AnyState.

-- name: ListAllPositions :many
SELECT * FROM positions WHERE deleted_at IS NULL ORDER BY ticker;

-- name: ListAllPositionsIncludingDeleted :many
-- History read (GET /purchases/history): names/yahoo tickers of deleted assets stay resolvable.
SELECT * FROM positions ORDER BY ticker;

-- name: GetPositionByID :one
SELECT * FROM positions WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPositionByTicker :one
-- Direct port of PositionRepository.findByTicker — used by PurchaseConfirmService's confirm
-- path and internal/positiontransfer's executeTransfer (both look up a position by ticker
-- alone; this app has no portfolio column to also scope by).
SELECT * FROM positions WHERE ticker = $1 AND deleted_at IS NULL;

-- name: GetPositionByTickerAnyState :one
-- Restore-on-create only: ticker is portfolioB's natural key, UNIQUE incl. deleted rows.
SELECT * FROM positions WHERE ticker = $1;

-- name: InsertPosition :one
INSERT INTO positions (
    ticker, name, exchange, asset_class, exposure, price_currency,
    units_ike_tata, units_ike_mama, units_ikze_tata, units_ikze_mama, units_inne,
    asset_id, is_primary,
    yahoo_ticker, sort_order
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13,
    $14, $15
) RETURNING *;

-- name: UpdatePositionFields :one
-- The direct-edit write path (PositionsController's PUT) — full-row overwrite of every
-- user-editable field, computed by the service from a read-modify-write over the existing row
-- (Go has no JPA dirty-checking). NEVER shares a code path with UpdatePositionDelta.
-- There is no target column: targets live only on the parent asset (PUT /assets/{id}).
-- asset_id is part of the overwrite (task 156 §13, re-homing a non-primary subasset): the
-- service MUST pass the CURRENT asset_id when the client did not ask to move the row —
-- omitting it would write 0 (rejected by the FK, but still a bug).
UPDATE positions SET
    units_ike_tata = $2,
    units_ike_mama = $3,
    units_ikze_tata = $4,
    units_ikze_mama = $5,
    units_inne = $6,
    total_spent_pln = $7,
    avg_buy_price = $8,
    name = $9,
    group_name = $10,
    asset_class = $11,
    yahoo_ticker = $12,
    sort_order = $13,
    asset_id = $14
WHERE id = $1
RETURNING *;

-- name: UpdatePositionDelta :one
-- The ONE canonical delta-write query — sets the five per-account unit buckets plus
-- total_spent_pln, called after position.ApplyDelta mutates the in-memory units value and the
-- common-go/costbasis kernel has computed the asset-level cost (task 161a-3). Every caller (internal/rebalancing's confirm path, internal/positiontransfer's
-- executeTransfer) declares this exact method name in its own narrow Queries interface.
UPDATE positions SET
    units_ike_tata = $2,
    units_ike_mama = $3,
    units_ikze_tata = $4,
    units_ikze_mama = $5,
    units_inne = $6,
    total_spent_pln = $7
WHERE id = $1
RETURNING *;

-- name: UpdatePositionCost :exec
-- Task 161a-3: writes ONLY total_spent_pln of a sibling subasset row whose recorded cost changed
-- because a trade on another row of the SAME asset re-scaled the asset's cost (asset-level average
-- cost, common-go/costbasis kernel). The trade's own row keeps going through UpdatePositionDelta
-- (units + cost in one statement); both run inside the trade's transaction so cost and units commit
-- or roll back together. Never used by the manual PUT edit path (units edits never change cost).
UPDATE positions SET total_spent_pln = $2 WHERE id = $1;

-- name: SoftDeletePosition :exec
-- DELETE /positions/{id} of a non-primary subasset with live siblings (task 173).
UPDATE positions SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL;

-- name: RestorePosition :one
-- Restore-on-create (task 173 §6.1). Keeps id, ticker, units, total_spent_pln, avg_buy_price,
-- group_name, created_at. Required request fields overwrite; optional ones keep the old value
-- when the request omits them (the service passes the raw request pointers).
UPDATE positions SET deleted_at = NULL,
    asset_id = sqlc.arg(asset_id), is_primary = sqlc.arg(is_primary),
    name = sqlc.arg(name), asset_class = sqlc.arg(asset_class), price_currency = sqlc.arg(price_currency),
    exposure     = COALESCE(sqlc.narg(exposure), exposure),
    yahoo_ticker = COALESCE(sqlc.narg(yahoo_ticker), yahoo_ticker),
    sort_order   = COALESCE(sqlc.narg(sort_order), sort_order)
WHERE id = sqlc.arg(id) AND deleted_at IS NOT NULL
RETURNING *;
