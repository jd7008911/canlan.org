-- internal/db/queries/swaps.sql
-- ============================================
-- Swap Queries for Cang Lan Fu Platform
-- ============================================

-- -----------------------------------------------------------------
-- Swap Transactions
-- -----------------------------------------------------------------

-- name: CreateSwap :one
INSERT INTO swaps (
    user_id,
    from_token_id,
    to_token_id,
    from_amount,
    to_amount,
    rate,
    tx_hash,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, 'completed'
)
RETURNING *;

-- name: GetSwapByID :one
SELECT *
FROM swaps
WHERE id = $1;

-- name: GetSwapByTxHash :one
SELECT *
FROM swaps
WHERE tx_hash = $1;

-- name: GetUserSwaps :many
SELECT
    s.*,
    ft.symbol AS from_token_symbol,
    tt.symbol AS to_token_symbol
FROM swaps s
JOIN tokens ft ON s.from_token_id = ft.id
JOIN tokens tt ON s.to_token_id = tt.id
WHERE s.user_id = $1
ORDER BY s.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetUserSwapsByTimeRange :many
SELECT
    s.*,
    ft.symbol AS from_token_symbol,
    tt.symbol AS to_token_symbol
FROM swaps s
JOIN tokens ft ON s.from_token_id = ft.id
JOIN tokens tt ON s.to_token_id = tt.id
WHERE s.user_id = $1
  AND s.created_at BETWEEN $2 AND $3
ORDER BY s.created_at DESC;

-- name: ListAllSwaps :many
SELECT
    s.*,
    u.wallet_address,
    ft.symbol AS from_token_symbol,
    tt.symbol AS to_token_symbol
FROM swaps s
JOIN users u ON s.user_id = u.id
JOIN tokens ft ON s.from_token_id = ft.id
JOIN tokens tt ON s.to_token_id = tt.id
ORDER BY s.created_at DESC
LIMIT $1 OFFSET $2;

-- -----------------------------------------------------------------
-- Swap Rate (from token prices)
-- -----------------------------------------------------------------

-- name: GetSwapRate :one
SELECT
    CASE
        WHEN $1::uuid = $2::uuid THEN 1.0
        ELSE COALESCE(
            (SELECT price_usd FROM tokens WHERE id = $1) /
            NULLIF((SELECT price_usd FROM tokens WHERE id = $2), 0),
            0
        )
    END::decimal AS rate;

-- name: GetSwapRateWithSymbols :one
SELECT
    CASE
        WHEN from_t.symbol = to_t.symbol THEN 1.0
        ELSE COALESCE(
            from_t.price_usd / NULLIF(to_t.price_usd, 0),
            0
        )
    END::decimal AS rate
FROM tokens from_t, tokens to_t
WHERE from_t.symbol = $1 AND to_t.symbol = $2;

-- name: GetTokenPrice :one
SELECT price_usd FROM tokens WHERE symbol = $1;

-- -----------------------------------------------------------------
-- Swap Statistics & Analytics
-- -----------------------------------------------------------------

-- name: GetUserSwapVolume :one
SELECT
    COALESCE(SUM(from_amount), 0)::decimal AS total_swapped,
    COUNT(*) AS swap_count
FROM swaps
WHERE user_id = $1
  AND created_at >= COALESCE($2::timestamp, '1970-01-01')
  AND created_at <= COALESCE($3::timestamp, NOW());

-- name: GetSwapVolume24h :one
SELECT
    COALESCE(SUM(from_amount), 0)::decimal AS volume_24h,
    COUNT(*) AS swap_count_24h
FROM swaps
WHERE created_at >= NOW() - INTERVAL '24 hours';

-- name: GetSwapVolumeByToken :many
SELECT
    t.symbol,
    COUNT(*) AS swap_count,
    COALESCE(SUM(s.from_amount), 0)::decimal AS total_volume
FROM swaps s
JOIN tokens t ON s.from_token_id = t.id
WHERE s.created_at >= NOW() - INTERVAL '7 days'
GROUP BY t.symbol
ORDER BY total_volume DESC;

-- name: GetPopularSwapPairs :many
SELECT
    ft.symbol AS from_token,
    tt.symbol AS to_token,
    COUNT(*) AS swap_count,
    COALESCE(SUM(s.from_amount), 0)::decimal AS total_volume
FROM swaps s
JOIN tokens ft ON s.from_token_id = ft.id
JOIN tokens tt ON s.to_token_id = tt.id
WHERE s.created_at >= NOW() - INTERVAL '30 days'
GROUP BY ft.symbol, tt.symbol
ORDER BY swap_count DESC
LIMIT $1;

-- -----------------------------------------------------------------
-- User Balance Updates (often paired with swaps)
-- -----------------------------------------------------------------

-- name: UpdateUserBalanceForSwap :exec
INSERT INTO user_balances (user_id, token_id, balance, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (user_id, token_id)
DO UPDATE SET
    balance = user_balances.balance + EXCLUDED.balance,
    updated_at = NOW();

-- -----------------------------------------------------------------
-- Pending / Failed Swaps (if needed)
-- -----------------------------------------------------------------

-- name: CreatePendingSwap :one
INSERT INTO swaps (
    user_id,
    from_token_id,
    to_token_id,
    from_amount,
    to_amount,
    rate,
    tx_hash,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, 'pending'
)
RETURNING *;

-- name: UpdateSwapStatus :exec
UPDATE swaps
SET
    status = $2,
    tx_hash = COALESCE($3, tx_hash)
WHERE id = $1;

-- -----------------------------------------------------------------
-- Cleanup
-- -----------------------------------------------------------------

-- name: DeleteOldSwaps :exec
DELETE FROM swaps
WHERE created_at < NOW() - INTERVAL '90 days'
  AND status = 'completed';