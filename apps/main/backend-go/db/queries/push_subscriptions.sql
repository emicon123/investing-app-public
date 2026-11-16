-- Web Push subscriptions (ADR-108, task 172). endpoint is a capability URL and p256dh/auth are key
-- material: rows from these queries never reach the API layer or a log line.

-- name: UpsertPushSubscription :one
-- The explicit updated_at also covers an upsert that changes no value (the trigger still fires).
INSERT INTO push_subscriptions (user_email, endpoint, p256dh, auth, device_label)
VALUES (@user_email, @endpoint, @p256dh, @auth, @device_label)
ON CONFLICT (endpoint) DO UPDATE SET
    user_email   = EXCLUDED.user_email,
    p256dh       = EXCLUDED.p256dh,
    auth         = EXCLUDED.auth,
    device_label = EXCLUDED.device_label,
    updated_at   = NOW()
RETURNING *;

-- name: DeletePushSubscriptionByEndpoint :exec
DELETE FROM push_subscriptions WHERE endpoint = $1;

-- name: DeletePushSubscriptionByID :exec
DELETE FROM push_subscriptions WHERE id = $1;

-- name: ListPushSubscriptions :many
SELECT * FROM push_subscriptions ORDER BY id;

-- name: ListPushSubscriptionsByUser :many
SELECT * FROM push_subscriptions WHERE user_email = $1 ORDER BY id;

-- name: MarkPushSubscriptionSuccess :exec
UPDATE push_subscriptions SET last_success_at = NOW() WHERE id = $1;
