-- Purchase-session queries — own `purchase_sessions`/`purchase_session_items`
-- exclusively (architect-output §2: neither repository was referenced from anywhere else in the
-- Java service tree) plus one INSERT into `binance_transfers` hardcoding source='SESSION' —
-- the purchase-completion write path (never touch or reuse InsertBinanceTransferManual from
-- internal/binancetransfers; guard_test.go in internal/purchasesession statically enforces the
-- mirror rule).
--
-- GetSessionForUpdate's SQL literally contains FOR UPDATE — the single highest-risk line in the
-- whole phase (architect-output §4). It MUST be the first statement inside CompleteSession's
-- transaction; do not reorder.

-- name: ExistsPlannedSession :one
SELECT EXISTS(SELECT 1 FROM purchase_sessions WHERE status = 'PLANNED');

-- name: GetPlannedSession :one
SELECT * FROM purchase_sessions WHERE status = 'PLANNED' LIMIT 1;

-- name: GetSessionByID :one
SELECT * FROM purchase_sessions WHERE id = $1;

-- name: GetSessionForUpdate :one
-- Pessimistic lock — port of PurchaseSessionRepository.findWithLockById
-- (@Lock(PESSIMISTIC_WRITE)). A concurrent double-submit of CompleteSession blocks here until
-- the first transaction commits, then hits the DONE guard instead of applying position deltas
-- twice.
SELECT * FROM purchase_sessions WHERE id = $1 FOR UPDATE;

-- name: InsertPurchaseSession :one
INSERT INTO purchase_sessions (
    status, planned_at, notes, retirement_budget_pln, main_budget_pln, portfolios_included
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: UpdateSessionStatusDone :one
UPDATE purchase_sessions SET status = 'DONE', done_at = $2 WHERE id = $1 RETURNING *;

-- name: DeleteSessionByID :exec
DELETE FROM purchase_sessions WHERE id = $1;

-- name: ListSessionsByStatusPaged :many
SELECT * FROM purchase_sessions
WHERE status = $1
ORDER BY planned_at DESC
LIMIT $2 OFFSET $3;

-- name: CountSessionsByStatus :one
SELECT COUNT(*) FROM purchase_sessions WHERE status = $1;

-- name: InsertPurchaseSessionItem :one
INSERT INTO purchase_session_items (
    session_id, portfolio, ticker, account_type, planned_units, planned_value_pln,
    actual_units, actual_value_pln, price_pln_at_plan, is_migration_sell, is_migration_buy
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: ListSessionItemsBySessionID :many
SELECT * FROM purchase_session_items WHERE session_id = $1 ORDER BY id;

-- name: ListSessionItemTotals :many
-- ListSessions' per-session item count and actual-value total for one page of sessions in a
-- single read (task 185 / F3.5, replaces one ListSessionItemsBySessionID per session). A session
-- with no items has no row here.
SELECT session_id,
    COUNT(*)::int AS item_count,
    COALESCE(SUM(actual_value_pln), 0)::numeric AS total_actual_pln,
    BOOL_OR(actual_value_pln IS NOT NULL) AS has_actual
FROM purchase_session_items
WHERE session_id = ANY(@session_ids::bigint[])
GROUP BY session_id;

-- name: ListSessionItemsOrderedForCompletion :many
-- Port of findBySessionIdOrderByIsMigrationSellDescPortfolioAscPlannedValuePlnDesc — sell items
-- (migration donors) process before buys, so a donor's balance is freed before any buy that
-- might depend on it within the same completion pass.
SELECT * FROM purchase_session_items
WHERE session_id = $1
ORDER BY is_migration_sell DESC, portfolio ASC, planned_value_pln DESC;

-- name: UpdateSessionItemActuals :one
UPDATE purchase_session_items SET actual_units = $2, actual_value_pln = $3 WHERE id = $1 RETURNING *;

-- name: DeleteSessionItemsBySessionID :exec
DELETE FROM purchase_session_items WHERE session_id = $1;

-- name: InsertBinanceTransferSession :one
-- The SESSION-sourced counterpart of internal/binancetransfers' InsertBinanceTransferManual —
-- defined here, in this package's own query file, per architect-output §2's single-writer
-- guard pattern (never edit or reuse InsertBinanceTransferManual from this package).
INSERT INTO binance_transfers (transfer_date, amount_pln, source, session_id, comment)
VALUES ($1, $2, 'SESSION', $3, $4)
RETURNING id, transfer_date, amount_pln, amount_native, native_currency, comment, source,
    session_id, created_at;
