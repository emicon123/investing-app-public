-- Phase 6 KROK 2/2 historia queries — owns `historia` for the read path plus the
-- snapshot's read-only dependencies (positions, deposits, inflation, price_cache,
-- fx_rate_cache). No portfolio dimension in this app (single portfolio).
--
-- Read-only boundary (task 144 lesson): price_cache/fx_rate_cache are READS only —
-- this domain never writes them. The snapshot upsert is the only write here, and
-- its scheduler is gated by HISTORIA_SCHEDULER_ENABLED (default false; true in compose,
-- ADR-097).

-- name: ListHistoriaRange :many
SELECT id, snapshot_date, total_deposits, portfolio_value, xirr, position_values, created_at
FROM historia
WHERE snapshot_date BETWEEN $1 AND $2
ORDER BY snapshot_date ASC;

-- name: ListHistoriaAll :many
SELECT id, snapshot_date, total_deposits, portfolio_value, xirr, position_values, created_at
FROM historia
ORDER BY snapshot_date ASC;

-- name: UpsertHistoria :exec
INSERT INTO historia (snapshot_date, total_deposits, portfolio_value, xirr, position_values)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (snapshot_date) DO UPDATE SET
    total_deposits = EXCLUDED.total_deposits,
    portfolio_value = EXCLUDED.portfolio_value,
    xirr = EXCLUDED.xirr,
    position_values = EXCLUDED.position_values;

-- name: ListHistoriaDeposits :many
SELECT deposit_date, amount
FROM deposits
ORDER BY deposit_date ASC;
