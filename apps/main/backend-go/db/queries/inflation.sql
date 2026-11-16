-- Phase 2 inflation queries — owns `inflation` exclusively (doc 13 §6.3: written only by
-- InflationEntryService.createInflationEntry + GusInflationSyncService.sync, both ported in this
-- phase; InflationIndexService/HistoriaService read it read-only — safe regardless of owner).
--
-- Write semantics (parity with GusInflationSyncService.sync, NOT an improvement): the sync path
-- overwrites cpi_value only when it actually changed and counts ONLY real changes
-- (new row or different cpi_value) — untouched rows are never rewritten.

-- name: ListInflationPage :many
SELECT id, year, month, cpi_value, created_at
FROM inflation
ORDER BY year DESC, month DESC
LIMIT $1 OFFSET $2;

-- name: CountInflation :one
SELECT COUNT(*)::bigint AS total
FROM inflation;

-- name: ListInflationPageByYear :many
SELECT id, year, month, cpi_value, created_at
FROM inflation
WHERE year = $3
ORDER BY year DESC, month DESC
LIMIT $1 OFFSET $2;

-- name: CountInflationByYear :one
SELECT COUNT(*)::bigint AS total
FROM inflation
WHERE year = $1;

-- name: GetInflationByYearMonth :one
SELECT id, year, month, cpi_value, created_at
FROM inflation
WHERE year = $1 AND month = $2;

-- name: CreateInflationEntry :one
INSERT INTO inflation (year, month, cpi_value)
VALUES ($1, $2, $3)
RETURNING id, year, month, cpi_value, created_at;

-- name: UpdateInflationCpi :one
UPDATE inflation
SET cpi_value = $3
WHERE year = $1 AND month = $2
RETURNING id, year, month, cpi_value, created_at;

-- name: ListInflationAsc :many
SELECT year, month
FROM inflation
ORDER BY year ASC, month ASC;
