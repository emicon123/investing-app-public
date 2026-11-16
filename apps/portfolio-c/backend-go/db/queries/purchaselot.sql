-- Phase 8 purchase-lot queries — owns `purchase_lots` exclusively, portfolio-scoped. One lot
-- per (confirm session, ticker, portfolio) — portfolioC has no IKE/IKZE waterfall, so every unit
-- confirmed for a ticker in one call becomes one lot directly (no per-account aggregation step
-- like portfolioB's).

-- name: InsertPurchaseLot :one
INSERT INTO purchase_lots (portfolio, session_id, purchased_at, ticker, units, price_pln, cost_pln)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListPurchaseLotsByPortfolio :many
-- Direct port of PurchaseLotRepository.findAllByPortfolioOrderByPurchasedAtDescCreatedAtDescTickerAsc
-- — the final session/lot ordering is re-derived in Go (PurchaseHistoryEngine.buildHistory),
-- this pre-sort is just a stable starting point.
SELECT * FROM purchase_lots WHERE portfolio = $1 ORDER BY purchased_at DESC, created_at DESC, ticker ASC;
