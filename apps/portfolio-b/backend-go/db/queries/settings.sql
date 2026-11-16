-- Phase 8's own read-only GetSettingByKey (monthly_contribution_pln, used by
-- internal/rebalancing's funding policy). The settings endpoints' list/update queries live in the
-- shared common-go/siblingcore/refdata query set (task 180 wave 5).

-- name: GetSettingByKey :one
SELECT * FROM settings WHERE key = $1;
