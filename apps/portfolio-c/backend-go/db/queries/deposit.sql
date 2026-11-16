-- Phase 8 deposit queries — owns `deposits`, portfolio-scoped throughout (no account_type
-- column at all — that concept doesn't exist in this app; portfolio is the only partition
-- dimension). Single InsertDeposit, shared by DepositsController's own manual-create path and
-- PurchaseConfirmService's request-scoped deposit-amount recording (portfolioC has no "auto-read a
-- global setting" behavior — see docs/adr/001-portfolio-dimension.md §2).

-- name: ListDeposits :many
SELECT * FROM deposits
WHERE portfolio = sqlc.arg(portfolio)::portfolio_enum
  AND (sqlc.narg(year)::integer IS NULL OR EXTRACT(YEAR FROM deposit_date)::integer = sqlc.narg(year)::integer)
ORDER BY deposit_date DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountDeposits :one
SELECT COUNT(*) FROM deposits
WHERE portfolio = sqlc.arg(portfolio)::portfolio_enum
  AND (sqlc.narg(year)::integer IS NULL OR EXTRACT(YEAR FROM deposit_date)::integer = sqlc.narg(year)::integer);

-- name: ListDepositsByPortfolioOrderByDateAsc :many
-- XIRR's own cash-flow build — every deposit in one portfolio, oldest first (direct port of
-- DepositRepository.findAllByPortfolioOrderByDepositDateAsc).
SELECT * FROM deposits WHERE portfolio = $1 ORDER BY deposit_date ASC, id ASC;

-- name: InsertDeposit :one
INSERT INTO deposits (portfolio, deposit_date, amount, comment)
VALUES ($1, $2, $3, $4)
RETURNING *;
