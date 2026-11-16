-- Phase 8 position-transfer queries — owns `position_transfers` exclusively. The manual
-- "Sprzedaj" sell-assist audit trail: one row per leg of a transfer (one sold leg, one-or-more
-- buy legs), written in the same transaction as the UpdatePositionDelta calls in position.sql.

-- name: InsertPositionTransfer :one
INSERT INTO position_transfers (transfer_id, ticker, account_type, units_delta, price_pln, total_pln, transferred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;
