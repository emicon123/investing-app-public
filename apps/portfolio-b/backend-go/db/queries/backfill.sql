-- Phase 6 KROK 2/2 backfill queries — the admin backfill's read set plus the single
-- position_values write. PortfolioB's variant: cutoff-filtered missing rows
-- (ACCURATE_CUTOFF 2026-07-14 — pre-cutoff NULL rows are intentionally skipped),
-- companions + transfers + CHF-aware pricing resolved in Go code, not SQL.

-- name: CountHistoriaAll :one
SELECT COUNT(*) FROM historia;

-- name: ListHistoriaMissingPositionValues :many
SELECT id, snapshot_date, total_deposits, portfolio_value, xirr, position_values, created_at
FROM historia
WHERE position_values IS NULL AND snapshot_date >= $1
ORDER BY snapshot_date ASC;

-- name: UpdateHistoriaPositionValues :exec
UPDATE historia
SET position_values = $2
WHERE id = $1;

-- name: ListBackfillPositions :many
SELECT ticker, price_currency, yahoo_ticker,
    units_ike_tata, units_ike_mama, units_ikze_tata, units_ikze_mama, units_inne,
    asset_id, is_primary
FROM positions
ORDER BY ticker, id;

-- name: ListBackfillLots :many
SELECT ticker, purchased_at, units
FROM purchase_lots
ORDER BY purchased_at DESC, ticker ASC;

-- name: ListBackfillTransfers :many
SELECT ticker, units_delta, transferred_at
FROM position_transfers;

-- name: ListHistoriaDateBounds :one
SELECT MIN(snapshot_date) AS min_date, MAX(snapshot_date) AS max_date
FROM historia;
