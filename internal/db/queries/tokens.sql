-- internal/db/queries/tokens.sql
-- ============================================
-- Token Queries for Cang Lan Fu Platform
-- ============================================

-- -----------------------------------------------------------------
-- Token Metadata
-- -----------------------------------------------------------------

-- name: GetTokenByID :one
SELECT *
FROM tokens
WHERE id = $1;

-- name: GetTokenBySymbol :one
SELECT *
FROM tokens
WHERE symbol = $1;

-- name: GetTokenByContractAddress :one
SELECT *
FROM tokens
WHERE contract_address = $1;

-- name: ListTokens :many
SELECT *
FROM tokens
WHERE is_active = true
ORDER BY symbol ASC;

-- name: ListAllTokens :many
SELECT *
FROM tokens
ORDER BY symbol ASC;

-- name: CreateToken :one
INSERT INTO tokens (
    symbol,
    name,
    decimals,
    contract_address,
    price_usd,
    is_active
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: UpdateToken :one
UPDATE tokens
SET
    name = $2,
    decimals = $3,
    contract_address = $4,
    is_active = $5,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateTokenPrice :exec
UPDATE tokens
SET
    price_usd = $2,
    price_updated_at = NOW()
WHERE id = $1;

-- name: UpdateTokenPriceBySymbol :exec
UPDATE tokens
SET
    price_usd = $2,
    price_updated_at = NOW()
WHERE symbol = $1;

-- name: DeleteToken :exec
DELETE FROM tokens WHERE id = $1;

-- name: DeactivateToken :exec
UPDATE tokens
SET is_active = false, updated_at = NOW()
WHERE id = $1;

-- -----------------------------------------------------------------
-- User Token Balances
-- -----------------------------------------------------------------

-- name: GetUserBalance :one
SELECT *
FROM user_balances
WHERE user_id = $1 AND token_id = $2;

-- name: GetUserBalanceBySymbol :one
SELECT ub.*
FROM user_balances ub
JOIN tokens t ON ub.token_id = t.id
WHERE ub.user_id = $1 AND t.symbol = $2;

-- name: GetUserBalances :many
SELECT
    ub.*,
    t.symbol,
    t.name,
    t.decimals,
    t.price_usd,
    (ub.balance * t.price_usd)::decimal AS value_usd
FROM user_balances ub
JOIN tokens t ON ub.token_id = t.id
WHERE ub.user_id = $1
ORDER BY value_usd DESC, t.symbol ASC;

-- name: GetUserBalancesByTokenType :many
SELECT
    ub.*,
    t.symbol,
    t.name,
    t.price_usd,
    (ub.balance * t.price_usd)::decimal AS value_usd
FROM user_balances ub
JOIN tokens t ON ub.token_id = t.id
WHERE ub.user_id = $1
  AND t.symbol IN (sqlc.slice('symbols'))
ORDER BY t.symbol;

-- name: CreateUserBalance :one
INSERT INTO user_balances (
    user_id,
    token_id,
    balance
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: UpdateUserBalance :exec
INSERT INTO user_balances (user_id, token_id, balance)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, token_id)
DO UPDATE SET
    balance = EXCLUDED.balance,
    updated_at = NOW();

-- name: AddUserBalance :exec
UPDATE user_balances
SET
    balance = balance + $3,
    updated_at = NOW()
WHERE user_id = $1 AND token_id = $2;

-- name: SubtractUserBalance :exec
UPDATE user_balances
SET
    balance = balance - $3,
    updated_at = NOW()
WHERE user_id = $1 AND token_id = $2 AND balance >= $3;

-- -----------------------------------------------------------------
-- Token Statistics & Analytics
-- -----------------------------------------------------------------

-- name: GetTotalTokenSupply :one
-- Note: This assumes a `total_supply` column exists.
-- If not, you may need to join with purchase/swap data.
-- For now, we return NULL as placeholder.
SELECT NULL::decimal AS total_supply;

-- name: GetTokenHoldersCount :one
SELECT COUNT(DISTINCT user_id)
FROM user_balances
WHERE token_id = $1 AND balance > 0;

-- name: GetTopTokenHolders :many
SELECT
    u.id,
    u.wallet_address,
    ub.balance,
    (ub.balance * t.price_usd)::decimal AS value_usd
FROM user_balances ub
JOIN users u ON ub.user_id = u.id
JOIN tokens t ON ub.token_id = t.id
WHERE ub.token_id = $1 AND ub.balance > 0
ORDER BY ub.balance DESC
LIMIT $2;

-- name: GetTokenMarketCap :one
SELECT
    (COALESCE(SUM(ub.balance), 0) * t.price_usd)::decimal AS market_cap
FROM tokens t
LEFT JOIN user_balances ub ON t.id = ub.token_id
WHERE t.id = $1
GROUP BY t.id, t.price_usd;

-- name: GetTokensWithPositiveBalance :many
SELECT DISTINCT t.*
FROM tokens t
JOIN user_balances ub ON t.id = ub.token_id
WHERE ub.user_id = $1 AND ub.balance > 0
ORDER BY t.symbol;

-- -----------------------------------------------------------------
-- Price Oracle Support
-- -----------------------------------------------------------------

-- name: GetTokensNeedingPriceUpdate :many
SELECT *
FROM tokens
WHERE is_active = true
  AND (price_updated_at IS NULL OR price_updated_at < NOW() - INTERVAL '5 minutes')
ORDER BY price_updated_at NULLS FIRST;

-- name: BulkUpdateTokenPrices :exec
UPDATE tokens
SET
    price_usd = data_table.price_usd,
    price_updated_at = NOW()
FROM (
    SELECT
        unnest($1::uuid[]) AS id,
        unnest($2::decimal[]) AS price_usd
) AS data_table
WHERE tokens.id = data_table.id;