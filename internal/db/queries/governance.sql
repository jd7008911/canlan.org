-- internal/db/queries/governance.sql
-- ============================================
-- Governance Queries for Cang Lan Fu Platform
-- ============================================

-- -----------------------------------------------------------------
-- Proposals
-- -----------------------------------------------------------------

-- name: CreateProposal :one
INSERT INTO governance_proposals (
    proposer_id,
    title,
    description,
    proposal_type,
    voting_end,
    quorum,
    threshold
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetProposalByID :one
SELECT *
FROM governance_proposals
WHERE id = $1;

-- name: GetActiveProposals :many
SELECT *
FROM governance_proposals
WHERE status = 'active'
  AND voting_end > NOW()
ORDER BY voting_end ASC;

-- name: GetPendingProposals :many
SELECT *
FROM governance_proposals
WHERE status = 'pending'
ORDER BY created_at DESC;

-- name: GetCompletedProposals :many
SELECT *
FROM governance_proposals
WHERE status IN ('passed', 'rejected', 'executed')
ORDER BY voting_end DESC
LIMIT $1 OFFSET $2;

-- name: ListProposals :many
SELECT *
FROM governance_proposals
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: UpdateProposalStatus :exec
UPDATE governance_proposals
SET status = $2, updated_at = NOW()
WHERE id = $1;

-- name: UpdateProposalVotes :exec
UPDATE governance_proposals
SET
    for_votes = $2,
    against_votes = $3,
    abstain_votes = $4,
    total_votes = $5,
    updated_at = NOW()
WHERE id = $1;

-- name: CloseExpiredProposals :exec
UPDATE governance_proposals
SET status = 'rejected'
WHERE status = 'active'
  AND voting_end <= NOW();

-- name: ExecuteProposal :one
UPDATE governance_proposals
SET status = 'executed', updated_at = NOW()
WHERE id = $1
RETURNING *;

-- -----------------------------------------------------------------
-- Votes
-- -----------------------------------------------------------------

-- name: CastVote :one
INSERT INTO votes (
    user_id,
    proposal_id,
    vote_power,
    vote_choice
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (user_id, proposal_id)
DO UPDATE SET
    vote_power = EXCLUDED.vote_power,
    vote_choice = EXCLUDED.vote_choice,
    created_at = NOW()
RETURNING *;

-- name: GetVote :one
SELECT *
FROM votes
WHERE user_id = $1 AND proposal_id = $2;

-- name: GetProposalVotes :many
SELECT
    v.*,
    u.wallet_address
FROM votes v
JOIN users u ON v.user_id = u.id
WHERE v.proposal_id = $1
ORDER BY v.created_at DESC;

-- name: GetUserVotes :many
SELECT
    v.*,
    gp.title,
    gp.status
FROM votes v
JOIN governance_proposals gp ON v.proposal_id = gp.id
WHERE v.user_id = $1
ORDER BY v.created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountProposalVotes :one
SELECT COUNT(*) FROM votes WHERE proposal_id = $1;

-- name: SumVotePower :one
SELECT
    COALESCE(SUM(vote_power), 0)::decimal AS total_power
FROM votes
WHERE proposal_id = $1 AND vote_choice = $2;

-- -----------------------------------------------------------------
-- Voting Power Calculation (based on user's combat power / badges)
-- -----------------------------------------------------------------

-- name: GetUserVotePower :one
SELECT
    COALESCE(cp.personal_power, 0)::decimal +
    COALESCE(cp.lp_weight, 0)::decimal +
    COALESCE(cp.burn_power, 0)::decimal AS vote_power
FROM combat_power cp
WHERE cp.user_id = $1;

-- name: GetUserVotePowerWithMultiplier :one
WITH user_multipliers AS (
    SELECT
        COALESCE(
            (SELECT (benefits->>'governance_multiplier')::decimal
             FROM badges b
             JOIN user_badges ub ON b.id = ub.badge_id
             WHERE ub.user_id = $1
               AND ub.is_active = true
               AND (ub.expiry_date IS NULL OR ub.expiry_date > NOW())
               AND b.benefits ? 'governance_multiplier'
             ORDER BY (benefits->>'governance_multiplier')::decimal DESC
             LIMIT 1),
            1.0
        ) AS multiplier
)
SELECT
    (COALESCE(cp.personal_power, 0) + COALESCE(cp.lp_weight, 0) + COALESCE(cp.burn_power, 0)) *
    COALESCE(um.multiplier, 1.0) AS vote_power
FROM combat_power cp
CROSS JOIN user_multipliers um
WHERE cp.user_id = $1;

-- -----------------------------------------------------------------
-- Proposal Results & Analytics
-- -----------------------------------------------------------------

-- name: GetProposalResults :one
SELECT
    id,
    title,
    for_votes,
    against_votes,
    abstain_votes,
    total_votes,
    quorum,
    threshold,
    CASE
        WHEN total_votes >= (quorum / 100 * (SELECT COALESCE(SUM(personal_power), 0) FROM combat_power))
        THEN true ELSE false
    END AS quorum_reached,
    CASE
        WHEN for_votes >= (threshold / 100 * (for_votes + against_votes + abstain_votes))
        THEN true ELSE false
    END AS threshold_passed
FROM governance_proposals
WHERE id = $1;

-- name: GetTotalVotablePower :one
SELECT COALESCE(SUM(personal_power + lp_weight + burn_power), 0)::decimal
FROM combat_power;

-- name: GetProposalParticipation :one
SELECT
    COUNT(DISTINCT user_id) AS unique_voters,
    (SELECT COUNT(*) FROM users WHERE is_active = true) AS total_eligible,
    (COUNT(DISTINCT user_id)::decimal / NULLIF((SELECT COUNT(*) FROM users WHERE is_active = true), 0) * 100) AS participation_percentage
FROM votes
WHERE proposal_id = $1;

-- -----------------------------------------------------------------
-- Administrative / Maintenance
-- -----------------------------------------------------------------

-- name: DeleteProposal :exec
DELETE FROM governance_proposals WHERE id = $1;

-- name: DeleteVotesByProposal :exec
DELETE FROM votes WHERE proposal_id = $1;

-- name: GetExpiredUnfinalizedProposals :many
SELECT *
FROM governance_proposals
WHERE status = 'active'
  AND voting_end <= NOW();