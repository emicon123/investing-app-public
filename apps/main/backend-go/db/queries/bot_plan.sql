-- Phase 5 bot-plan queries — BotPlanController + BotPlanService port.
--
-- Write ownership: bot_daily_plan + bot_plan_scenario are written ONLY by the plan
-- ingest path (POST /bot/plans/ingest, upsert by plan_date, scenarios replaced).
-- No other Go package writes these tables. The watcher reads scenarios by event_time.

-- name: FindPlanByDate :one
SELECT id, plan_date, status, summary, market_context, model, created_at, updated_at
FROM bot_daily_plan
WHERE plan_date = $1;

-- name: LatestPlan :one
SELECT id, plan_date, status, summary, market_context, model, created_at, updated_at
FROM bot_daily_plan
ORDER BY plan_date DESC
LIMIT 1;

-- name: ListPlans :many
SELECT id, plan_date, status, summary, market_context, model, created_at, updated_at
FROM bot_daily_plan
ORDER BY plan_date ASC
LIMIT $1 OFFSET $2;

-- name: CountPlans :one
SELECT COUNT(*)::bigint AS total FROM bot_daily_plan;

-- name: UpsertPlan :one
INSERT INTO bot_daily_plan (plan_date, status, summary, market_context, model)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (plan_date)
DO UPDATE SET status = EXCLUDED.status,
              summary = EXCLUDED.summary,
              market_context = EXCLUDED.market_context,
              model = EXCLUDED.model
RETURNING id, plan_date, status, summary, market_context, model, created_at, updated_at;

-- name: DeleteScenariosByPlanID :exec
DELETE FROM bot_plan_scenario WHERE plan_id = $1;

-- name: InsertScenario :one
INSERT INTO bot_plan_scenario (plan_id, calendar_event_id, event_name, event_time,
    scenario_label, trigger_condition, action, leverage, surprise_min, surprise_max,
    confidence, reasoning)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id, plan_id, calendar_event_id, event_name, event_time, scenario_label,
    trigger_condition, action, leverage, reasoning, created_at, surprise_min,
    surprise_max, confidence;

-- name: ScenariosByPlanID :many
SELECT id, plan_id, calendar_event_id, event_name, event_time, scenario_label,
    trigger_condition, action, leverage, reasoning, created_at, surprise_min,
    surprise_max, confidence
FROM bot_plan_scenario
WHERE plan_id = $1
ORDER BY event_time ASC, id ASC;

-- name: WatcherSnapshots :many
SELECT s.id AS scenario_id, s.calendar_event_id, s.event_name, s.event_time,
    s.scenario_label, s.trigger_condition, s.action, s.leverage, s.surprise_min,
    s.surprise_max, s.confidence,
    c.id AS cal_id, c.actual AS cal_actual, c.forecast AS cal_forecast,
    c.previous AS cal_previous
FROM bot_plan_scenario s
LEFT JOIN bot_calendar_event c ON c.id = s.calendar_event_id
WHERE s.event_time >= $1 AND s.event_time <= $2
ORDER BY s.event_time ASC, s.event_name ASC, s.id ASC;

-- name: ExistingCalendarEventIDs :many
SELECT id
FROM bot_calendar_event
WHERE id = ANY(sqlc.arg(ids)::bigint[]);
