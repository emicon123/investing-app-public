-- Phase 6 KROK 2/2 account-cash queries — owns `account_cash_snapshot` (singleton
-- row id=1, four IKE/IKZE columns). Single-row read + single-row update only.

-- name: GetAccountCash :one
SELECT id, ike_tata, ike_mama, ikze_tata, ikze_mama, updated_at, update_reason
FROM account_cash_snapshot
WHERE id = 1;

-- name: UpdateAccountCash :one
UPDATE account_cash_snapshot
SET ike_tata = $1,
    ike_mama = $2,
    ikze_tata = $3,
    ikze_mama = $4,
    updated_at = $5,
    update_reason = $6
WHERE id = 1
RETURNING id, ike_tata, ike_mama, ikze_tata, ikze_mama, updated_at, update_reason;
