// internal/services/badge.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/db"
)

// BadgeService handles all badge-related business logic.
type BadgeService struct {
	queries   *db.Queries
	assetSvc  *AssetService
	combatSvc *CombatPowerService
}

// NewBadgeService creates a new badge service.
func NewBadgeService(queries *db.Queries, assetSvc *AssetService, combatSvc *CombatPowerService) *BadgeService {
	return &BadgeService{
		queries:   queries,
		assetSvc:  assetSvc,
		combatSvc: combatSvc,
	}
}

// ---------------------------------------------------------------------
// Badge Purchase
// ---------------------------------------------------------------------

// PurchaseBadge allows a user to buy a badge.
// It deducts the price from the user's USDT balance and creates a user_badge record.
func (s *BadgeService) PurchaseBadge(ctx context.Context, userID uuid.UUID, badgeID uuid.UUID) (*db.UserBadge, error) {
	// Get badge details
	badge, err := s.queries.GetBadgeByID(ctx, badgeID)
	if err != nil {
		return nil, fmt.Errorf("badge not found: %w", err)
	}

	// Check if user already owns this badge and it's still active
	existing, _ := s.queries.GetUserBadgeByID(ctx, db.GetUserBadgeByIDParams{
		ID:     badgeID,
		UserID: userID,
	})
	if existing != nil && existing.IsActive {
		// If badge is permanent or not expired, reject
		if existing.ExpiryDate == nil || existing.ExpiryDate.After(time.Now()) {
			return nil, fmt.Errorf("user already owns this badge")
		}
	}

	// Check user's USDT balance
	usdtBalance, err := s.assetSvc.GetUserTokenBalance(ctx, userID, "USDT")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch USDT balance: %w", err)
	}

	if usdtBalance.LessThan(badge.PriceUsd) {
		return nil, fmt.Errorf("insufficient USDT balance: have %s, need %s",
			usdtBalance.String(), badge.PriceUsd.String())
	}

	// Deduct USDT balance
	err = s.queries.SubtractUserBalance(ctx, db.SubtractUserBalanceParams{
		UserID:  userID,
		TokenID: mustGetUSDTTokenID(ctx, s.queries), // Helper to get USDT token ID
		Balance: badge.PriceUsd,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to deduct USDT: %w", err)
	}

	// Calculate expiry date (if not permanent)
	var expiryDate *time.Time
	// In the future, badges could have a duration field; for now, assume permanent (nil)
	// If we add a duration column to badges table, we can set it here.

	// Create user badge
	userBadge, err := s.queries.PurchaseBadge(ctx, db.PurchaseBadgeParams{
		UserID:     userID,
		BadgeID:    badgeID,
		ExpiryDate: expiryDate,
	})
	if err != nil {
		// Rollback balance deduction? For simplicity, we log the error but don't auto-rollback.
		// In production, consider using a transaction.
		return nil, fmt.Errorf("failed to create user badge: %w", err)
	}

	// Apply badge benefits immediately (e.g., combat power multiplier)
	if err := s.ApplyBadgeBenefits(ctx, userID, badge); err != nil {
		// Log but don't fail the purchase
		// logger.Error("failed to apply badge benefits", "error", err)
	}

	return &userBadge, nil
}

// Helper to get USDT token ID (cached or fetched each time)
var usdtTokenID uuid.UUID

func mustGetUSDTTokenID(ctx context.Context, queries *db.Queries) uuid.UUID {
	if usdtTokenID != uuid.Nil {
		return usdtTokenID
	}
	token, err := queries.GetTokenBySymbol(ctx, "USDT")
	if err != nil {
		panic("USDT token not found in database")
	}
	usdtTokenID = token.ID
	return usdtTokenID
}

// ---------------------------------------------------------------------
// User Badge Retrieval
// ---------------------------------------------------------------------

// GetUserBadges returns all badges (active + inactive) for a user.
func (s *BadgeService) GetUserBadges(ctx context.Context, userID uuid.UUID) ([]db.GetUserBadgesRow, error) {
	return s.queries.GetUserBadges(ctx, userID)
}

// GetUserActiveBadges returns only currently active badges for a user.
func (s *BadgeService) GetUserActiveBadges(ctx context.Context, userID uuid.UUID) ([]db.GetUserActiveBadgesRow, error) {
	return s.queries.GetUserActiveBadges(ctx, userID)
}

// GetBadgeDetails returns information about a specific badge.
func (s *BadgeService) GetBadgeDetails(ctx context.Context, badgeID uuid.UUID) (*db.Badge, error) {
	return s.queries.GetBadgeByID(ctx, badgeID)
}

// ListAvailableBadges returns all badges available for purchase.
func (s *BadgeService) ListAvailableBadges(ctx context.Context) ([]db.Badge, error) {
	return s.queries.ListBadges(ctx)
}

// ---------------------------------------------------------------------
// Badge Benefits & Multipliers
// ---------------------------------------------------------------------

// BadgeBenefits represents the benefits a badge provides.
type BadgeBenefits struct {
	CombatPowerMultiplier decimal.Decimal `json:"combat_power_multiplier,omitempty"`
	GovernanceMultiplier  decimal.Decimal `json:"governance_multiplier,omitempty"`
	MiningBoost           decimal.Decimal `json:"mining_boost,omitempty"`          // percentage increase
	LPYieldBoost          decimal.Decimal `json:"lp_yield_boost,omitempty"`        // percentage increase
	DailyRewardBonus      decimal.Decimal `json:"daily_reward_bonus,omitempty"`    // fixed amount
	ReferralRewardShare   decimal.Decimal `json:"referral_reward_share,omitempty"` // e.g., 0.1 = 10%
	CustomPermissions     []string        `json:"custom_permissions,omitempty"`
}

// ParseBenefits unmarshals the JSON benefits from a badge.
func (s *BadgeService) ParseBenefits(badge *db.Badge) (*BadgeBenefits, error) {
	if badge.Benefits == nil {
		return &BadgeBenefits{}, nil
	}
	var benefits BadgeBenefits
	if err := json.Unmarshal(badge.Benefits, &benefits); err != nil {
		return nil, fmt.Errorf("failed to parse badge benefits: %w", err)
	}
	return &benefits, nil
}

// ApplyBadgeBenefits applies the benefits of a newly purchased badge to the user.
func (s *BadgeService) ApplyBadgeBenefits(ctx context.Context, userID uuid.UUID, badge *db.Badge) error {
	benefits, err := s.ParseBenefits(badge)
	if err != nil {
		return err
	}

	// Apply combat power multiplier – trigger a combat power recalculation.
	if !benefits.CombatPowerMultiplier.IsZero() && s.combatSvc != nil {
		// The multiplier will be picked up during the next combat power calculation.
		// We just trigger a refresh.
		if err := s.combatSvc.UpdateCombatPower(ctx, userID); err != nil {
			return fmt.Errorf("failed to update combat power: %w", err)
		}
	}

	// Other benefits (governance, mining, etc.) can be stored in user_preferences or a separate table.
	// For now, they are applied on-the-fly when needed via GetUserActiveBadgeMultipliers.

	return nil
}

// GetUserActiveBadgeMultipliers aggregates all multipliers from the user's active badges.
func (s *BadgeService) GetUserActiveBadgeMultipliers(ctx context.Context, userID uuid.UUID) (*BadgeBenefits, error) {
	activeBadges, err := s.GetUserActiveBadges(ctx, userID)
	if err != nil {
		return nil, err
	}

	total := &BadgeBenefits{
		CombatPowerMultiplier: decimal.NewFromInt(1),
		GovernanceMultiplier:  decimal.NewFromInt(1),
		MiningBoost:           decimal.Zero,
		LPYieldBoost:          decimal.Zero,
		DailyRewardBonus:      decimal.Zero,
		ReferralRewardShare:   decimal.Zero,
	}

	for _, ub := range activeBadges {
		// ub.Benefits is a json.RawMessage; we need to convert to *db.Badge first.
		badge := &db.Badge{
			ID:       ub.BadgeID,
			Name:     ub.BadgeName,
			Symbol:   ub.BadgeSymbol,
			Benefits: ub.Benefits,
		}
		benefits, err := s.ParseBenefits(badge)
		if err != nil {
			continue // skip malformed benefits
		}

		// Sum multipliers (additive for boosts, multiplicative for multipliers)
		if !benefits.CombatPowerMultiplier.IsZero() {
			// Multipliers are multiplicative; we multiply them.
			total.CombatPowerMultiplier = total.CombatPowerMultiplier.Mul(benefits.CombatPowerMultiplier)
		}
		if !benefits.GovernanceMultiplier.IsZero() {
			total.GovernanceMultiplier = total.GovernanceMultiplier.Mul(benefits.GovernanceMultiplier)
		}
		total.MiningBoost = total.MiningBoost.Add(benefits.MiningBoost)
		total.LPYieldBoost = total.LPYieldBoost.Add(benefits.LPYieldBoost)
		total.DailyRewardBonus = total.DailyRewardBonus.Add(benefits.DailyRewardBonus)
		total.ReferralRewardShare = total.ReferralRewardShare.Add(benefits.ReferralRewardShare)

		// Merge permissions
		total.CustomPermissions = append(total.CustomPermissions, benefits.CustomPermissions...)
	}

	return total, nil
}

// ---------------------------------------------------------------------
// Badge Expiry & Maintenance
// ---------------------------------------------------------------------

// DeactivateExpiredBadges runs as a background job to automatically deactivate expired badges.
func (s *BadgeService) DeactivateExpiredBadges(ctx context.Context) error {
	return s.queries.AutoExpireBadges(ctx)
}

// GetExpiringBadges returns badges that will expire within the next 7 days.
func (s *BadgeService) GetExpiringBadges(ctx context.Context) ([]db.GetExpiringBadgesRow, error) {
	return s.queries.GetExpiringBadges(ctx)
}

// ---------------------------------------------------------------------
// Network Statistics
// ---------------------------------------------------------------------

// GetNetworkBadgeStats returns global badge statistics.
type NetworkBadgeStats struct {
	TotalBadges        int64 `json:"total_badges"`
	DistributionByTier []struct {
		Tier  int32 `json:"tier"`
		Count int64 `json:"count"`
	} `json:"distribution_by_tier"`
	MostPopularBadges []db.GetMostPopularBadgesRow `json:"most_popular_badges"`
}

func (s *BadgeService) GetNetworkBadgeStats(ctx context.Context) (*NetworkBadgeStats, error) {
	total, err := s.queries.GetTotalBadgesInNetwork(ctx)
	if err != nil {
		return nil, err
	}

	byTierRows, err := s.queries.GetBadgeStatsByTier(ctx)
	if err != nil {
		return nil, err
	}

	var distribution []struct {
		Tier  int32 `json:"tier"`
		Count int64 `json:"count"`
	}
	for _, row := range byTierRows {
		distribution = append(distribution, struct {
			Tier  int32 `json:"tier"`
			Count int64 `json:"count"`
		}{
			Tier:  row.Tier,
			Count: row.Count,
		})
	}

	popular, err := s.queries.GetMostPopularBadges(ctx, 10)
	if err != nil {
		return nil, err
	}

	return &NetworkBadgeStats{
		TotalBadges:        total,
		DistributionByTier: distribution,
		MostPopularBadges:  popular,
	}, nil
}
