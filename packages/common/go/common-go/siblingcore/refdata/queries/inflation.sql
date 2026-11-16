-- inflation: the inflationsync store and the historia read path's CPI index of portfolioB and portfolioC.

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

-- name: ListHistoriaInflationAsc :many
SELECT year, month, cpi_value
FROM inflation
ORDER BY year ASC, month ASC;
