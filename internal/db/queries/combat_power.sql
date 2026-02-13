-- name: GetCombatPower :one
SELECT * FROM combat_power WHERE user_id = $1;

-- name: UpsertCombatPower :exec
INSERT INTO combat_power (user_id, personal_power, network_power, lp_weight, burn_power, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (user_id) 
DO UPDATE SET 
    personal_power = EXCLUDED.personal_power,
    network_power = EXCLUDED.network_power,
    lp_weight = EXCLUDED.lp_weight,
    burn_power = EXCLUDED.burn_power,
    updated_at = NOW();

-- name: AddBurnPower :exec
UPDATE combat_power 
SET burn_power = burn_power + $2, personal_power = personal_power + $2, updated_at = NOW()
WHERE user_id = $1;

-- name: GetNetworkCombatPower :one
SELECT COALESCE(SUM(personal_power), 0)::decimal FROM combat_power;