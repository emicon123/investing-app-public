-- Phase 8 purchase-lot queries — owns `purchase_lots` exclusively (PurchaseConfirmService's
-- own write path; PurchaseHistoryService's read path). One lot per (confirm session, ticker),
-- aggregated IKE/IKZE units only (INNE excluded — read-only, never a lot).

-- name: InsertPurchaseLot :one
INSERT INTO purchase_lots (session_id, purchased_at, ticker, units, price_pln, cost_pln)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAllPurchaseLots :many
-- Direct port of PurchaseLotRepository.findAllByOrderByPurchasedAtDescCreatedAtDescTickerAsc —
-- the final session/lot ordering is re-derived in Go (PurchaseHistoryEngine.buildHistory), this
-- pre-sort is just a stable starting point.
SELECT * FROM purchase_lots ORDER BY purchased_at DESC, created_at DESC, ticker ASC;
