-- Market-data queries — own price_cache and fx_rate_cache exclusively (doc 13 §6.3).
-- The GetTrackedTickers query reads `positions` (written by the position package) read-only, to
-- discover which tickers to refresh — reading another package's table is fine, writing is not
-- (see AGENTS.md).

-- name: GetAllPriceCache :many
SELECT ticker, price, currency, price_pln, fetched_at
FROM price_cache
ORDER BY ticker;

-- name: GetPriceCacheByTicker :one
SELECT ticker, price, currency, price_pln, fetched_at
FROM price_cache
WHERE ticker = $1;

-- name: UpsertPriceCache :exec
INSERT INTO price_cache (ticker, price, currency, price_pln, fetched_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (ticker) DO UPDATE SET
    price = EXCLUDED.price,
    currency = EXCLUDED.currency,
    price_pln = EXCLUDED.price_pln,
    fetched_at = EXCLUDED.fetched_at;

-- name: GetAllFxRateCache :many
SELECT from_currency, rate_to_pln, source, fetched_at
FROM fx_rate_cache
ORDER BY from_currency;

-- name: GetFxRateCacheByCurrency :one
SELECT from_currency, rate_to_pln, source, fetched_at
FROM fx_rate_cache
WHERE from_currency = $1;

-- name: UpsertFxRateCache :exec
INSERT INTO fx_rate_cache (from_currency, rate_to_pln, source, fetched_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (from_currency) DO UPDATE SET
    rate_to_pln = EXCLUDED.rate_to_pln,
    source = EXCLUDED.source,
    fetched_at = EXCLUDED.fetched_at;

-- name: GetTrackedTickers :many
-- Read-only lookup of every distinct yahoo_ticker known to `positions`, plus the price currency
-- and exchange needed to route/convert it — the direct equivalent of PriceCacheService.
-- forceRefreshAll's tickerInfoMap built from positionRepository.findAll(). DISTINCT ON picks the
-- lowest-id row per ticker, a deterministic tie-break for the (extremely unlikely) case two
-- positions share a yahoo_ticker with different currency/exchange — Java's HashMap.putIfAbsent
-- over findAll() iteration order has no stronger guarantee than this in practice.
SELECT DISTINCT ON (yahoo_ticker) yahoo_ticker, price_currency, exchange
FROM positions
WHERE yahoo_ticker IS NOT NULL AND yahoo_ticker <> ''
  AND deleted_at IS NULL
ORDER BY yahoo_ticker, id;
