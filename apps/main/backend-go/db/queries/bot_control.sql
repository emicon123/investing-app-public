-- Phase 5 bot-control queries — BotControlController + BotControlService port.
--
-- Write ownership: bot_control (single row, id=1, V65 seed) is written ONLY here via
-- PATCH /bot/control (human kill switch). No other Go package writes this table.

-- name: GetBotControl :one
SELECT id, trading_enabled, paused_reason, updated_at
FROM bot_control
WHERE id = 1;

-- name: UpdateBotControl :one
UPDATE bot_control
SET trading_enabled = $1, paused_reason = $2
WHERE id = 1
RETURNING id, trading_enabled, paused_reason, updated_at;
