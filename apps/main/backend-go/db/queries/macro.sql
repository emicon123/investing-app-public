-- Macro-state queries — own macro_state and macro_observations exclusively
-- (doc 13 §6.3: both written only by the macro service; no other domain writes either table).
-- The GetSettingByKey query reads `settings` (written by the settings package) read-only, for the
-- macro-derisk flag — reading another package's table is fine, writing is not.
--
-- Write semantics (parity with MacroDataService.upsertOne, NOT an improvement): the
-- (series_code, observation_date) upsert overwrites source+value UNCONDITIONALLY — a MANUAL
-- paste over a CHINADATA month wins until the next refresh's backbone upsert reclaims it, and
-- the ADR-068 tip-precedence/outranks rule lives in Go code (service layer), not here.

-- name: UpsertObservation :exec
INSERT INTO macro_observations (series_code, source, observation_date, value)
VALUES ($1, $2, $3, $4)
ON CONFLICT (series_code, observation_date) DO UPDATE SET
    source = EXCLUDED.source,
    value = EXCLUDED.value;

-- name: ObsBySeriesAsc :many
SELECT series_code, source, observation_date, value
FROM macro_observations
WHERE series_code = $1
ORDER BY observation_date ASC;

-- name: ObsLatestBySeries :one
SELECT series_code, source, observation_date, value
FROM macro_observations
WHERE series_code = $1
ORDER BY observation_date DESC
LIMIT 1;

-- name: ObsLatestBySeriesAndSource :one
SELECT series_code, source, observation_date, value
FROM macro_observations
WHERE series_code = $1 AND source = $2
ORDER BY observation_date DESC
LIMIT 1;

-- name: ObsBySeriesAndDate :one
SELECT series_code, source, observation_date, value
FROM macro_observations
WHERE series_code = $1 AND observation_date = $2;

-- name: UpsertMacroState :one
INSERT INTO macro_state (
    as_of_date, risk_off,
    net_liq_latest_usd_mln, net_liq_yoy, net_liq_yoy_accel, net_liq_risk_off,
    global_m2_latest_usd_mln, global_m2_yoy, global_m2_yoy_accel, global_m2_risk_off,
    global_m2_stale, composite_z, smoothed_z, blend_factor, computed_at
) VALUES (
    $1, $2,
    $3, $4, $5, $6,
    $7, $8, $9, $10,
    $11, $12, $13, $14, $15
)
ON CONFLICT (as_of_date) DO UPDATE SET
    risk_off = EXCLUDED.risk_off,
    net_liq_latest_usd_mln = EXCLUDED.net_liq_latest_usd_mln,
    net_liq_yoy = EXCLUDED.net_liq_yoy,
    net_liq_yoy_accel = EXCLUDED.net_liq_yoy_accel,
    net_liq_risk_off = EXCLUDED.net_liq_risk_off,
    global_m2_latest_usd_mln = EXCLUDED.global_m2_latest_usd_mln,
    global_m2_yoy = EXCLUDED.global_m2_yoy,
    global_m2_yoy_accel = EXCLUDED.global_m2_yoy_accel,
    global_m2_risk_off = EXCLUDED.global_m2_risk_off,
    global_m2_stale = EXCLUDED.global_m2_stale,
    composite_z = EXCLUDED.composite_z,
    smoothed_z = EXCLUDED.smoothed_z,
    blend_factor = EXCLUDED.blend_factor,
    computed_at = EXCLUDED.computed_at
RETURNING *;

-- name: MacroStateLatestByComputedAt :one
-- Ordered by computed_at, NOT as_of_date: a refresh with no resolvable net-liquidity date
-- falls back to persisting under today's calendar date, which would otherwise permanently
-- outrank a later real row keyed on an earlier legitimate as-of date (releases always lag
-- "today") — the direct port of MacroStateRepository.findTopByOrderByComputedAtDesc.
SELECT *
FROM macro_state
ORDER BY computed_at DESC
LIMIT 1;

-- name: MacroStateByAsOfDate :one
SELECT *
FROM macro_state
WHERE as_of_date = $1;

-- name: MacroStateHistoryAsc :many
SELECT *
FROM macro_state
ORDER BY as_of_date ASC;

-- name: GetSettingByKey :one
SELECT key, value
FROM settings
WHERE key = $1;
