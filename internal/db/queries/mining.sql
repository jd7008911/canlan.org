-- name: GetMiningMachine :one
SELECT * FROM mining_machines WHERE user_id = $1;

-- name: CreateMiningMachine :one
INSERT INTO mining_machines (user_id, level, static_rate, acceleration_rate)
VALUES ($1, 1, 0.01, 0.001)
RETURNING *;

-- name: UpgradeMiningMachine :exec
UPDATE mining_machines 
SET level = level + 1, 
    static_rate = static_rate * 1.1,
    acceleration_rate = acceleration_rate * 1.2,
    updated_at = NOW()
WHERE user_id = $1;

-- name: AddMiningEarning :exec
INSERT INTO mining_earnings (user_id, machine_id, amount, earning_type, date)
VALUES ($1, $2, $3, $4, CURRENT_DATE);

-- name: GetTodayMiningEarnings :many
SELECT * FROM mining_earnings 
WHERE user_id = $1 AND date = CURRENT_DATE AND earning_type = $2;