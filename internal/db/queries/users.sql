-- name: GetUserByWallet :one
SELECT * FROM users WHERE wallet_address = $1;

-- name: CreateUser :one
INSERT INTO users (wallet_address, referral_code, parent_id, invited_by, node_level)
VALUES ($1, $2, $3, $4, 0)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserNodeLevel :exec
UPDATE users SET node_level = $2, updated_at = NOW() WHERE id = $1;