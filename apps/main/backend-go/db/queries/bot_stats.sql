-- Phase 5 bot-stats queries — BotStatsController + BotStatsService port.
--
-- Read-only: stats/equity/open-positions aggregate over bot_trade with GROUP BY, never
-- a find-all-then-compute-in-memory (Architect binding). No writes in this file.

-- name: BotTradeStats :one
SELECT COUNT(*)::bigint AS total,
    COUNT(*) FILTER (WHERE status = 'OPEN')::bigint AS open_trades,
    COUNT(*) FILTER (WHERE status = 'CLOSED')::bigint AS closed_trades,
    COUNT(*) FILTER (WHERE status = 'CLOSED' AND pnl_pln > 0)::bigint AS wins,
    COALESCE(SUM(pnl_pln) FILTER (WHERE status = 'CLOSED'), 0)::numeric(18,4) AS total_pnl,
    AVG(EXTRACT(EPOCH FROM (closed_at - opened_at)) / 60)
        FILTER (WHERE status = 'CLOSED' AND opened_at IS NOT NULL AND closed_at IS NOT NULL)
        ::numeric AS avg_hold_minutes
FROM bot_trade;

-- name: BestBotTrade :one
SELECT id, signal_id, calendar_event_id, side, status, quantity, leverage,
    entry_price, exit_price, pnl_pln, pnl_pct, okx_order_id, notes, opened_at,
    closed_at, created_at, updated_at, okx_algo_id, margin_allocated_pln,
    sizing_confidence, scalp_session_status, scalp_session_claimed_at
FROM bot_trade
WHERE status = 'CLOSED' AND pnl_pln IS NOT NULL
ORDER BY pnl_pln DESC
LIMIT 1;

-- name: WorstBotTrade :one
SELECT id, signal_id, calendar_event_id, side, status, quantity, leverage,
    entry_price, exit_price, pnl_pln, pnl_pct, okx_order_id, notes, opened_at,
    closed_at, created_at, updated_at, okx_algo_id, margin_allocated_pln,
    sizing_confidence, scalp_session_status, scalp_session_claimed_at
FROM bot_trade
WHERE status = 'CLOSED' AND pnl_pln IS NOT NULL
ORDER BY pnl_pln ASC
LIMIT 1;

-- name: BotEquityByDay :many
SELECT ((closed_at AT TIME ZONE 'Europe/Warsaw')::date) AS day,
    SUM(pnl_pln)::numeric(18,4) AS daily_pnl
FROM bot_trade
WHERE status = 'CLOSED' AND closed_at IS NOT NULL
GROUP BY ((closed_at AT TIME ZONE 'Europe/Warsaw')::date)
ORDER BY day ASC;

-- name: OpenBotTrades :many
SELECT id, signal_id, calendar_event_id, side, status, quantity, leverage,
    entry_price, exit_price, pnl_pln, pnl_pct, okx_order_id, notes, opened_at,
    closed_at, created_at, updated_at, okx_algo_id, margin_allocated_pln,
    sizing_confidence, scalp_session_status, scalp_session_claimed_at
FROM bot_trade
WHERE status = 'OPEN'
ORDER BY opened_at DESC
LIMIT $1 OFFSET $2;
