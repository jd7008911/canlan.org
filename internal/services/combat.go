package services

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/db"
)

type CombatPowerService struct {
	queries *db.Queries
}

func NewCombatPowerService(queries *db.Queries) *CombatPowerService {
	return &CombatPowerService{queries: queries}
}

// CalculatePersonalPower computes a user's combat power based on:
// - Token holdings (LAN/CAN)
// - LP positions
// - Burns
// - Badge multipliers
func (s *CombatPowerService) CalculatePersonalPower(ctx context.Context, userID uuid.UUID) (decimal.Decimal, error) {
	// Get user's token balances
	balances, err := s.queries.GetUserBalances(ctx, userID)
	if err != nil {
		return decimal.Zero, err
	}

	// Get LP weight
	lp, err := s.queries.GetUserLPWeight(ctx, userID)
	if err != nil {
		lp = decimal.Zero
	}

	// Get burn power
	burn, err := s.queries.GetUserBurnPower(ctx, userID)
	if err != nil {
		burn = decimal.Zero
	}

	// Get badge multipliers
	badges, err := s.queries.GetUserActiveBadges(ctx, userID)
	multiplier := decimal.NewFromInt(1)
	for _, b := range badges {
		// Parse benefits JSON to extract combat_power_multiplier
		// Simplified: assume multiplier is additive
		if b.Benefits != nil {
			// parse multiplier logic
		}
	}

	// Base power = holdings + LP weight + burn power
	basePower := decimal.Zero
	for _, b := range balances {
		// Only LAN and CAN contribute to combat power
		token, _ := s.queries.GetTokenByID(ctx, b.TokenID)
		if token.Symbol == "LAN" || token.Symbol == "CAN" {
			basePower = basePower.Add(b.Balance)
		}
	}
	basePower = basePower.Add(lp).Add(burn)

	// Apply multiplier
	personalPower := basePower.Mul(multiplier)

	return personalPower, nil
}

// UpdateCombatPower refreshes a user's combat power and stores it
func (s *CombatPowerService) UpdateCombatPower(ctx context.Context, userID uuid.UUID) error {
	personal, err := s.CalculatePersonalPower(ctx, userID)
	if err != nil {
		return err
	}

	// Get network power (team power from referrals)
	team, err := s.queries.GetTeamCombatPower(ctx, userID)
	if err != nil {
		team = decimal.Zero
	}

	// Get LP weight and burn power from DB
	lp, _ := s.queries.GetUserLPWeight(ctx, userID)
	burn, _ := s.queries.GetUserBurnPower(ctx, userID)

	return s.queries.UpsertCombatPower(ctx, db.UpsertCombatPowerParams{
		UserID:        userID,
		PersonalPower: personal,
		NetworkPower:  team,
		LpWeight:      lp,
		BurnPower:     burn,
	})
}
