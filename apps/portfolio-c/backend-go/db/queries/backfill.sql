-- Phase 6 KROK 2/2 backfill queries — the admin backfill's read set plus the single
-- position_values write, scoped per portfolio. PortfolioC's variant is flat: no cutoff
-- (every NULL row is filled), purchase lots and position transfers replayed through
-- siblingcore/backfill; no companions folding, no CHF special — one loop per portfolio over that
-- portfolio's own positions, lots and transfers.

-- name: CountHistoriaByPortfolio :one
SELECT COUNT(*) FROM historia WHERE portfolio = $1;

-- name: ListHistoriaMissingPositionValues :many
SELECT id, portfolio, snapshot_date, total_deposits, portfolio_value, xirr, position_values, created_at
FROM historia
WHERE portfolio = $1 AND position_values IS NULL
ORDER BY snapshot_date ASC;

-- name: UpdateHistoriaPositionValues :exec
UPDATE historia
SET position_values = $2
WHERE id = $1;

-- name: ListBackfillPositions :many
SELECT ticker, price_currency, yahoo_ticker, units,
    asset_id, is_primary
FROM positions
WHERE portfolio = $1
ORDER BY ticker, id;

-- name: ListBackfillLots :many
SELECT ticker, purchased_at, units
FROM purchase_lots
WHERE portfolio = $1
ORDER BY purchased_at DESC, ticker ASC;

-- name: ListBackfillTransfers :many
SELECT ticker, units_delta, transferred_at
FROM position_transfers
WHERE portfolio = $1;

-- name: ListHistoriaDateBounds :one
SELECT MIN(snapshot_date) AS min_date, MAX(snapshot_date) AS max_date
FROM historia
WHERE portfolio = $1;
