-- Phase 8 deposit queries — owns `deposits` (single InsertDeposit, shared by
-- DepositsController's own manual-create path and PurchaseConfirmService's auto-deposit-per-
-- confirm, exactly like DepositService.createDeposit is reused for both in Java). This app's
-- `deposits` table has no portfolio/account_type/exchange/currency columns (V11 dropped
-- account_type entirely, and V1 never had the others) — a year filter is the only query
-- dimension DepositsController exposes.

-- name: ListDeposits :many
SELECT * FROM deposits
WHERE (sqlc.narg(year)::integer IS NULL OR EXTRACT(YEAR FROM deposit_date)::integer = sqlc.narg(year)::integer)
ORDER BY deposit_date DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountDeposits :one
SELECT COUNT(*) FROM deposits
WHERE (sqlc.narg(year)::integer IS NULL OR EXTRACT(YEAR FROM deposit_date)::integer = sqlc.narg(year)::integer);

-- name: ListAllDepositsOrderByDateAsc :many
-- XIRR's own cash-flow build — every deposit, oldest first (direct port of
-- DepositRepository.findAllByOrderByDepositDateAsc).
SELECT * FROM deposits ORDER BY deposit_date ASC, id ASC;

-- name: InsertDeposit :one
INSERT INTO deposits (deposit_date, amount, comment)
VALUES ($1, $2, $3)
RETURNING *;
