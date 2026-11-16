-- Phase 4 crypto-trades queries — owns `crypto_trades` exclusively (doc 13 §6.3: this
-- domain's controller/service is the only writer; V1/V18/V56 shape the table, no later
-- migration touches it).
--
-- List/count share one nullable-filter predicate, mirroring CryptoTradeService.buildSpec:
-- blank ticker = no filter (isBlank check), year via EXTRACT(YEAR FROM trade_date).
-- Ordering: Java's findAll(spec, pageable) carries no explicit sort (heap order on an
-- append-only table, no delete/update endpoint exists), so ORDER BY id ASC is the
-- deterministic equivalent. Insert hardcodes source='MANUAL' in SQL (mirroring
-- CryptoTradeMapper's constant="MANUAL") so the forcing holds even if a caller forgets it.

-- name: ListCryptoTrades :many
SELECT id, trade_date, ticker, exchange, account_type, side, quantity, unit_price,
    price_currency, total_amount, amount_pln, source, notes, created_at
FROM crypto_trades
WHERE (sqlc.narg(ticker)::text IS NULL OR ticker = sqlc.narg(ticker)::text)
  AND (sqlc.narg(year)::integer IS NULL OR EXTRACT(YEAR FROM trade_date)::integer = sqlc.narg(year)::integer)
  AND (sqlc.narg(exchange)::exchange_enum IS NULL OR exchange = sqlc.narg(exchange)::exchange_enum)
  AND (sqlc.narg(side)::trade_side_enum IS NULL OR side = sqlc.narg(side)::trade_side_enum)
ORDER BY id ASC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountCryptoTrades :one
SELECT COUNT(*)
FROM crypto_trades
WHERE (sqlc.narg(ticker)::text IS NULL OR ticker = sqlc.narg(ticker)::text)
  AND (sqlc.narg(year)::integer IS NULL OR EXTRACT(YEAR FROM trade_date)::integer = sqlc.narg(year)::integer)
  AND (sqlc.narg(exchange)::exchange_enum IS NULL OR exchange = sqlc.narg(exchange)::exchange_enum)
  AND (sqlc.narg(side)::trade_side_enum IS NULL OR side = sqlc.narg(side)::trade_side_enum);

-- name: FindCryptoTradesByYear :many
SELECT id, trade_date, ticker, exchange, account_type, side, quantity, unit_price,
    price_currency, total_amount, amount_pln, source, notes, created_at
FROM crypto_trades
WHERE EXTRACT(YEAR FROM trade_date)::integer = sqlc.arg(year)::integer
ORDER BY trade_date DESC;

-- name: InsertCryptoTrade :one
INSERT INTO crypto_trades (trade_date, ticker, exchange, account_type, side, quantity,
    unit_price, price_currency, total_amount, amount_pln, source, notes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'MANUAL', $11)
RETURNING id, trade_date, ticker, exchange, account_type, side, quantity, unit_price,
    price_currency, total_amount, amount_pln, source, notes, created_at;
