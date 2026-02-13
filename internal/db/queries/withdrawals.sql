-- internal/db/queries/withdrawals.sql
-- ============================================
-- Withdrawal Queries for Cang Lan Fu Platform
-- ============================================

-- -----------------------------------------------------------------
-- Withdrawal Requests
-- -----------------------------------------------------------------

-- name: CreateWithdrawal :one
INSERT INTO withdrawals (
    user_id,
    token_id,
    amount,
    status,
    tx_hash,
    completed_at
) VALUES (
    $1, $2, $3, 'pending', NULL, NULL
)
RETURNING *;

-- name: GetWithdrawalByID :one
SELECT *
FROM withdrawals
WHERE id = $1;

-- name: GetWithdrawalByTxHash :one
SELECT *
FROM withdrawals
WHERE tx_hash = $1;

-- name: GetUserWithdrawals :many
SELECT
    w.*,
    t.symbol AS token_symbol,
    t.decimals
FROM withdrawals w
JOIN tokens t ON w.token_id = t.id
WHERE w.user_id = $1
ORDER BY w.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetUserWithdrawalsByStatus :many
SELECT
    w.*,
    t.symbol AS token_symbol
FROM withdrawals w
JOIN tokens t ON w.token_id = t.id
WHERE w.user_id = $1 AND w.status = $2
ORDER BY w.created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListPendingWithdrawals :many
SELECT
    w.*,
    u.wallet_address,
    t.symbol AS token_symbol
FROM withdrawals w
JOIN users u ON w.user_id = u.id
JOIN tokens t ON w.token_id = t.id
WHERE w.status = 'pending'
ORDER BY w.created_at ASC
LIMIT $1 OFFSET $2;

-- name: UpdateWithdrawalStatus :exec
UPDATE withdrawals
SET
    status = $2,
    tx_hash = COALESCE($3, tx_hash),
    completed_at = CASE WHEN $2 = 'completed' THEN NOW() ELSE NULL END
WHERE id = $1;

-- name: CancelWithdrawal :exec
UPDATE withdrawals
SET status = 'cancelled'
WHERE id = $1 AND user_id = $2 AND status = 'pending';

-- -----------------------------------------------------------------
-- Withdrawal Limits
-- -----------------------------------------------------------------

-- name: GetWithdrawalLimits :one
SELECT *
FROM withdrawal_limits
WHERE user_id = $1;

-- name: CreateWithdrawalLimits :one
INSERT INTO withdrawal_limits (
    user_id,
    daily_limit,
    monthly_limit,
    daily_used,
    monthly_used,
    last_daily_reset,
    last_monthly_reset
) VALUES (
    $1, $2, $3, 0, 0, CURRENT_DATE, CURRENT_DATE
)
RETURNING *;

-- name: UpsertWithdrawalLimits :one
INSERT INTO withdrawal_limits (
    user_id,
    daily_limit,
    monthly_limit,
    daily_used,
    monthly_used,
    last_daily_reset,
    last_monthly_reset
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (user_id)
DO UPDATE SET
    daily_limit = EXCLUDED.daily_limit,
    monthly_limit = EXCLUDED.monthly_limit,
    daily_used = EXCLUDED.daily_used,
    monthly_used = EXCLUDED.monthly_used,
    last_daily_reset = EXCLUDED.last_daily_reset,
    last_monthly_reset = EXCLUDED.last_monthly_reset,
    updated_at = NOW()
RETURNING *;

-- name: UpdateWithdrawalLimits :exec
UPDATE withdrawal_limits
SET
    daily_limit = $2,
    monthly_limit = $3,
    updated_at = NOW()
WHERE user_id = $1;

-- -----------------------------------------------------------------
-- Withdrawal Limit Usage & Reset
-- -----------------------------------------------------------------

-- name: IncrementWithdrawalUsage :exec
UPDATE withdrawal_limits
SET
    daily_used = daily_used + $2,
    monthly_used = monthly_used + $2,
    updated_at = NOW()
WHERE user_id = $1;

-- name: ResetDailyWithdrawalLimit :exec
UPDATE withdrawal_limits
SET
    daily_used = 0,
    last_daily_reset = CURRENT_DATE,
    updated_at = NOW()
WHERE user_id = $1;

-- name: ResetMonthlyWithdrawalLimit :exec
UPDATE withdrawal_limits
SET
    monthly_used = 0,
    last_monthly_reset = CURRENT_DATE,
    updated_at = NOW()
WHERE user_id = $1;

-- name: ResetAllDailyLimits :exec
UPDATE withdrawal_limits
SET
    daily_used = 0,
    last_daily_reset = CURRENT_DATE,
    updated_at = NOW()
WHERE last_daily_reset < CURRENT_DATE;

-- name: ResetAllMonthlyLimits :exec
UPDATE withdrawal_limits
SET
    monthly_used = 0,
    last_monthly_reset = CURRENT_DATE,
    updated_at = NOW()
WHERE last_monthly_reset < DATE_TRUNC('month', CURRENT_DATE);

-- -----------------------------------------------------------------
-- Withdrawal Limit Checks
-- -----------------------------------------------------------------

-- name: CheckWithdrawalLimit :one
SELECT
    (wl.daily_used + $2) <= wl.daily_limit AS daily_limit_ok,
    (wl.monthly_used + $2) <= wl.monthly_limit AS monthly_limit_ok,
    wl.daily_limit - (wl.daily_used + $2) AS remaining_daily,
    wl.monthly_limit - (wl.monthly_used + $2) AS remaining_monthly
FROM withdrawal_limits wl
WHERE wl.user_id = $1;

-- name: GetRemainingWithdrawalLimits :one
SELECT
    wl.daily_limit - wl.daily_used AS remaining_daily,
    wl.monthly_limit - wl.monthly_used AS remaining_monthly,
    wl.daily_limit,
    wl.monthly_limit,
    wl.daily_used,
    wl.monthly_used,
    wl.last_daily_reset,
    wl.last_monthly_reset
FROM withdrawal_limits wl
WHERE wl.user_id = $1;

-- -----------------------------------------------------------------
-- User Summary & Analytics
-- -----------------------------------------------------------------

-- name: GetUserWithdrawalSummary :one
SELECT
    COUNT(*) AS total_withdrawals,
    COALESCE(SUM(amount), 0)::decimal AS total_amount_withdrawn,
    COUNT(CASE WHEN status = 'pending' THEN 1 END) AS pending_count,
    COUNT(CASE WHEN status = 'completed' THEN 1 END) AS completed_count,
    COUNT(CASE WHEN status = 'cancelled' THEN 1 END) AS cancelled_count,
    COUNT(CASE WHEN status = 'failed' THEN 1 END) AS failed_count
FROM withdrawals
WHERE user_id = $1;

-- name: GetWithdrawalStatsByToken :many
SELECT
    t.symbol,
    COUNT(*) AS withdrawal_count,
    COALESCE(SUM(w.amount), 0)::decimal AS total_withdrawn,
    COALESCE(AVG(w.amount), 0)::decimal AS avg_withdrawal
FROM withdrawals w
JOIN tokens t ON w.token_id = t.id
WHERE w.user_id = $1 AND w.status = 'completed'
GROUP BY t.symbol
ORDER BY total_withdrawn DESC;

-- -----------------------------------------------------------------
-- Administrative Queries
-- -----------------------------------------------------------------

-- name: GetPendingWithdrawalsCount :one
SELECT COUNT(*)
FROM withdrawals
WHERE status = 'pending';

-- name: GetWithdrawalVolume24h :one
SELECT COALESCE(SUM(amount), 0)::decimal
FROM withdrawals
WHERE status = 'completed'
  AND completed_at >= NOW() - INTERVAL '24 hours';

-- name: DeleteOldWithdrawals :exec
DELETE FROM withdrawals
WHERE created_at < NOW() - INTERVAL '90 days'
  AND status IN ('completed', 'cancelled', 'failed');