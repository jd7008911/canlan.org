package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/db"
)

type BurnService struct {
	queries       *db.Queries
	combatService *CombatPowerService
}

func NewBurnService(queries *db.Queries, combat *CombatPowerService) *BurnService {
	return &BurnService{queries: queries, combatService: combat}
}

// BurnTokens processes a burn transaction
func (s *BurnService) BurnTokens(ctx context.Context, userID uuid.UUID, tokenID uuid.UUID, amount decimal.Decimal, txHash string) error {
	// Check user balance
	balance, err := s.queries.GetUserBalance(ctx, db.GetUserBalanceParams{
		UserID:  userID,
		TokenID: tokenID,
	})
	if err != nil {
		return fmt.Errorf("insufficient balance")
	}
	if balance.Balance.LessThan(amount) {
		return fmt.Errorf("insufficient balance")
	}

	// Deduct balance
	err = s.queries.UpdateUserBalance(ctx, db.UpdateUserBalanceParams{
		UserID:  userID,
		TokenID: tokenID,
		Balance: balance.Balance.Sub(amount),
	})
	if err != nil {
		return err
	}

	// Calculate combat power gained (e.g., 1:1 ratio for LAN)
	// In production, this may be dynamic based on token
	combatGained := amount

	// Record burn
	_, err = s.queries.CreateBurn(ctx, db.CreateBurnParams{
		UserID:            userID,
		TokenID:           tokenID,
		Amount:            amount,
		CombatPowerGained: combatGained,
		TxHash:            &txHash,
	})
	if err != nil {
		return err
	}

	// Update combat power
	err = s.queries.AddBurnPower(ctx, db.AddBurnPowerParams{
		UserID:    userID,
		BurnPower: combatGained,
	})
	if err != nil {
		return err
	}

	// Recalculate full combat power
	return s.combatService.UpdateCombatPower(ctx, userID)
}
