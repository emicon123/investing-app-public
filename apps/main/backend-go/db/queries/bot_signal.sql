-- Phase 5 bot-signal queries — BotSignalController + BotSignalService port.
--
-- Write ownership: bot_signal is written ONLY here (POST /bot/signals machine-only,
-- plus the watcher-internal path which reuses the same insert). executed is flipped
-- by MarkSignalExecuted. No other Go package writes this table.

-- name: InsertSignal :one
INSERT INTO bot_signal (calendar_event_id, plan_scenario_id, event_name, actual,
    forecast, previous, surprise_desc, decision, executed, raw_payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE, $9)
RETURNING id, calendar_event_id, plan_scenario_id, event_name, actual, forecast,
    previous, surprise_desc, decision, executed, raw_payload, created_at;

-- name: MarkSignalExecuted :one
UPDATE bot_signal
SET executed = TRUE
WHERE id = $1
RETURNING id, calendar_event_id, plan_scenario_id, event_name, actual, forecast,
    previous, surprise_desc, decision, executed, raw_payload, created_at;

-- name: ListSignals :many
SELECT id, calendar_event_id, plan_scenario_id, event_name, actual, forecast,
    previous, surprise_desc, decision, executed, raw_payload, created_at
FROM bot_signal
WHERE (sqlc.arg(event_name)::text = ''
    OR event_name ILIKE '%' || sqlc.arg(event_name)::text || '%')
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountSignals :one
SELECT COUNT(*)::bigint AS total
FROM bot_signal
WHERE (sqlc.arg(event_name)::text = ''
    OR event_name ILIKE '%' || sqlc.arg(event_name)::text || '%');
