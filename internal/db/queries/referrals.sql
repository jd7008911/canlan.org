-- name: GetReferralByCode :one
SELECT * FROM users WHERE referral_code = $1;

-- name: GetDirectReferrals :many
SELECT * FROM users WHERE invited_by = $1 ORDER BY created_at DESC;

-- name: CountDirectReferrals :one
SELECT COUNT(*) FROM users WHERE invited_by = $1;

-- name: GetTeamCombatPower :one
SELECT COALESCE(SUM(cp.personal_power), 0)::decimal
FROM combat_power cp
JOIN users u ON cp.user_id = u.id
WHERE u.invited_by = $1 OR u.parent_id = $1;