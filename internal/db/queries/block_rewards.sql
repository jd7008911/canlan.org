-- name: GetCurrentMintBlock :one
SELECT * FROM mint_blocks 
WHERE distributed = false 
ORDER BY created_at DESC 
LIMIT 1;

-- name: GetUserWeightSnapshot :one
SELECT * FROM weight_snapshots 
WHERE user_id = $1 AND block_id = $2;

-- name: CreateWeightSnapshot :exec
INSERT INTO weight_snapshots (user_id, block_id, transaction_weight, lp_weight, burn_weight)
VALUES ($1, $2, $3, $4, $5);

-- name: GetUserUnclaimedRewards :many
SELECT * FROM block_rewards 
WHERE user_id = $1 AND claimed = false;

-- name: ClaimReward :exec
UPDATE block_rewards 
SET claimed = true, claimed_at = NOW() 
WHERE id = $1 AND user_id = $2;