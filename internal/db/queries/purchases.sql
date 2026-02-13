-- name: CreatePurchase :one
INSERT INTO purchases (user_id, token_id, amount, price_usd, total_value, payment_token_id, status, expiry_date)
VALUES ($1, $2, $3, $4, $5, $6, 'pending', NOW() + INTERVAL '15 minutes')
RETURNING *;

-- name: GetPurchaseByID :one
SELECT * FROM purchases WHERE id = $1 AND user_id = $2;

-- name: UpdatePurchaseStatus :exec
UPDATE purchases 
SET status = $2, completed_at = CASE WHEN $2 = 'completed' THEN NOW() ELSE NULL END, tx_hash = $3
WHERE id = $1;

-- name: GetUserPurchases :many
SELECT p.*, t.symbol as token_symbol 
FROM purchases p
JOIN tokens t ON p.token_id = t.id
WHERE p.user_id = $1
ORDER BY p.created_at DESC
LIMIT $2 OFFSET $3;