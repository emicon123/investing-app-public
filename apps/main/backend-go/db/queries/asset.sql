-- Asset queries — task 156 (Asset / Subasset model). Owns `assets` (the group root:
-- targets, exposure, asset_class, name) plus the two primary-flip writes on `positions`.
-- Group reads stay load-all-then-join in Go (the established single-user-app convention —
-- see position.sql's own doc comment); no ListPositionsByAsset query needed.

-- Task 173 (ADR-109) soft delete: the existing read names are LIVE-ONLY (deleted_at IS NULL) —
-- safe by default for every current-holdings reader. History readers opt in to
-- …IncludingDeleted; the restore-on-create path uses …AnyState.

-- name: ListAllAssets :many
SELECT * FROM assets WHERE deleted_at IS NULL ORDER BY id;

-- name: ListAllAssetsIncludingDeleted :many
-- History read (purchase-session detail): past sessions keep naming deleted assets.
SELECT * FROM assets ORDER BY id;

-- name: GetAssetByID :one
SELECT * FROM assets WHERE id = $1 AND deleted_at IS NULL;

-- name: GetAssetByIDAnyState :one
-- Restore-on-create only: a deleted subasset's own asset, live or deleted.
SELECT * FROM assets WHERE id = $1;

-- name: GetAssetByKeyAnyState :one
-- Restore-on-create only: (key, portfolio) is the asset natural key, unique incl. deleted rows.
SELECT * FROM assets WHERE key = $1 AND portfolio = $2;

-- name: InsertAsset :one
INSERT INTO assets (
    portfolio, key, name, exposure, asset_class,
    target_allocation_pct, target_allocation_pct_risk_off
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7
) RETURNING *;

-- name: UpdateAssetFields :one
-- PUT /assets/{id} — the only writer of asset-level targets (subasset rows never carry
-- targets; V74's positions_subasset_targets_null CHECK proved every subasset row already
-- had NULLs before V75 dropped the columns). Read-modify-write lives in the service
-- (Go has no JPA dirty-checking).
UPDATE assets SET
    target_allocation_pct = $2,
    target_allocation_pct_risk_off = $3,
    name = $4
WHERE id = $1
RETURNING *;

-- name: SoftDeleteAsset :exec
-- DELETE /assets/{id} and the last-live-subasset DELETE /positions/{id} (task 173): one
-- statement = atomic cascade; now() is transaction-constant, so the asset and its live
-- subassets share the timestamp. Subassets already soft-deleted keep their earlier deleted_at.
-- Rows are never hard-deleted, so history FKs/links stay valid.
WITH subs AS (
    UPDATE positions SET deleted_at = now()
    WHERE positions.asset_id = sqlc.arg(asset_id) AND positions.deleted_at IS NULL
)
UPDATE assets SET deleted_at = now()
WHERE assets.id = sqlc.arg(asset_id) AND assets.deleted_at IS NULL;

-- name: RestoreAsset :one
-- Restore-on-create (task 173 §6.1): required newAsset fields overwrite; targets take the
-- request value when given, else keep the old one.
UPDATE assets SET deleted_at = NULL,
    name = sqlc.arg(name), exposure = sqlc.arg(exposure), asset_class = sqlc.arg(asset_class),
    target_allocation_pct          = COALESCE(sqlc.narg(target_allocation_pct), target_allocation_pct),
    target_allocation_pct_risk_off = COALESCE(sqlc.narg(target_allocation_pct_risk_off), target_allocation_pct_risk_off)
WHERE id = sqlc.arg(id) AND deleted_at IS NOT NULL
RETURNING *;

-- name: ClearAssetPrimary :exec
-- Deliberately NOT filtered by deleted_at: the restore path relies on it to clear stale
-- primary flags left on deleted rows (task 173 invariant I3).
-- Set-primary flip, first half: every subasset of the group becomes non-primary. Paired
-- with SetSubassetPrimary in one transaction (service-level TransactFunc) — the partial
-- UNIQUE(asset_id) WHERE is_primary only forbids two primaries, so clear-then-set order
-- never violates it mid-flight.
UPDATE positions SET is_primary = false WHERE asset_id = $1 AND is_primary;

-- name: SetSubassetPrimary :one
-- Set-primary flip, second half: exactly one row becomes primary. Pure boolean flip — no
-- cost basis, units, or targets move (they never leave their rows now). The AND asset_id
-- clause makes a subasset from another asset match zero rows (service maps that to 400).
UPDATE positions SET is_primary = true WHERE id = $1 AND asset_id = $2 AND deleted_at IS NULL RETURNING *;
