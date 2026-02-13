-- internal/db/queries/badges.sql
-- ============================================
-- Badges Queries for Cang Lan Fu Platform
-- ============================================

-- -----------------------------------------------------------------
-- Badge Catalog
-- -----------------------------------------------------------------

-- name: ListBadges :many
SELECT *
FROM badges
WHERE is_active = true
ORDER BY tier ASC, name ASC;

-- name: GetBadgeByID :one
SELECT *
FROM badges
WHERE id = $1;

-- name: GetBadgeBySymbol :one
SELECT *
FROM badges
WHERE symbol = $1;

-- name: GetBadgeByName :one
SELECT *
FROM badges
WHERE name = $1;

-- name: CreateBadge :one
INSERT INTO badges (
    name,
    symbol,
    description,
    icon_url,
    price_usd,
    benefits,
    tier,
    is_active
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: UpdateBadge :one
UPDATE badges
SET
    name = $2,
    symbol = $3,
    description = $4,
    icon_url = $5,
    price_usd = $6,
    benefits = $7,
    tier = $8,
    is_active = $9,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteBadge :exec
UPDATE badges
SET is_active = false, updated_at = NOW()
WHERE id = $1;

-- -----------------------------------------------------------------
-- User Badges (Purchases / Subscriptions)
-- -----------------------------------------------------------------

-- name: PurchaseBadge :one
INSERT INTO user_badges (
    user_id,
    badge_id,
    expiry_date,
    is_active
) VALUES (
    $1, $2, $3, true
)
RETURNING *;

-- name: GetUserBadges :many
SELECT
    ub.*,
    b.name AS badge_name,
    b.symbol AS badge_symbol,
    b.description,
    b.icon_url,
    b.benefits,
    b.tier,
    b.price_usd
FROM user_badges ub
JOIN badges b ON ub.badge_id = b.id
WHERE ub.user_id = $1
ORDER BY ub.purchased_at DESC;

-- name: GetUserActiveBadges :many
SELECT
    ub.*,
    b.name AS badge_name,
    b.symbol AS badge_symbol,
    b.description,
    b.icon_url,
    b.benefits,
    b.tier,
    b.price_usd
FROM user_badges ub
JOIN badges b ON ub.badge_id = b.id
WHERE ub.user_id = $1
  AND ub.is_active = true
  AND (ub.expiry_date IS NULL OR ub.expiry_date > NOW())
ORDER BY ub.purchased_at DESC;

-- name: GetUserBadgeByID :one
SELECT
    ub.*,
    b.name AS badge_name,
    b.symbol AS badge_symbol,
    b.description,
    b.icon_url,
    b.benefits,
    b.tier,
    b.price_usd
FROM user_badges ub
JOIN badges b ON ub.badge_id = b.id
WHERE ub.id = $1 AND ub.user_id = $2;

-- name: DeactivateUserBadge :exec
UPDATE user_badges
SET is_active = false, updated_at = NOW()
WHERE id = $1 AND user_id = $2;

-- name: CountUserBadges :one
SELECT COUNT(*)
FROM user_badges
WHERE user_id = $1 AND is_active = true;

-- name: CountUserBadgesByTier :many
SELECT
    b.tier,
    COUNT(*) AS count
FROM user_badges ub
JOIN badges b ON ub.badge_id = b.id
WHERE ub.user_id = $1 AND ub.is_active = true
GROUP BY b.tier
ORDER BY b.tier;

-- -----------------------------------------------------------------
-- Badge Benefits & Combat Power Multipliers
-- -----------------------------------------------------------------

-- name: GetUserBadgeMultipliers :many
SELECT b.benefits
FROM user_badges ub
JOIN badges b ON ub.badge_id = b.id
WHERE ub.user_id = $1
  AND ub.is_active = true
  AND (ub.expiry_date IS NULL OR ub.expiry_date > NOW());

-- -----------------------------------------------------------------
-- Network‑wide Badge Statistics
-- -----------------------------------------------------------------

-- name: GetTotalBadgesInNetwork :one
SELECT COUNT(*)
FROM user_badges
WHERE is_active = true;

-- name: GetBadgeStatsByTier :many
SELECT
    b.tier,
    COUNT(*) AS count
FROM user_badges ub
JOIN badges b ON ub.badge_id = b.id
WHERE ub.is_active = true
GROUP BY b.tier
ORDER BY b.tier;

-- name: GetNetworkBadgeHolders :many
SELECT
    u.wallet_address,
    u.id AS user_id,
    ub.purchased_at,
    b.name AS badge_name,
    b.symbol AS badge_symbol,
    b.tier
FROM user_badges ub
JOIN users u ON ub.user_id = u.id
JOIN badges b ON ub.badge_id = b.id
WHERE ub.is_active = true
ORDER BY ub.purchased_at DESC
LIMIT $1 OFFSET $2;

-- -----------------------------------------------------------------
-- Badge Direct List (Referral‑based badge holders)
-- -----------------------------------------------------------------

-- name: GetBadgeDirectList :many
SELECT
    u.wallet_address AS user_wallet,
    u.id AS user_id,
    ub.purchased_at,
    b.name AS badge_name,
    b.symbol AS badge_symbol,
    b.tier,
    referrer.wallet_address AS referrer_wallet,
    referrer.id AS referrer_id
FROM user_badges ub
JOIN users u ON ub.user_id = u.id
JOIN badges b ON ub.badge_id = b.id
LEFT JOIN users referrer ON u.invited_by = referrer.id
WHERE ub.is_active = true
ORDER BY ub.purchased_at DESC
LIMIT $1 OFFSET $2;

-- name: GetBadgeDirectListByReferrer :many
SELECT
    u.wallet_address AS user_wallet,
    u.id AS user_id,
    ub.purchased_at,
    b.name AS badge_name,
    b.symbol AS badge_symbol,
    b.tier
FROM user_badges ub
JOIN users u ON ub.user_id = u.id
JOIN badges b ON ub.badge_id = b.id
WHERE u.invited_by = $1
  AND ub.is_active = true
ORDER BY ub.purchased_at DESC
LIMIT $2 OFFSET $3;

-- -----------------------------------------------------------------
-- Badge Expiry & Maintenance
-- -----------------------------------------------------------------

-- name: GetExpiringBadges :many
SELECT
    ub.*,
    b.name AS badge_name,
    b.symbol AS badge_symbol,
    u.wallet_address
FROM user_badges ub
JOIN badges b ON ub.badge_id = b.id
JOIN users u ON ub.user_id = u.id
WHERE ub.expiry_date IS NOT NULL
  AND ub.expiry_date BETWEEN NOW() AND NOW() + INTERVAL '7 days'
  AND ub.is_active = true
ORDER BY ub.expiry_date ASC;

-- name: AutoExpireBadges :exec
UPDATE user_badges
SET is_active = false, updated_at = NOW()
WHERE expiry_date IS NOT NULL
  AND expiry_date <= NOW()
  AND is_active = true;

-- name: RenewUserBadge :one
UPDATE user_badges
SET
    expiry_date = $3,
    is_active = true,
    updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- -----------------------------------------------------------------
-- Administrative / Analytics
-- -----------------------------------------------------------------

-- name: GetMostPopularBadges :many
SELECT
    b.id,
    b.name,
    b.symbol,
    b.tier,
    COUNT(ub.id) AS purchase_count
FROM badges b
LEFT JOIN user_badges ub ON b.id = ub.badge_id AND ub.is_active = true
WHERE b.is_active = true
GROUP BY b.id
ORDER BY purchase_count DESC
LIMIT $1;

-- name: GetUserBadgePurchaseHistory :many
SELECT
    ub.*,
    b.name AS badge_name,
    b.symbol AS badge_symbol,
    b.tier
FROM user_badges ub
JOIN badges b ON ub.badge_id = b.id
WHERE ub.user_id = $1
ORDER BY ub.purchased_at DESC
LIMIT $2 OFFSET $3;