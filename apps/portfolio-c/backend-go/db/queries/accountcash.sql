-- Phase 6 KROK 2/2 account-cash queries — owns `account_cash_snapshot` (PK =
-- portfolio, single balance_pln column). One row per portfolio, read + update only.

-- name: GetAccountCash :one
SELECT portfolio, balance_pln, updated_at, update_reason
FROM account_cash_snapshot
WHERE portfolio = $1;

-- name: UpdateAccountCash :one
UPDATE account_cash_snapshot
SET balance_pln = $2,
    updated_at = $3,
    update_reason = $4
WHERE portfolio = $1
RETURNING portfolio, balance_pln, updated_at, update_reason;
