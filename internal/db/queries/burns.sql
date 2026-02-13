-- name: CreateBurn :one
INSERT INTO burns (user_id, token_id, amount, combat_power_gained, tx_hash)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserBurns :many
SELECT * FROM burns WHERE user_id = $1 ORDER BY created_at DESC;