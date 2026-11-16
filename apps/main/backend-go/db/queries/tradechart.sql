-- Trade-chart queries — READ-ONLY (doc 13 §6.3, ADR-057). This package never
-- writes: markers come from existing `asset_purchases` rows (V9/V15/V45 `trade_type`),
-- display metadata from `positions` (V1). Both tables are written by other packages (position,
-- purchasesession); these three SELECTs only read them.
--
-- AggregateTradesByTicker mirrors AssetPurchaseRepository.aggregateByTicker: one row per
-- DISTINCT ticker, trade_count excluding SEED_IMPORT (but a ticker with only seed rows still
-- gets a row with count 0), MIN/MAX purchased_at over ALL rows including seed ones.
-- ListPositionsForChart mirrors TradeChartService.resolvePositionsByTicker's lowest-id-wins
-- via SELECT DISTINCT ON (ticker) ORDER BY ticker, id.

-- name: AggregateTradesByTicker :many
SELECT ticker,
    COUNT(*) FILTER (WHERE trade_type <> 'SEED_IMPORT'::trade_type_enum) AS trade_count,
    MIN(purchased_at)::timestamptz AS first_trade_at,
    MAX(purchased_at)::timestamptz AS last_trade_at
FROM asset_purchases
GROUP BY ticker
ORDER BY ticker ASC;

-- name: ListPositionsForChart :many
SELECT DISTINCT ON (ticker) id, ticker, name, yahoo_ticker, price_currency
FROM positions
ORDER BY ticker ASC, id ASC;

-- name: ListTradesByTicker :many
SELECT id, portfolio, ticker, account_type, units_delta, price_pln, total_pln,
    purchased_at, trade_type, session_id
FROM asset_purchases
WHERE ticker = $1
ORDER BY purchased_at ASC;
