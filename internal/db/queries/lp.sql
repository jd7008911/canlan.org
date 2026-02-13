-- internal/db/queries/lp.sql
-- ============================================
-- Liquidity Pool (LP) Queries for Cang Lan Fu
-- ============================================

-- -----------------------------------------------------------------
-- Pools
-- -----------------------------------------------------------------

-- name: ListLiquidityPools :many
SELECT *
FROM liquidity_pools
WHERE is_active = true
ORDER BY name ASC;

-- name: GetLiquidityPoolByID :one
SELECT *
FROM liquidity_pools
WHERE id = $1;

-- name: GetLiquidityPoolByTokens :one
SELECT *
FROM liquidity_pools
WHERE token0_id = $1 AND token1_id = $2;

-- name: CreateLiquidityPool :one
INSERT INTO liquidity_pools (
    name,
    token0_id,
    token1_id,
    total_liquidity_usd,
    apr,
    is_active
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: UpdateLiquidityPool :exec
UPDATE liquidity_pools
SET
    total_liquidity_usd = $2,
    apr = $3,
    updated_at = NOW()
WHERE id = $1;

-- name: UpdatePoolAPR :exec
UPDATE liquidity_pools
SET apr = $2, updated_at = NOW()
WHERE id = $1;

-- -----------------------------------------------------------------
-- LP Positions
-- -----------------------------------------------------------------

-- name: GetLPPosition :one
SELECT *
FROM lp_positions
WHERE id = $1;

-- name: GetUserLPPosition :one
SELECT *
FROM lp_positions
WHERE user_id = $1 AND pool_id = $2;

-- name: GetUserLPPositions :many
SELECT
    lp.*,
    p.name AS pool_name,
    t0.symbol AS token0_symbol,
    t1.symbol AS token1_symbol
FROM lp_positions lp
JOIN liquidity_pools p ON lp.pool_id = p.id
JOIN tokens t0 ON p.token0_id = t0.id
JOIN tokens t1 ON p.token1_id = t1.id
WHERE lp.user_id = $1
ORDER BY lp.created_at DESC;

-- name: CreateLPPosition :one
INSERT INTO lp_positions (
    user_id,
    pool_id,
    lp_amount,
    share_percentage
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: UpdateLPPosition :exec
UPDATE lp_positions
SET
    lp_amount = $3,
    share_percentage = $4,
    updated_at = NOW()
WHERE user_id = $1 AND pool_id = $2;

-- name: AddLpAmount :exec
UPDATE lp_positions
SET
    lp_amount = lp_amount + $3,
    share_percentage = $4,
    updated_at = NOW()
WHERE user_id = $1 AND pool_id = $2;

-- name: SubtractLpAmount :exec
UPDATE lp_positions
SET
    lp_amount = lp_amount - $3,
    share_percentage = $4,
    updated_at = NOW()
WHERE user_id = $1 AND pool_id = $2;

-- name: DeleteLPPosition :exec
DELETE FROM lp_positions
WHERE user_id = $1 AND pool_id = $2;

-- -----------------------------------------------------------------
-- LP Transactions (Add / Remove)
-- -----------------------------------------------------------------

-- name: CreateLPTransaction :one
INSERT INTO lp_transactions (
    user_id,
    position_id,
    type,
    token0_amount,
    token1_amount,
    lp_amount,
    tx_hash
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetUserLPTransactions :many
SELECT
    t.*,
    p.pool_id,
    pl.name AS pool_name
FROM lp_transactions t
JOIN lp_positions p ON t.position_id = p.id
JOIN liquidity_pools pl ON p.pool_id = pl.id
WHERE t.user_id = $1
ORDER BY t.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetLPTransactionsByPosition :many
SELECT *
FROM lp_transactions
WHERE position_id = $1
ORDER BY created_at DESC;

-- -----------------------------------------------------------------
-- LP Weight & Combat Power
-- -----------------------------------------------------------------

-- name: GetUserLPWeight :one
SELECT COALESCE(SUM(lp_amount), 0)::decimal AS lp_weight
FROM lp_positions
WHERE user_id = $1;

-- name: GetUserLPWeightByPool :one
SELECT COALESCE(lp_amount, 0)::decimal AS lp_weight
FROM lp_positions
WHERE user_id = $1 AND pool_id = $2;

-- name: GetAllUserLPWeights :many
SELECT
    user_id,
    COALESCE(SUM(lp_amount), 0)::decimal AS lp_weight
FROM lp_positions
GROUP BY user_id;

-- -----------------------------------------------------------------
-- Pool Statistics
-- -----------------------------------------------------------------

-- name: GetPoolTotalLiquidity :one
SELECT total_liquidity_usd
FROM liquidity_pools
WHERE id = $1;

-- name: GetPoolTotalLPAmount :one
SELECT COALESCE(SUM(lp_amount), 0)::decimal
FROM lp_positions
WHERE pool_id = $1;

-- name: GetUserSharePercentage :one
SELECT
    COALESCE(
        lp.lp_amount / NULLIF(pool.total, 0),
        0
    )::decimal AS share_percentage
FROM lp_positions lp
CROSS JOIN (
    SELECT COALESCE(SUM(lp_amount), 0)::decimal AS total
    FROM lp_positions
    WHERE pool_id = $1
) pool
WHERE lp.user_id = $2 AND lp.pool_id = $1;

-- name: GetTopLiquidityProviders :many
SELECT
    u.id,
    u.wallet_address,
    COALESCE(SUM(lp.lp_amount), 0)::decimal AS total_lp
FROM users u
JOIN lp_positions lp ON u.id = lp.user_id
WHERE lp.pool_id = $1
GROUP BY u.id, u.wallet_address
ORDER BY total_lp DESC
LIMIT $2;

-- -----------------------------------------------------------------
-- Maintenance & Cleanup
-- -----------------------------------------------------------------

-- name: CleanupZeroLPPositions :exec
DELETE FROM lp_positions
WHERE lp_amount <= 0 OR lp_amount IS NULL;

-- name: RecalculateAllSharePercentages :exec
WITH pool_totals AS (
    SELECT
        pool_id,
        COALESCE(SUM(lp_amount), 0)::decimal AS total
    FROM lp_positions
    GROUP BY pool_id
)
UPDATE lp_positions lp
SET share_percentage =
    CASE
        WHEN pt.total > 0 THEN (lp.lp_amount / pt.total) * 100
        ELSE 0
    END,
    updated_at = NOW()
FROM pool_totals pt
WHERE lp.pool_id = pt.pool_id;

-- -----------------------------------------------------------------
-- Aggregated LP Value (USD) – requires token prices
-- -----------------------------------------------------------------

-- name: GetUserTotalLiquidityUSD :one
SELECT COALESCE(SUM(lp.lp_amount * p.total_liquidity_usd / NULLIF(pt.total, 0)), 0)::decimal
FROM lp_positions lp
JOIN liquidity_pools p ON lp.pool_id = p.id
CROSS JOIN LATERAL (
    SELECT COALESCE(SUM(lp_amount), 0)::decimal AS total
    FROM lp_positions
    WHERE pool_id = lp.pool_id
) pt
WHERE lp.user_id = $1;

-- name: GetPoolAPRHistory :many
SELECT
    apr,
    updated_at
FROM liquidity_pools
WHERE id = $1
ORDER BY updated_at DESC
LIMIT 30;