// internal/services/mining.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/db"
)

// MiningService handles mining machine operations, daily earnings,
// machine upgrades, and acceleration release.
type MiningService struct {
	queries   *db.Queries
	assetSvc  *AssetService
	combatSvc *CombatPowerService
	badgeSvc  *BadgeService
}

// NewMiningService creates a new mining service.
func NewMiningService(queries *db.Queries, assetSvc *AssetService, combatSvc *CombatPowerService, badgeSvc *BadgeService) *MiningService {
	return &MiningService{
		queries:   queries,
		assetSvc:  assetSvc,
		combatSvc: combatSvc,
		badgeSvc:  badgeSvc,
	}
}

// ---------------------------------------------------------------------
// Mining Machine Management
// ---------------------------------------------------------------------

// GetUserMiningMachine retrieves the user's mining machine.
// If the user does not have one, it creates a default machine.
func (s *MiningService) GetUserMiningMachine(ctx context.Context, userID uuid.UUID) (*db.MiningMachine, error) {
	machine, err := s.queries.GetMiningMachine(ctx, userID)
	if err == nil {
		return &machine, nil
	}
	// Create default machine (level 1)
	machine, err = s.queries.CreateMiningMachine(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to create mining machine: %w", err)
	}
	return &machine, nil
}

// UpgradeMachine increases the user's mining machine level.
// Cost: could be in LAN or USDT – for simplicity we assume LAN.
// Returns the new machine state.
func (s *MiningService) UpgradeMachine(ctx context.Context, userID uuid.UUID) (*db.MiningMachine, error) {
	machine, err := s.GetUserMiningMachine(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Define upgrade costs per level (example values)
	// Level 1 -> 2: 100 LAN, Level 2 -> 3: 250 LAN, etc.
	upgradeCost := s.getUpgradeCost(machine.Level)

	// Check user's LAN balance
	lanBalance, err := s.assetSvc.GetUserTokenBalance(ctx, userID, "LAN")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch LAN balance: %w", err)
	}
	if lanBalance.LessThan(upgradeCost) {
		return nil, fmt.Errorf("insufficient LAN: need %s, have %s", upgradeCost.String(), lanBalance.String())
	}

	// Deduct cost
	lanToken, err := s.queries.GetTokenBySymbol(ctx, "LAN")
	if err != nil {
		return nil, fmt.Errorf("LAN token not found: %w", err)
	}
	err = s.queries.SubtractUserBalance(ctx, db.SubtractUserBalanceParams{
		UserID:  userID,
		TokenID: lanToken.ID,
		Balance: upgradeCost,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to deduct LAN: %w", err)
	}

	// Perform upgrade
	err = s.queries.UpgradeMiningMachine(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade machine: %w", err)
	}

	// Fetch updated machine
	updated, err := s.queries.GetMiningMachine(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Update combat power (mining machine may contribute to combat power?)
	if s.combatSvc != nil {
		_ = s.combatSvc.UpdateCombatPower(ctx, userID) // ignore error
	}

	return &updated, nil
}

// getUpgradeCost returns the cost in LAN to upgrade from the given level.
func (s *MiningService) getUpgradeCost(currentLevel int32) decimal.Decimal {
	switch currentLevel {
	case 1:
		return decimal.NewFromInt(100)
	case 2:
		return decimal.NewFromInt(250)
	case 3:
		return decimal.NewFromInt(500)
	case 4:
		return decimal.NewFromInt(1000)
	case 5:
		return decimal.NewFromInt(2000)
	default:
		return decimal.NewFromInt(5000) // level 6+
	}
}

// ---------------------------------------------------------------------
// Daily Earnings Calculation
// ---------------------------------------------------------------------

// MiningEarnings contains today's earnings broken down by type.
type MiningEarnings struct {
	StaticRelease       decimal.Decimal `json:"static_release"`       // base mining rate
	AccelerationRelease decimal.Decimal `json:"acceleration_release"` // bonus from level
	Total               decimal.Decimal `json:"total"`
}

// CalculateDailyEarnings computes the user's mining earnings for the current day.
// It does NOT persist them; use AccrueDailyEarnings to store.
func (s *MiningService) CalculateDailyEarnings(ctx context.Context, userID uuid.UUID) (*MiningEarnings, error) {
	machine, err := s.GetUserMiningMachine(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Base static rate (LAN per day)
	staticRate := machine.StaticRate

	// Acceleration rate from level (example: +0.001 per level)
	// This could be stored as acceleration_rate in the machine table.
	// For simplicity, we assume machine.AccelerationRate is already set.
	accelerationRate := machine.AccelerationRate

	// Apply badge boosts (mining boost percentage)
	var miningBoostMultiplier = decimal.NewFromInt(1)
	multipliers, err := s.badgeSvc.GetUserActiveBadgeMultipliers(ctx, userID)
	if err == nil && !multipliers.MiningBoost.IsZero() {
		// MiningBoost is a percentage (e.g., 0.1 = 10%)
		miningBoostMultiplier = decimal.NewFromInt(1).Add(multipliers.MiningBoost)
	}

	staticEarning := staticRate.Mul(miningBoostMultiplier)
	accelerationEarning := accelerationRate.Mul(miningBoostMultiplier)

	total := staticEarning.Add(accelerationEarning)

	return &MiningEarnings{
		StaticRelease:       staticEarning,
		AccelerationRelease: accelerationEarning,
		Total:               total,
	}, nil
}

// AccrueDailyEarnings calculates today's earnings and adds them to the user's LAN balance.
// It also records the earnings in mining_earnings table.
// Should be called once per day per user (by a cron job or on user action).
func (s *MiningService) AccrueDailyEarnings(ctx context.Context, userID uuid.UUID) (*MiningEarnings, error) {
	// Check if we already accrued earnings today (prevent double accrual)
	existing, err := s.queries.GetTodayMiningEarnings(ctx, db.GetTodayMiningEarningsParams{
		UserID: userID,
		// We'll need to adapt this – the query from mining.sql might return all earnings types.
		// For simplicity, we'll assume a function that checks if any record exists for today.
		// This is a placeholder; adjust based on actual queries.
	})
	if err == nil && len(existing) > 0 {
		return nil, fmt.Errorf("earnings already accrued today")
	}

	earnings, err := s.CalculateDailyEarnings(ctx, userID)
	if err != nil {
		return nil, err
	}

	machine, err := s.GetUserMiningMachine(ctx, userID)
	if err != nil {
		return nil, err
	}

	lanToken, err := s.queries.GetTokenBySymbol(ctx, "LAN")
	if err != nil {
		return nil, fmt.Errorf("LAN token not found: %w", err)
	}

	// Add earnings to user's LAN balance
	err = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
		UserID:  userID,
		TokenID: lanToken.ID,
		Balance: earnings.Total,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to credit LAN earnings: %w", err)
	}

	// Record static release
	_, err = s.queries.AddMiningEarning(ctx, db.AddMiningEarningParams{
		UserID:      userID,
		MachineID:   machine.ID,
		Amount:      earnings.StaticRelease,
		EarningType: "static",
		Date:        time.Now(),
	})
	if err != nil {
		// Non-critical, log but continue
	}

	// Record acceleration release
	_, err = s.queries.AddMiningEarning(ctx, db.AddMiningEarningParams{
		UserID:      userID,
		MachineID:   machine.ID,
		Amount:      earnings.AccelerationRelease,
		EarningType: "acceleration",
		Date:        time.Now(),
	})
	if err != nil {
		// Log only
	}

	// Update machine's total mined
	// This requires a custom query – we'll implement via a helper.
	err = s.queries.AddTotalMined(ctx, db.AddTotalMinedParams{
		UserID: userID,
		Amount: earnings.Total,
	})
	if err != nil {
		// Log only
	}

	return earnings, nil
}

// ---------------------------------------------------------------------
// Mining Statistics
// ---------------------------------------------------------------------

// UserMiningStats represents a user's mining performance.
type UserMiningStats struct {
	Level             int32           `json:"level"`
	StaticRate        decimal.Decimal `json:"static_rate"`
	AccelerationRate  decimal.Decimal `json:"acceleration_rate"`
	TotalMined        decimal.Decimal `json:"total_mined"`
	TodayEarnings     *MiningEarnings `json:"today_earnings,omitempty"`
	YesterdayEarnings decimal.Decimal `json:"yesterday_earnings,omitempty"`
	LastWeekEarnings  decimal.Decimal `json:"last_week_earnings,omitempty"`
	UpgradeCost       decimal.Decimal `json:"upgrade_cost"`
}

// GetUserMiningStats aggregates mining statistics for the dashboard.
func (s *MiningService) GetUserMiningStats(ctx context.Context, userID uuid.UUID) (*UserMiningStats, error) {
	machine, err := s.GetUserMiningMachine(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Get today's earnings (if any)
	todayEarnings, err := s.CalculateDailyEarnings(ctx, userID)
	if err != nil {
		todayEarnings = &MiningEarnings{StaticRelease: decimal.Zero, AccelerationRelease: decimal.Zero, Total: decimal.Zero}
	}

	// Get yesterday's total earnings (from mining_earnings)
	yesterday := time.Now().AddDate(0, 0, -1)
	yesterdayEarnings, err := s.queries.GetMiningEarningsByDate(ctx, db.GetMiningEarningsByDateParams{
		UserID: userID,
		Date:   yesterday,
	})
	if err != nil {
		yesterdayEarnings = decimal.Zero
	}

	// Get last 7 days total (excluding today)
	lastWeekEarnings, err := s.queries.GetMiningEarningsSumByDateRange(ctx, db.GetMiningEarningsSumByDateRangeParams{
		UserID:    userID,
		StartDate: time.Now().AddDate(0, 0, -7),
		EndDate:   time.Now().AddDate(0, 0, -1),
	})
	if err != nil {
		lastWeekEarnings = decimal.Zero
	}

	stats := &UserMiningStats{
		Level:             machine.Level,
		StaticRate:        machine.StaticRate,
		AccelerationRate:  machine.AccelerationRate,
		TotalMined:        machine.TotalMined,
		TodayEarnings:     todayEarnings,
		YesterdayEarnings: yesterdayEarnings,
		LastWeekEarnings:  lastWeekEarnings,
		UpgradeCost:       s.getUpgradeCost(machine.Level),
	}

	return stats, nil
}

// ---------------------------------------------------------------------
// Batch Operations (Cron Jobs)
// ---------------------------------------------------------------------

// ProcessDailyMiningForAllUsers is intended to be called by a cron job
// once per day to accrue mining earnings for every user.
func (s *MiningService) ProcessDailyMiningForAllUsers(ctx context.Context) error {
	users, err := s.queries.GetAllUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users: %w", err)
	}

	for _, user := range users {
		// Skip if already accrued today – AccrueDailyEarnings already checks.
		_, err := s.AccrueDailyEarnings(ctx, user.ID)
		if err != nil {
			// Log error and continue
			// logger.Error("failed to accrue mining earnings", "user_id", user.ID, "error", err)
			continue
		}
	}
	return nil
}

// ---------------------------------------------------------------------
// Helper Queries (to be added to sqlc queries if not present)
// ---------------------------------------------------------------------

// The following are pseudo‑queries – they should be defined in mining.sql.
// For completeness, we assume they exist or we add them.

func (q *db.Queries) AddTotalMined(ctx context.Context, arg db.AddTotalMinedParams) error {
	// Implementation via SQLC – will be generated.
	// UPDATE mining_machines SET total_mined = total_mined + $2 WHERE user_id = $1
	return nil
}

func (q *db.Queries) GetMiningEarningsByDate(ctx context.Context, arg db.GetMiningEarningsByDateParams) (decimal.Decimal, error) {
	// SELECT COALESCE(SUM(amount),0) FROM mining_earnings WHERE user_id = $1 AND date = $2
	return decimal.Zero, nil
}

func (q *db.Queries) GetMiningEarningsSumByDateRange(ctx context.Context, arg db.GetMiningEarningsSumByDateRangeParams) (decimal.Decimal, error) {
	// SELECT COALESCE(SUM(amount),0) FROM mining_earnings WHERE user_id = $1 AND date BETWEEN $2 AND $3
	return decimal.Zero, nil
}
