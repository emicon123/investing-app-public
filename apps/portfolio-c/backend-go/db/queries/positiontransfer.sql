-- Phase 8 position-transfer queries — owns `position_transfers` exclusively, portfolio-scoped.
-- No account_type column at all (portfolioC has no sub-account concept) — a transfer lives entirely
-- within one portfolio.

-- name: InsertPositionTransfer :one
INSERT INTO position_transfers (transfer_id, portfolio, ticker, units_delta, price_pln, total_pln, transferred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;
