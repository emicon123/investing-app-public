-- Phase 7 deposit queries — owns `deposits` (single InsertDeposit query, shared by
-- internal/deposit's own manual-create path, internal/rebalancing's auto-deposit, and
-- internal/purchasesession's auto-deposit — architect-output §2: "no arithmetic to share, no new
-- Go abstraction needed beyond the one generated method") — plus `account_cap_adjustment`
-- exclusively (ADR-058, only ever read/written by AccountCapUsageService).
--
-- SumDepositAmountByAccountTypeAndYear deliberately ignores portfolio/capital_source (mirrors
-- DepositRepository.sumAmountByAccountTypeAndYear's own javadoc: internal transfers still count
-- toward the annual statutory cap even though XIRR/Historia exclude them).

-- name: ListDeposits :many
SELECT * FROM deposits
WHERE (sqlc.narg(portfolio)::portfolio_enum IS NULL OR portfolio = sqlc.narg(portfolio)::portfolio_enum)
  AND (sqlc.narg(account_type)::account_type_enum IS NULL OR account_type = sqlc.narg(account_type)::account_type_enum)
  AND (sqlc.narg(exchange)::exchange_enum IS NULL OR exchange = sqlc.narg(exchange)::exchange_enum)
  AND (sqlc.narg(year)::integer IS NULL OR EXTRACT(YEAR FROM deposit_date)::integer = sqlc.narg(year)::integer)
ORDER BY deposit_date DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountDeposits :one
SELECT COUNT(*) FROM deposits
WHERE (sqlc.narg(portfolio)::portfolio_enum IS NULL OR portfolio = sqlc.narg(portfolio)::portfolio_enum)
  AND (sqlc.narg(account_type)::account_type_enum IS NULL OR account_type = sqlc.narg(account_type)::account_type_enum)
  AND (sqlc.narg(exchange)::exchange_enum IS NULL OR exchange = sqlc.narg(exchange)::exchange_enum)
  AND (sqlc.narg(year)::integer IS NULL OR EXTRACT(YEAR FROM deposit_date)::integer = sqlc.narg(year)::integer);

-- name: InsertDeposit :one
INSERT INTO deposits (deposit_date, amount, currency, exchange, account_type, portfolio, comment, capital_source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: SumDepositAmountByAccountTypeAndYear :one
SELECT COALESCE(SUM(amount), 0)::numeric AS total
FROM deposits
WHERE account_type = sqlc.arg(account_type)::account_type_enum
  AND EXTRACT(YEAR FROM deposit_date)::integer = sqlc.arg(year)::integer;

-- name: GetAccountCapAdjustment :one
SELECT * FROM account_cap_adjustment WHERE account_type = $1 AND year = $2;

-- name: UpsertAccountCapAdjustment :one
INSERT INTO account_cap_adjustment (account_type, year, adjustment_pln)
VALUES ($1, $2, $3)
ON CONFLICT (account_type, year) DO UPDATE SET adjustment_pln = EXCLUDED.adjustment_pln
RETURNING *;
