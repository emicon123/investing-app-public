-- Binance-transfers queries (doc 13 §6.3).
--
-- This package serves the read/list/manual-entry/delete endpoints only. The
-- purchase-completion-triggered insert into `binance_transfers` (source forced by the
-- completion path, session-linked) belongs to the purchasesession package — hence
-- InsertBinanceTransferManual hardcodes source='MANUAL' AND
-- session_id=NULL in SQL, and NO other INSERT into this table exists anywhere in Go
-- (enforced by TestNoCompletionSourcedWriteInPackage, which fails on the completion-path
-- source literal anywhere under this package, comments and tests included).

-- name: ListBinanceTransfers :many
SELECT id, transfer_date, amount_pln, amount_native, native_currency, comment, source,
    session_id, created_at
FROM binance_transfers
ORDER BY transfer_date DESC, id DESC;

-- name: ListBinanceTransfersByYear :many
SELECT id, transfer_date, amount_pln, amount_native, native_currency, comment, source,
    session_id, created_at
FROM binance_transfers
WHERE EXTRACT(YEAR FROM transfer_date)::integer = sqlc.arg(year)::integer
ORDER BY transfer_date DESC, id DESC;

-- name: SumAmountPlnByYear :many
SELECT EXTRACT(YEAR FROM transfer_date)::integer AS year, SUM(amount_pln)::numeric AS total_pln
FROM binance_transfers
GROUP BY EXTRACT(YEAR FROM transfer_date)
ORDER BY EXTRACT(YEAR FROM transfer_date) DESC;

-- name: InsertBinanceTransferManual :one
INSERT INTO binance_transfers (transfer_date, amount_pln, amount_native, native_currency,
    comment, source, session_id)
VALUES ($1, $2, $3, $4, $5, 'MANUAL', NULL)
RETURNING id, transfer_date, amount_pln, amount_native, native_currency, comment, source,
    session_id, created_at;

-- name: ExistsBinanceTransferById :one
SELECT EXISTS(SELECT 1 FROM binance_transfers WHERE id = $1);

-- name: DeleteBinanceTransfer :exec
DELETE FROM binance_transfers WHERE id = $1;
