-- Historia queries — own `historia` exclusively (doc 13 §6.3: the scheduler is the only
-- writer; the GET path never writes).
--
-- Read-only dependencies (doc 13 §6.3): `positions` (value computation reads only),
-- `deposits` (EXTERNAL only per ADR-059), `inflation` (the inflation-index read),
-- `price_cache` (single-ticker PLN read — never a live fetch here).
--
-- Write semantics: UpsertHistoria is a single ON CONFLICT(snapshot_date, portfolio)
-- statement updating value/deposits/xirr/position_values only — created_at is never
-- touched (matches HistoriaScheduler's find-then-update-or-insert, minus the race).

-- name: ListHistoriaByPortfolio :many
SELECT id, snapshot_date, portfolio, total_deposits, portfolio_value, xirr, position_values, created_at
FROM historia
WHERE portfolio = $1 AND snapshot_date BETWEEN $2 AND $3
ORDER BY snapshot_date ASC;

-- name: ListHistoriaAllRange :many
SELECT id, snapshot_date, portfolio, total_deposits, portfolio_value, xirr, position_values, created_at
FROM historia
WHERE snapshot_date BETWEEN $1 AND $2
ORDER BY snapshot_date ASC;

-- name: ListHistoriaAll :many
SELECT id, snapshot_date, portfolio, total_deposits, portfolio_value, xirr, position_values, created_at
FROM historia
ORDER BY snapshot_date ASC;

-- name: GetHistoriaByDatePortfolio :one
SELECT id, snapshot_date, portfolio, total_deposits, portfolio_value, xirr, position_values, created_at
FROM historia
WHERE snapshot_date = $1 AND portfolio = $2;

-- name: UpsertHistoria :exec
INSERT INTO historia (snapshot_date, portfolio, total_deposits, portfolio_value, xirr, position_values)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (snapshot_date, portfolio) DO UPDATE SET
    total_deposits = EXCLUDED.total_deposits,
    portfolio_value = EXCLUDED.portfolio_value,
    xirr = EXCLUDED.xirr,
    position_values = EXCLUDED.position_values;

-- name: ListHistoriaPositions :many
SELECT ticker, yahoo_ticker, price_currency,
    units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
    asset_id, is_primary
FROM positions
WHERE portfolio = $1 AND deleted_at IS NULL;

-- name: ListHistoriaExternalDeposits :many
SELECT deposit_date, amount, currency
FROM deposits
WHERE portfolio = $1 AND capital_source = $2
ORDER BY deposit_date ASC;

-- name: ListHistoriaInflationAsc :many
SELECT year, month, cpi_value
FROM inflation
ORDER BY year ASC, month ASC;

-- name: GetPriceCachePlnByTicker :one
SELECT price_pln
FROM price_cache
WHERE ticker = $1;
