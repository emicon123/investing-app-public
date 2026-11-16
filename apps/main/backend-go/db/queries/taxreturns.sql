-- Phase 2 tax-returns queries — owns `tax_returns` exclusively (doc 13 §6.3: written only
-- by TaxReturnService.createTaxReturn; no other domain writes this table).

-- name: ListTaxReturnsPage :many
SELECT id, year, gross_gain, tax_paid, effective_rate_pct, created_at, updated_at
FROM tax_returns
ORDER BY year DESC
LIMIT $1 OFFSET $2;

-- name: CountTaxReturns :one
SELECT COUNT(*)::bigint AS total
FROM tax_returns;

-- name: GetTaxReturnByYear :one
SELECT id, year, gross_gain, tax_paid, effective_rate_pct, created_at, updated_at
FROM tax_returns
WHERE year = $1;

-- name: CreateTaxReturn :one
INSERT INTO tax_returns (year, gross_gain, tax_paid, effective_rate_pct)
VALUES ($1, $2, $3, $4)
RETURNING id, year, gross_gain, tax_paid, effective_rate_pct, created_at, updated_at;
