-- internal/db/queries/nodes.sql
-- ============================================
-- Node & Referral Network Queries for Cang Lan Fu
-- ============================================

-- -----------------------------------------------------------------
-- Node Level Definitions
-- -----------------------------------------------------------------

-- name: ListNodeLevels :many
SELECT *
FROM node_levels
ORDER BY level ASC;

-- name: GetNodeLevel :one
SELECT *
FROM node_levels
WHERE level = $1;

-- name: CreateNodeLevel :one
INSERT INTO node_levels (
    level,
    required_team_power,
    required_direct_members,
    rights,
    gift_limit
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: UpdateNodeLevel :one
UPDATE node_levels
SET
    required_team_power = $2,
    required_direct_members = $3,
    rights = $4,
    gift_limit = $5
WHERE level = $1
RETURNING *;

-- name: DeleteNodeLevel :exec
DELETE FROM node_levels
WHERE level = $1;

-- -----------------------------------------------------------------
-- User Node Status
-- -----------------------------------------------------------------

-- name: GetUserNode :one
SELECT *
FROM user_nodes
WHERE user_id = $1;

-- name: UpsertUserNode :one
INSERT INTO user_nodes (
    user_id,
    current_level,
    team_power,
    team_members,
    direct_referrals,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, NOW()
)
ON CONFLICT (user_id)
DO UPDATE SET
    current_level = EXCLUDED.current_level,
    team_power = EXCLUDED.team_power,
    team_members = EXCLUDED.team_members,
    direct_referrals = EXCLUDED.direct_referrals,
    updated_at = NOW()
RETURNING *;

-- name: UpdateUserNodeLevel :exec
UPDATE user_nodes
SET
    current_level = $2,
    updated_at = NOW()
WHERE user_id = $1;

-- name: UpdateUserNodeTeamStats :exec
UPDATE user_nodes
SET
    team_power = $2,
    team_members = $3,
    updated_at = NOW()
WHERE user_id = $1;

-- name: IncrementDirectReferrals :exec
UPDATE user_nodes
SET
    direct_referrals = direct_referrals + 1,
    updated_at = NOW()
WHERE user_id = $1;

-- -----------------------------------------------------------------
-- Team Calculation Queries
-- -----------------------------------------------------------------

-- name: GetDownlineUserIDs :many
WITH RECURSIVE team_tree AS (
    SELECT id, invited_by, parent_id
    FROM users
    WHERE invited_by = $1 OR parent_id = $1
    UNION ALL
    SELECT u.id, u.invited_by, u.parent_id
    FROM users u
    INNER JOIN team_tree t ON u.invited_by = t.id OR u.parent_id = t.id
)
SELECT id FROM team_tree;

-- name: GetTeamPower :one
SELECT COALESCE(SUM(cp.personal_power), 0)::decimal
FROM combat_power cp
WHERE cp.user_id IN (
    WITH RECURSIVE team_tree AS (
        SELECT id FROM users WHERE invited_by = $1 OR parent_id = $1
        UNION ALL
        SELECT u.id
        FROM users u
        INNER JOIN team_tree t ON u.invited_by = t.id OR u.parent_id = t.id
    )
    SELECT id FROM team_tree
);

-- name: GetTeamMemberCount :one
WITH RECURSIVE team_tree AS (
    SELECT id, invited_by, parent_id
    FROM users
    WHERE invited_by = $1 OR parent_id = $1
    UNION ALL
    SELECT u.id, u.invited_by, u.parent_id
    FROM users u
    INNER JOIN team_tree t ON u.invited_by = t.id OR u.parent_id = t.id
)
SELECT COUNT(*) FROM team_tree;

-- name: GetDirectReferralCount :one
SELECT COUNT(*)
FROM users
WHERE invited_by = $1;

-- name: GetUserNodeWithDetails :one
SELECT
    un.*,
    u.wallet_address,
    u.parent_id,
    u.invited_by,
    nl.required_team_power,
    nl.required_direct_members,
    nl.rights,
    nl.gift_limit,
    (nl.required_team_power <= un.team_power) AS team_power_met,
    (nl.required_direct_members <= un.direct_referrals) AS members_met
FROM user_nodes un
JOIN users u ON un.user_id = u.id
LEFT JOIN node_levels nl ON nl.level = un.current_level + 1
WHERE un.user_id = $1;

-- -----------------------------------------------------------------
-- Node Upgrade Eligibility
-- -----------------------------------------------------------------

-- name: CheckNodeUpgradeEligibility :one
SELECT
    un.user_id,
    un.current_level,
    un.team_power,
    un.direct_referrals,
    nl.level AS next_level,
    nl.required_team_power,
    nl.required_direct_members,
    (un.team_power >= nl.required_team_power) AS power_eligible,
    (un.direct_referrals >= nl.required_direct_members) AS members_eligible,
    (un.team_power >= nl.required_team_power AND un.direct_referrals >= nl.required_direct_members) AS eligible
FROM user_nodes un
CROSS JOIN node_levels nl
WHERE un.user_id = $1
  AND nl.level = un.current_level + 1;

-- name: UpgradeUserNode :exec
UPDATE users
SET node_level = $2, updated_at = NOW()
WHERE id = $1;

-- name: BatchUpgradeNodes :exec
WITH eligible_users AS (
    SELECT
        un.user_id,
        un.current_level
    FROM user_nodes un
    JOIN node_levels nl ON nl.level = un.current_level + 1
    WHERE un.team_power >= nl.required_team_power
      AND un.direct_referrals >= nl.required_direct_members
)
UPDATE users u
SET node_level = u.node_level + 1,
    updated_at = NOW()
FROM eligible_users eu
WHERE u.id = eu.user_id;

-- -----------------------------------------------------------------
-- Network-Wide Node Statistics
-- -----------------------------------------------------------------

-- name: GetNetworkNodeDistribution :many
SELECT
    node_level,
    COUNT(*) AS user_count
FROM users
GROUP BY node_level
ORDER BY node_level;

-- name: GetNetworkStats :one
SELECT
    COUNT(*) AS total_users,
    AVG(node_level) AS average_node_level,
    MAX(node_level) AS highest_node_level,
    SUM(CASE WHEN node_level >= 1 THEN 1 ELSE 0 END) AS node_holders
FROM users;

-- name: GetTopNodes :many
SELECT
    u.id,
    u.wallet_address,
    u.node_level,
    un.team_power,
    un.team_members,
    un.direct_referrals
FROM users u
JOIN user_nodes un ON u.id = un.user_id
WHERE u.node_level > 0
ORDER BY u.node_level DESC, un.team_power DESC
LIMIT $1;

-- -----------------------------------------------------------------
-- Referral Tree Views
-- -----------------------------------------------------------------

-- name: GetReferralAncestors :many
WITH RECURSIVE ancestor_tree AS (
    SELECT id, wallet_address, parent_id, invited_by, node_level, 0 AS depth
    FROM users
    WHERE id = $1
    UNION ALL
    SELECT u.id, u.wallet_address, u.parent_id, u.invited_by, u.node_level, a.depth + 1
    FROM users u
    INNER JOIN ancestor_tree a ON u.id = a.parent_id OR u.id = a.invited_by
)
SELECT *
FROM ancestor_tree
WHERE id != $1
ORDER BY depth ASC;

-- name: GetReferralSubtree :many
WITH RECURSIVE subtree AS (
    SELECT id, wallet_address, invited_by, parent_id, node_level, 0 AS depth
    FROM users
    WHERE id = $1
    UNION ALL
    SELECT u.id, u.wallet_address, u.invited_by, u.parent_id, u.node_level, s.depth + 1
    FROM users u
    INNER JOIN subtree s ON u.invited_by = s.id OR u.parent_id = s.id
)
SELECT *
FROM subtree
WHERE id != $1
ORDER BY depth, created_at;

-- name: GetReferralTreeDepth :one
WITH RECURSIVE subtree AS (
    SELECT id, invited_by, parent_id, 0 AS depth
    FROM users
    WHERE id = $1
    UNION ALL
    SELECT u.id, u.invited_by, u.parent_id, s.depth + 1
    FROM users u
    INNER JOIN subtree s ON u.invited_by = s.id OR u.parent_id = s.id
)
SELECT COALESCE(MAX(depth), 0) FROM subtree;

-- -----------------------------------------------------------------
-- Team Power Aggregation (for all users – maintenance)
-- -----------------------------------------------------------------

-- name: RecalculateAllTeamStats :exec
WITH team_stats AS (
    SELECT
        u.id AS user_id,
        COALESCE((
            SELECT SUM(cp.personal_power)
            FROM combat_power cp
            WHERE cp.user_id IN (
                WITH RECURSIVE team AS (
                    SELECT id FROM users WHERE invited_by = u.id OR parent_id = u.id
                    UNION ALL
                    SELECT m.id
                    FROM users m
                    INNER JOIN team t ON m.invited_by = t.id OR m.parent_id = t.id
                )
                SELECT id FROM team
            )
        ), 0)::decimal AS team_power,
        (
            WITH RECURSIVE team AS (
                SELECT id FROM users WHERE invited_by = u.id OR parent_id = u.id
                UNION ALL
                SELECT m.id
                FROM users m
                INNER JOIN team t ON m.invited_by = t.id OR m.parent_id = t.id
            )
            SELECT COUNT(*) FROM team
        ) AS team_members
    FROM users u
)
UPDATE user_nodes un
SET
    team_power = ts.team_power,
    team_members = ts.team_members,
    updated_at = NOW()
FROM team_stats ts
WHERE un.user_id = ts.user_id;

-- -----------------------------------------------------------------
-- Rights & Gift Limits
-- -----------------------------------------------------------------

-- name: GetUserRights :one
SELECT nl.rights
FROM users u
JOIN node_levels nl ON u.node_level = nl.level
WHERE u.id = $1;

-- name: GetUserGiftLimit :one
SELECT nl.gift_limit
FROM users u
JOIN node_levels nl ON u.node_level = nl.level
WHERE u.id = $1;

-- name: CheckGiftLimitAvailable :one
SELECT
    nl.gift_limit - COALESCE(SUM(amount), 0) AS remaining_limit
FROM users u
JOIN node_levels nl ON u.node_level = nl.level
LEFT JOIN withdrawals w ON u.id = w.user_id AND w.created_at >= DATE_TRUNC('month', NOW())
WHERE u.id = $1
GROUP BY nl.gift_limit;