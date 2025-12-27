-- name: GetSystemSettings :one
SELECT * FROM system_settings;

-- name: UpdateSystemSettings :exec
UPDATE system_settings 
SET 
    rewards_enabled = COALESCE(sqlc.arg(rewards_enabled), rewards_enabled), 
    vaults_enabled = COALESCE(sqlc.arg(vaults_enabled), vaults_enabled), 
    smart_conversions_enabled = COALESCE(sqlc.arg(smart_conversions_enabled), smart_conversions_enabled), 
    rapid_ramp_enabled = COALESCE(sqlc.arg(rapid_ramp_enabled), rapid_ramp_enabled) 
WHERE id = 1;

