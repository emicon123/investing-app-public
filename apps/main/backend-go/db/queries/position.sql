-- Phase 7 position queries — owns `positions` (doc 13 §6.3 write-ownership cluster) plus the
-- one write into `asset_purchases` every position-delta caller pairs with it. See
-- .claude/tasks/150-architect-output.md §2: "one canonical delta-write query, shared via a
-- pure function, not a shared service object" — UpdatePositionDelta is that one query, called
-- from internal/position, internal/rebalancing, and internal/purchasesession/execution.go, each
-- declaring it in their own narrow Queries interface (structural typing — no import of this
-- package needed to call it).
--
-- ~30 rows across both portfolios total (Java's own repeated comment: "fine for single-user
-- app") — every read here is a full-table SELECT; filtering/grouping/pagination happens in Go,
-- matching how RebalancingTransferService/PurchaseSessionService/PurchasePlanningService/
-- MacroDeriskService already load-all-then-filter in Java rather than issuing narrow WHERE
-- queries. This is a deliberate simplification of SQL surface area, not a behavior change.
--
-- Two genuinely different write paths on the SAME table, kept separate per architect-output §2:
-- UpdatePositionDelta (delta-apply, called via position.ApplyDelta) vs. UpdatePositionFields
-- (PositionsController's own direct field edits) vs. ClearAssetPrimary/SetSubassetPrimary
-- (assets.sql — the set-primary flip, task 156's replacement for promotePosition's
-- group-leadership swap). Never merge these.
--
-- Task 156 (Asset / Subasset model): positions rows are subassets grouped by asset_id
-- (assets.sql owns the group root). is_companion_row / companion_parent_ticker /
-- target_allocation_pct(RiskOff) are gone (dropped V75 — targets live only on assets);
-- leadership is positions.is_primary (partial UNIQUE per asset) and group reads join
-- assets in Go.

-- Task 173 (ADR-109) soft delete: the existing read names are LIVE-ONLY (deleted_at IS NULL);
-- history readers opt in to …IncludingDeleted, the restore path to …AnyState.

-- name: ListAllPositions :many
SELECT * FROM positions WHERE deleted_at IS NULL ORDER BY ticker;

-- name: ListAllPositionsIncludingDeleted :many
-- History read (purchase-session detail): past sessions keep naming deleted assets. Live rows
-- sort before deleted ones of the same ticker, so first-per-ticker lookups prefer the live row.
SELECT * FROM positions ORDER BY ticker, (deleted_at IS NOT NULL), id;

-- name: GetPositionByID :one
SELECT * FROM positions WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPositionByNaturalKeyAnyState :one
-- Restore-on-create only: (ticker, exchange, portfolio) is main's natural key, unique incl. deleted rows.
SELECT * FROM positions WHERE ticker = $1 AND exchange = $2 AND portfolio = $3;

-- name: InsertPosition :one
INSERT INTO positions (
    ticker, name, portfolio, exchange, asset_class, exposure, price_currency, purchase_currency,
    units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
    asset_id, is_primary, yahoo_ticker, company_backed
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12, $13,
    $14, $15, $16, $17
) RETURNING *;

-- name: UpdatePositionFields :one
-- The direct-edit write path (PositionsController's PUT) — full-row overwrite of every
-- user-editable field, computed by the service from a read-modify-write over the existing row
-- (Go has no JPA dirty-checking; the service applies UpdatePositionRequest's non-null fields
-- onto the fetched row, then calls this). Targets are NOT editable here — they live on the
-- parent asset (PUT /assets/{id}); the handler rejects them with a 400 before this is built.
-- asset_id is the §13 re-home of a NON-primary subasset (PUT /positions/{id} with assetId):
-- full-row overwrite, so the service ALWAYS passes the current asset_id when the field is
-- omitted (forgetting it writes 0 and fails on the FK).
-- NEVER shares a code path with UpdatePositionDelta.
UPDATE positions SET
    units_ike = $2,
    units_ikze = $3,
    units_regular_eur = $4,
    units_regular_usd = $5,
    units_regular_pln = $6,
    total_spent_pln = $7,
    avg_buy_price = $8,
    name = $9,
    asset_class = $10,
    yahoo_ticker = $11,
    company_backed = $12,
    asset_id = $13
WHERE id = $1
RETURNING *;

-- name: UpdatePositionDelta :one
-- The ONE canonical delta-write query (architect-output §2) — sets the five unit buckets plus
-- total_spent_pln, called after position.ApplyDelta/UpdateTotalSpentPln mutate the in-memory
-- PositionUnits value. Every caller (internal/position's own completeSession-adjacent paths,
-- internal/rebalancing.ExecuteTransfer, internal/purchasesession/execution.go) declares this
-- exact method name in its own narrow Queries interface.
UPDATE positions SET
    units_ike = $2,
    units_ikze = $3,
    units_regular_eur = $4,
    units_regular_usd = $5,
    units_regular_pln = $6,
    total_spent_pln = $7
WHERE id = $1
RETURNING *;

-- name: UpdatePositionCost :exec
-- Task 161a-2: writes ONLY total_spent_pln of a sibling subasset row whose recorded cost changed
-- because a trade on another row of the SAME asset re-scaled the asset's cost (asset-level average
-- cost, costbasis kernel). The trade's own row keeps going through UpdatePositionDelta (units +
-- cost in one statement); both run inside the trade's transaction so cost and units commit or
-- roll back together. Never used by the manual PUT edit path (units edits never change cost).
UPDATE positions SET total_spent_pln = $2 WHERE id = $1;

-- name: SoftDeletePosition :exec
-- DELETE /positions/{id} of a non-primary subasset with live siblings (task 173).
UPDATE positions SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL;

-- name: RestorePosition :one
-- Restore-on-create (task 173 §6.1). Keeps id, ticker, natural key, units, total_spent_pln,
-- avg_buy_price, created_at. Required request fields overwrite; optional ones keep the old
-- value when the request omits them (the service passes the raw request pointers).
UPDATE positions SET deleted_at = NULL,
    asset_id = sqlc.arg(asset_id), is_primary = sqlc.arg(is_primary),
    name = sqlc.arg(name), asset_class = sqlc.arg(asset_class), price_currency = sqlc.arg(price_currency),
    exposure          = COALESCE(sqlc.narg(exposure), exposure),
    yahoo_ticker      = COALESCE(sqlc.narg(yahoo_ticker), yahoo_ticker),
    purchase_currency = COALESCE(sqlc.narg(purchase_currency), purchase_currency),
    company_backed    = COALESCE(sqlc.narg(company_backed), company_backed)
WHERE id = sqlc.arg(id) AND deleted_at IS NOT NULL
RETURNING *;

-- name: InsertAssetPurchase :one
-- Paired write: every position-delta write path (internal/rebalancing, internal/purchasesession/
-- execution.go) also writes one audit row here in the same transaction. session_id is NULL for
-- ad-hoc rebalancing-transfer legs (mirrors AssetPurchase.builder().session(null)) and set for
-- purchase-session-completion rows. xtb_import_key stays NULL on this path (only the V80
-- XTB-certain backfill carries keys); the column is wired so sqlc tracks the V79 schema.
INSERT INTO asset_purchases (
    session_id, portfolio, ticker, account_type, units_delta, price_pln, total_pln,
    purchased_at, trade_type, xtb_import_key
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;
