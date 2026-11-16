-- Settings queries — own `settings` exclusively for WRITES (the settings update; doc 13 §6.3).
-- Reads of single keys from other domains (macro's derisk gate, the schedulers' config, purchase
-- planning's account caps) are read-only.
--
-- NOTE: GetSettingByKey (SELECT key, value WHERE key = $1) already exists in macro.sql —
-- REUSED from there, deliberately NOT duplicated here (sqlc generates one Go func per query
-- name across all files; a second definition would collide). This file adds only what macro.sql
-- does not have: the full-row list (with updated_at) and the validated single-key update.
-- updated_at is set by the set_updated_at() BEFORE UPDATE trigger (V1__init.sql) — Go never
-- sends updated_at, it only reads back what the trigger wrote via RETURNING.

-- name: ListSettings :many
SELECT key, value, updated_at
FROM settings
ORDER BY key;

-- name: UpdateSettingValue :one
UPDATE settings
SET value = $2
WHERE key = $1
RETURNING key, value, updated_at;
