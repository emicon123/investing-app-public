-- Phase 5 bot-calendar queries — BotCalendarController + BotCalendarService +
-- ForexFactorySyncService port.
--
-- Write ownership: bot_calendar_event is written ONLY by the calendar sync path
-- (POST /bot/calendar/sync, ForexFactory source). The nightly purge deletes old rows.
-- No other Go package writes this table.

-- name: SearchCalendarEvents :many
SELECT *
FROM bot_calendar_event
WHERE event_time >= $1
  AND event_time < $2
  AND (CARDINALITY(sqlc.arg(impacts)::text[]) = 0 OR impact = ANY(sqlc.arg(impacts)::text[]))
  AND (sqlc.arg(currency)::text = '' OR currency = sqlc.arg(currency)::text)
ORDER BY event_time ASC
LIMIT $3 OFFSET $4;

-- name: CountCalendarEvents :one
SELECT COUNT(*)::bigint AS total
FROM bot_calendar_event
WHERE event_time >= $1
  AND event_time < $2
  AND (CARDINALITY(sqlc.arg(impacts)::text[]) = 0 OR impact = ANY(sqlc.arg(impacts)::text[]))
  AND (sqlc.arg(currency)::text = '' OR currency = sqlc.arg(currency)::text);

-- name: UpsertCalendarEvent :one
INSERT INTO bot_calendar_event (event_time, currency, country, event_name, impact,
    forecast, previous, actual, source, external_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (external_id)
DO UPDATE SET event_time = EXCLUDED.event_time,
              currency = EXCLUDED.currency,
              country = EXCLUDED.country,
              event_name = EXCLUDED.event_name,
              impact = EXCLUDED.impact,
              forecast = EXCLUDED.forecast,
              previous = EXCLUDED.previous,
              -- ADR-093 §1b: a source's own missing data must never blank out a previously
              -- known value (FF's feed has no actual pre-release; BLS-direct may have
              -- written one already). forecast/previous stay unconditional — nothing else
              -- races to write those fields.
              actual = COALESCE(EXCLUDED.actual, bot_calendar_event.actual),
              source = EXCLUDED.source
RETURNING id;

-- name: DeleteCalendarEventsBefore :execrows
DELETE FROM bot_calendar_event WHERE event_time < $1;

-- name: FindCalendarByNameAndTime :many
SELECT *
FROM bot_calendar_event
WHERE event_name = $1 AND event_time = $2
ORDER BY id ASC;
