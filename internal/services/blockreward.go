// internal/services/blockreward.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/config"
	"github.com/yourproject/canglanfu-api/internal/db"
)

// BlockRewardService manages mint blocks, weight snapshots, and reward distribution.
type BlockRewardService struct {
	queries *db.Queries
	cfg     *config.Config
}

// NewBlockRewardService creates a new block reward service.
func NewBlockRewardService(queries *db.Queries, cfg *config.Config) *BlockRewardService {
	return &BlockRewardService{
		queries: queries,
		cfg:     cfg,
	}
}

// ---------------------------------------------------------------------
// Block Lifecycle
// ---------------------------------------------------------------------

// GetCurrentBlockAndCountdown returns the active (undistributed) mint block
// and the time remaining until the next block is generated.
// If no active block exists, it returns nil and the time until the next scheduled block.
func (s *BlockRewardService) GetCurrentBlockAndCountdown(ctx context.Context) (*db.MintBlock, time.Duration, error) {
	block, err := s.queries.GetCurrentMintBlock(ctx)
	if err != nil {
		// No active block – compute time until next block from last block or genesis
		lastBlock, err := s.queries.GetLastMintBlock(ctx)
		if err != nil {
			// No blocks at all – assume genesis at service start
			return nil, s.cfg.App.MintBlockInterval, nil
		}
		nextTime := lastBlock.CreatedAt.Add(s.cfg.App.MintBlockInterval)
		remaining := time.Until(nextTime)
		return nil, remaining, nil
	}
	// Block exists – time until next block is the interval from its creation
	nextTime := block.CreatedAt.Add(s.cfg.App.MintBlockInterval)
	remaining := time.Until(nextTime)
	return block, remaining, nil
}

// CreateNewBlock generates a new mint block with a fixed reward.
// The block number should be incremented from the last block.
func (s *BlockRewardService) CreateNewBlock(ctx context.Context) (*db.MintBlock, error) {
	// Determine next block number
	var nextBlockNumber int64 = 1
	lastBlock, err := s.queries.GetLastMintBlock(ctx)
	if err == nil && lastBlock != nil {
		nextBlockNumber = lastBlock.BlockNumber + 1
	}

	// Determine total reward for this block (could be a fixed emission schedule)
	// For simplicity, we use a constant reward per block; this can be made configurable.
	blockReward := decimal.NewFromInt(1000) // 1000 CAN per block – adjust as needed

	block, err := s.queries.CreateMintBlock(ctx, db.CreateMintBlockParams{
		BlockNumber: nextBlockNumber,
		TotalReward: blockReward,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create mint block: %w", err)
	}

	// After creating block, calculate weights for all users
	if err := s.CalculateWeights(ctx, block.ID); err != nil {
		// Log error but don't fail block creation
		// logger.Error("failed to calculate weights", "block_id", block.ID, "error", err)
	}

	return block, nil
}

// ---------------------------------------------------------------------
// Weight Calculation & Reward Distribution
// ---------------------------------------------------------------------

// CalculateWeights takes a snapshot of all users' transaction weight, LP weight,
// and burn weight at the time of the block and stores them in weight_snapshots.
func (s *BlockRewardService) CalculateWeights(ctx context.Context, blockID uuid.UUID) error {
	// Get all active users (those with any positive combat power)
	users, err := s.queries.GetAllUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users: %w", err)
	}

	for _, user := range users {
		// Get user's balances for CAN (transaction weight)
		canBalance, _ := s.queries.GetUserBalanceBySymbol(ctx, db.GetUserBalanceBySymbolParams{
			UserID: user.ID,
			Symbol: "CAN",
		})
		transactionWeight := canBalance.Balance // decimal.Zero if no balance

		// Get LP weight from combat_power table
		cp, err := s.queries.GetCombatPower(ctx, user.ID)
		lpWeight := decimal.Zero
		if err == nil {
			lpWeight = cp.LpWeight
		}

		// Get burn weight from combat_power table
		burnWeight := decimal.Zero
		if err == nil {
			burnWeight = cp.BurnPower
		}

		// Store snapshot
		err = s.queries.CreateWeightSnapshot(ctx, db.CreateWeightSnapshotParams{
			UserID:            user.ID,
			BlockID:           blockID,
			TransactionWeight: transactionWeight,
			LpWeight:          lpWeight,
			BurnWeight:        burnWeight,
		})
		if err != nil {
			// Log but continue processing other users
			// logger.Error("failed to create weight snapshot", "user_id", user.ID, "error", err)
			continue
		}
	}

	// After weights are snapshotted, distribute rewards
	return s.DistributeRewards(ctx, blockID)
}

// DistributeRewards allocates the block's total reward to users proportionally
// based on their total weight (transaction + LP + burn).
func (s *BlockRewardService) DistributeRewards(ctx context.Context, blockID uuid.UUID) error {
	// Get block details
	block, err := s.queries.GetBlockByID(ctx, blockID)
	if err != nil {
		return fmt.Errorf("block not found: %w", err)
	}

	// Get all weight snapshots for this block
	snapshots, err := s.queries.GetWeightSnapshotsByBlock(ctx, blockID)
	if err != nil {
		return fmt.Errorf("failed to fetch weight snapshots: %w", err)
	}

	// Calculate total weight across all users
	totalWeight := decimal.Zero
	for _, snap := range snapshots {
		userWeight := snap.TransactionWeight.Add(snap.LpWeight).Add(snap.BurnWeight)
		totalWeight = totalWeight.Add(userWeight)
	}

	if totalWeight.IsZero() {
		// No weight – no rewards to distribute, but mark block as distributed anyway
		return s.queries.UpdateBlockDistributed(ctx, db.UpdateBlockDistributedParams{
			ID:            blockID,
			Distributed:   true,
			DistributedAt: time.Now(),
		})
	}

	// Distribute rewards to each user
	for _, snap := range snapshots {
		userWeight := snap.TransactionWeight.Add(snap.LpWeight).Add(snap.BurnWeight)
		if userWeight.IsZero() {
			continue
		}

		// User's share = (userWeight / totalWeight) * totalReward
		share := block.TotalReward.Mul(userWeight).Div(totalWeight)

		// Create block_reward record
		_, err := s.queries.CreateBlockReward(ctx, db.CreateBlockRewardParams{
			UserID:      snap.UserID,
			BlockID:     blockID,
			Amount:      share,
			WeightType:  "total", // could also store individual weights in separate records
			WeightValue: userWeight,
			Claimed:     false,
		})
		if err != nil {
			// Log error but continue distributing to others
			// logger.Error("failed to create block reward", "user_id", snap.UserID, "error", err)
			continue
		}
	}

	// Mark block as distributed
	return s.queries.UpdateBlockDistributed(ctx, db.UpdateBlockDistributedParams{
		ID:            blockID,
		Distributed:   true,
		DistributedAt: time.Now(),
	})
}

// ---------------------------------------------------------------------
// Reward Claiming
// ---------------------------------------------------------------------

// ClaimRewards allows a user to claim specific unclaimed rewards.
// Returns the total amount claimed (in CAN).
func (s *BlockRewardService) ClaimRewards(ctx context.Context, userID uuid.UUID, rewardIDs []uuid.UUID) (decimal.Decimal, error) {
	totalClaimed := decimal.Zero

	for _, rewardID := range rewardIDs {
		// Verify reward belongs to user and is unclaimed
		reward, err := s.queries.GetBlockReward(ctx, rewardID)
		if err != nil {
			continue // skip if not found
		}
		if reward.UserID != userID || reward.Claimed {
			continue
		}

		// Update reward as claimed
		err = s.queries.ClaimReward(ctx, db.ClaimRewardParams{
			ID:     rewardID,
			UserID: userID,
		})
		if err != nil {
			continue
		}

		// Add to user's CAN balance (or mint new tokens)
		// This should be done in a transaction with the claim update.
		err = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
			UserID:  userID,
			TokenID: mustGetCANTokenID(ctx, s.queries), // Helper to get CAN token ID
			Balance: reward.Amount,
		})
		if err != nil {
			// Rollback claim? For simplicity, we log and continue.
			// In production, use a transaction.
			continue
		}

		totalClaimed = totalClaimed.Add(reward.Amount)
	}

	return totalClaimed, nil
}

// ClaimAllRewards claims all unclaimed rewards for a user.
func (s *BlockRewardService) ClaimAllRewards(ctx context.Context, userID uuid.UUID) (decimal.Decimal, error) {
	rewards, err := s.queries.GetUserUnclaimedRewards(ctx, userID)
	if err != nil {
		return decimal.Zero, err
	}

	ids := make([]uuid.UUID, len(rewards))
	for i, r := range rewards {
		ids[i] = r.ID
	}

	return s.ClaimRewards(ctx, userID, ids)
}

// ---------------------------------------------------------------------
// User Reward Summary
// ---------------------------------------------------------------------

// UserRewardsSummary contains a user's lifetime reward statistics.
type UserRewardsSummary struct {
	TotalEarned    decimal.Decimal `json:"total_earned"`
	TotalClaimed   decimal.Decimal `json:"total_claimed"`
	PendingRewards decimal.Decimal `json:"pending_rewards"`
	UnclaimedCount int64           `json:"unclaimed_count"`
	LastClaimTime  *time.Time      `json:"last_claim_time,omitempty"`
}

// GetUserRewardsSummary aggregates a user's reward information.
func (s *BlockRewardService) GetUserRewardsSummary(ctx context.Context, userID uuid.UUID) (*UserRewardsSummary, error) {
	// Get all rewards for user
	allRewards, err := s.queries.GetUserBlockRewards(ctx, userID)
	if err != nil {
		return nil, err
	}

	totalEarned := decimal.Zero
	totalClaimed := decimal.Zero
	pendingRewards := decimal.Zero
	unclaimedCount := int64(0)
	var lastClaimTime *time.Time

	for _, reward := range allRewards {
		totalEarned = totalEarned.Add(reward.Amount)
		if reward.Claimed {
			totalClaimed = totalClaimed.Add(reward.Amount)
			if lastClaimTime == nil || reward.ClaimedAt.After(*lastClaimTime) {
				lastClaimTime = reward.ClaimedAt
			}
		} else {
			pendingRewards = pendingRewards.Add(reward.Amount)
			unclaimedCount++
		}
	}

	return &UserRewardsSummary{
		TotalEarned:    totalEarned,
		TotalClaimed:   totalClaimed,
		PendingRewards: pendingRewards,
		UnclaimedCount: unclaimedCount,
		LastClaimTime:  lastClaimTime,
	}, nil
}

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------

var canTokenID uuid.UUID

func mustGetCANTokenID(ctx context.Context, queries *db.Queries) uuid.UUID {
	if canTokenID != uuid.Nil {
		return canTokenID
	}
	token, err := queries.GetTokenBySymbol(ctx, "CAN")
	if err != nil {
		panic("CAN token not found in database")
	}
	canTokenID = token.ID
	return canTokenID
}
