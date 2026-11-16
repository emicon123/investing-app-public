-- Phase 5 bot-trade queries — BotTradeController + BotTradeService +
-- BotScalpSessionService + BotTradeActionService + BotTradeTickService port.
--
-- Write ownership: bot_trade, bot_trade_action, bot_trade_tick are written ONLY here.
-- Atomicity rules (Architect binding): scalp claim/finish are a SINGLE
-- UPDATE...WHERE per statement (PENDING->RUNNING / RUNNING->DONE); the caller maps
-- rowcount 1 → 200 and 0 → 404/409 AFTER a follow-up read — NEVER select-then-update.
-- Claim/finish action rows are inserted in the same transaction as the claim/finish
-- update (service-level transact, mirroring Java's @Transactional).

-- name: ListTrades :many
SELECT id, signal_id, calendar_event_id, side, status, quantity, leverage,
    entry_price, exit_price, pnl_pln, pnl_pct, okx_order_id, notes, opened_at,
    closed_at, created_at, updated_at, okx_algo_id, margin_allocated_pln,
    sizing_confidence, scalp_session_status, scalp_session_claimed_at
FROM bot_trade
WHERE (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
  AND (sqlc.arg(side)::text = '' OR side = sqlc.arg(side)::text)
ORDER BY opened_at DESC
LIMIT $1 OFFSET $2;

-- name: CountTrades :one
SELECT COUNT(*)::bigint AS total
FROM bot_trade
WHERE (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
  AND (sqlc.arg(side)::text = '' OR side = sqlc.arg(side)::text);

-- name: GetTradeByID :one
SELECT id, signal_id, calendar_event_id, side, status, quantity, leverage,
    entry_price, exit_price, pnl_pln, pnl_pct, okx_order_id, notes, opened_at,
    closed_at, created_at, updated_at, okx_algo_id, margin_allocated_pln,
    sizing_confidence, scalp_session_status, scalp_session_claimed_at
FROM bot_trade
WHERE id = $1;

-- name: ExistsTradeByID :one
SELECT EXISTS(SELECT 1 FROM bot_trade WHERE id = $1);

-- name: ExistsOpenTrade :one
SELECT EXISTS(SELECT 1 FROM bot_trade WHERE status = 'OPEN');

-- name: ExistsTradeOpenedAfter :one
SELECT EXISTS(SELECT 1 FROM bot_trade WHERE opened_at > $1);

-- name: InsertTrade :one
INSERT INTO bot_trade (signal_id, calendar_event_id, side, status, quantity, leverage,
    entry_price, okx_order_id, notes, opened_at)
VALUES ($1, $2, $3, 'OPEN', $4, $5, $6, $7, $8, NOW())
RETURNING id, signal_id, calendar_event_id, side, status, quantity, leverage,
    entry_price, exit_price, pnl_pln, pnl_pct, okx_order_id, notes, opened_at,
    closed_at, created_at, updated_at, okx_algo_id, margin_allocated_pln,
    sizing_confidence, scalp_session_status, scalp_session_claimed_at;

-- name: CloseTrade :one
UPDATE bot_trade
SET exit_price = $2, pnl_pln = $3, pnl_pct = $4, status = 'CLOSED', closed_at = NOW()
WHERE id = $1 AND status = 'OPEN'
RETURNING id, signal_id, calendar_event_id, side, status, quantity, leverage,
    entry_price, exit_price, pnl_pln, pnl_pct, okx_order_id, notes, opened_at,
    closed_at, created_at, updated_at, okx_algo_id, margin_allocated_pln,
    sizing_confidence, scalp_session_status, scalp_session_claimed_at;

-- name: AttachExecutionAudit :exec
UPDATE bot_trade
SET okx_algo_id = $2, margin_allocated_pln = $3, sizing_confidence = $4
WHERE id = $1;

-- name: AttachPlacedAudit :exec
UPDATE bot_trade
SET okx_algo_id = $2, margin_allocated_pln = $3, sizing_confidence = $4,
    scalp_session_status = 'PENDING'
WHERE id = $1;

-- name: AppendTradeNote :exec
UPDATE bot_trade
SET notes = CASE
    WHEN notes IS NULL OR notes = '' THEN $2
    ELSE notes || chr(10) || $2
END
WHERE id = $1;

-- name: PendingScalpTrades :many
SELECT id, signal_id, calendar_event_id, side, status, quantity, leverage,
    entry_price, exit_price, pnl_pln, pnl_pct, okx_order_id, notes, opened_at,
    closed_at, created_at, updated_at, okx_algo_id, margin_allocated_pln,
    sizing_confidence, scalp_session_status, scalp_session_claimed_at
FROM bot_trade
WHERE scalp_session_status = 'PENDING'
ORDER BY opened_at ASC;

-- name: ClaimPendingScalp :execrows
UPDATE bot_trade
SET scalp_session_status = 'RUNNING', scalp_session_claimed_at = NOW()
WHERE id = $1 AND scalp_session_status = 'PENDING';

-- name: FinishRunningScalp :execrows
UPDATE bot_trade
SET scalp_session_status = 'DONE'
WHERE id = $1 AND scalp_session_status = 'RUNNING';

-- name: InsertTradeAction :one
INSERT INTO bot_trade_action (trade_id, action_type, price, quantity, reasoning, source)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, trade_id, action_type, price, quantity, reasoning, source, created_at;

-- name: ListTradeActions :many
SELECT id, trade_id, action_type, price, quantity, reasoning, source, created_at
FROM bot_trade_action
WHERE trade_id = $1
ORDER BY created_at ASC;

-- name: InsertTradeTick :one
INSERT INTO bot_trade_tick (trade_id, price, volume, cvd, atr_1m, ob_imbalance,
    funding_rate, long_short_ratio, open_interest)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, trade_id, price, volume, cvd, atr_1m, ob_imbalance, funding_rate,
    created_at, long_short_ratio, open_interest;

-- name: ListTradeTicks :many
SELECT id, trade_id, price, volume, cvd, atr_1m, ob_imbalance, funding_rate,
    created_at, long_short_ratio, open_interest
FROM bot_trade_tick
WHERE trade_id = $1
ORDER BY created_at ASC;

-- name: ListTradeTicksBetween :many
SELECT id, trade_id, price, volume, cvd, atr_1m, ob_imbalance, funding_rate,
    created_at, long_short_ratio, open_interest
FROM bot_trade_tick
WHERE trade_id = $1 AND created_at >= $2 AND created_at <= $3
ORDER BY created_at ASC;

-- name: ExistingSignalIDs :many
SELECT id
FROM bot_signal
WHERE id = ANY(sqlc.arg(ids)::bigint[]);
