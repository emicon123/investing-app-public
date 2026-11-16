-- settings: the settingscore store of portfolioB and portfolioC.

-- name: GetSettingByKey :one
SELECT * FROM settings WHERE key = $1;

-- name: ListSettings :many
SELECT key, value, updated_at
FROM settings
ORDER BY key;

-- name: UpdateSettingValue :one
UPDATE settings
SET value = $2
WHERE key = $1
RETURNING key, value, updated_at;
