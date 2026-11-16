-- Cash-positions queries — own `cash_positions` exclusively (doc 13 §6.3: written only by the
-- cash-positions create-snapshot service; the rebalancing value computation reads the latest
-- snapshot read-only).
--
-- Snapshots are append-only batches sharing one recorded_at: CreateCashPosition inserts a single
-- row; the service inserts the whole batch in ONE transaction (port of saveAll's atomicity).

-- name: LatestCashRecordedAt :one
SELECT recorded_at
FROM cash_positions
ORDER BY recorded_at DESC
LIMIT 1;

-- name: CashPositionsByRecordedAt :many
SELECT id, location, amount_pln, recorded_at
FROM cash_positions
WHERE recorded_at = $1
ORDER BY id;

-- name: CreateCashPosition :one
INSERT INTO cash_positions (location, amount_pln, recorded_at)
VALUES ($1, $2, $3)
RETURNING id, location, amount_pln, recorded_at;
