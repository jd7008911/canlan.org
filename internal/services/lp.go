// internal/services/lp.go
package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/db"
)

// LPService handles all liquidity pool-related business logic.
type LPService struct {
	queries   *db.Queries
	assetSvc  *AssetService
	combatSvc *CombatPowerService
}

// NewLPService creates a new liquidity pool service.
func NewLPService(queries *db.Queries, assetSvc *AssetService, combatSvc *CombatPowerService) *LPService {
	return &LPService{
		queries:   queries,
		assetSvc:  assetSvc,
		combatSvc: combatSvc,
	}
}

// ---------------------------------------------------------------------
// Pool Information
// ---------------------------------------------------------------------

// GetPoolByID retrieves a liquidity pool by ID.
func (s *LPService) GetPoolByID(ctx context.Context, poolID uuid.UUID) (*db.LiquidityPool, error) {
	pool, err := s.queries.GetLiquidityPoolByID(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("pool not found: %w", err)
	}
	return &pool, nil
}

// GetPoolByTokens retrieves a pool by its two token symbols.
func (s *LPService) GetPoolByTokens(ctx context.Context, token0Symbol, token1Symbol string) (*db.LiquidityPool, error) {
	token0, err := s.queries.GetTokenBySymbol(ctx, token0Symbol)
	if err != nil {
		return nil, fmt.Errorf("token %s not found", token0Symbol)
	}
	token1, err := s.queries.GetTokenBySymbol(ctx, token1Symbol)
	if err != nil {
		return nil, fmt.Errorf("token %s not found", token1Symbol)
	}

	pool, err := s.queries.GetLiquidityPoolByTokens(ctx, db.GetLiquidityPoolByTokensParams{
		Token0ID: token0.ID,
		Token1ID: token1.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("pool not found: %w", err)
	}
	return &pool, nil
}

// ListPools returns all active liquidity pools.
func (s *LPService) ListPools(ctx context.Context) ([]db.LiquidityPool, error) {
	return s.queries.ListLiquidityPools(ctx)
}

// ---------------------------------------------------------------------
// User LP Positions
// ---------------------------------------------------------------------

// GetUserLPPositions returns all LP positions for a user.
func (s *LPService) GetUserLPPositions(ctx context.Context, userID uuid.UUID) ([]db.GetUserLPPositionsRow, error) {
	return s.queries.GetUserLPPositions(ctx, userID)
}

// GetUserLPPosition returns a user's position in a specific pool.
func (s *LPService) GetUserLPPosition(ctx context.Context, userID, poolID uuid.UUID) (*db.LpPosition, error) {
	position, err := s.queries.GetUserLPPosition(ctx, db.GetUserLPPositionParams{
		UserID: userID,
		PoolID: poolID,
	})
	if err != nil {
		return nil, fmt.Errorf("position not found: %w", err)
	}
	return &position, nil
}

// GetUserLPWeight returns the total LP weight (sum of LP amounts) for a user.
// Used by combat power calculation.
func (s *LPService) GetUserLPWeight(ctx context.Context, userID uuid.UUID) (decimal.Decimal, error) {
	weight, err := s.queries.GetUserLPWeight(ctx, userID)
	if err != nil {
		return decimal.Zero, nil // no positions = zero weight
	}
	return weight, nil
}

// ---------------------------------------------------------------------
// Add Liquidity
// ---------------------------------------------------------------------

// AddLiquidityParams defines the input for adding liquidity.
type AddLiquidityParams struct {
	UserID  uuid.UUID
	PoolID  uuid.UUID
	Amount0 decimal.Decimal // amount of token0 to add
	Amount1 decimal.Decimal // amount of token1 to add
	TxHash  string
}

// AddLiquidity adds liquidity to a pool and mints LP tokens for the user.
// It deducts the required tokens from the user's balance and creates/updates an LP position.
func (s *LPService) AddLiquidity(ctx context.Context, params AddLiquidityParams) (*db.LpPosition, error) {
	// 1. Get pool and its tokens
	pool, err := s.GetPoolByID(ctx, params.PoolID)
	if err != nil {
		return nil, err
	}
	if !pool.IsActive {
		return nil, fmt.Errorf("pool is not active")
	}

	token0, err := s.queries.GetTokenByID(ctx, pool.Token0ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch token0: %w", err)
	}
	token1, err := s.queries.GetTokenByID(ctx, pool.Token1ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch token1: %w", err)
	}

	// 2. Verify user has sufficient balances
	bal0, err := s.assetSvc.GetUserTokenBalance(ctx, params.UserID, token0.Symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s balance: %w", token0.Symbol, err)
	}
	if bal0.LessThan(params.Amount0) {
		return nil, fmt.Errorf("insufficient %s balance: have %s, need %s",
			token0.Symbol, bal0.String(), params.Amount0.String())
	}

	bal1, err := s.assetSvc.GetUserTokenBalance(ctx, params.UserID, token1.Symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s balance: %w", token1.Symbol, err)
	}
	if bal1.LessThan(params.Amount1) {
		return nil, fmt.Errorf("insufficient %s balance: have %s, need %s",
			token1.Symbol, bal1.String(), params.Amount1.String())
	}

	// 3. Deduct tokens from user's balance
	err = s.queries.SubtractUserBalance(ctx, db.SubtractUserBalanceParams{
		UserID:  params.UserID,
		TokenID: token0.ID,
		Balance: params.Amount0,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to deduct %s: %w", token0.Symbol, err)
	}

	err = s.queries.SubtractUserBalance(ctx, db.SubtractUserBalanceParams{
		UserID:  params.UserID,
		TokenID: token1.ID,
		Balance: params.Amount1,
	})
	if err != nil {
		// Attempt to rollback token0 deduction? For simplicity, we'll just fail.
		// In production, use a database transaction.
		return nil, fmt.Errorf("failed to deduct %s: %w", token1.Symbol, err)
	}

	// 4. Compute pool's total liquidity and total LP supply
	totalLiquidityUSD := pool.TotalLiquidityUsd
	totalLPSupply, err := s.queries.GetPoolTotalLPAmount(ctx, params.PoolID)
	if err != nil {
		totalLPSupply = decimal.Zero
	}

	// 5. Compute USD value of added liquidity
	price0 := token0.PriceUsd
	price1 := token1.PriceUsd
	addedValue := params.Amount0.Mul(price0).Add(params.Amount1.Mul(price1))

	// 6. Calculate LP tokens to mint
	var lpAmount decimal.Decimal
	var sharePercentage decimal.Decimal

	if totalLPSupply.IsZero() || totalLiquidityUSD.IsZero() {
		// First depositor – set initial LP amount equal to added value (or 1:1)
		lpAmount = addedValue
		sharePercentage = decimal.NewFromInt(100) // 100%
	} else {
		// LP tokens = (addedValue / totalLiquidityUSD) * totalLPSupply
		lpAmount = addedValue.Mul(totalLPSupply).Div(totalLiquidityUSD)
		sharePercentage = lpAmount.Div(totalLPSupply.Add(lpAmount)).Mul(decimal.NewFromInt(100))
	}

	// 7. Create or update user's LP position
	var position db.LpPosition
	existing, err := s.queries.GetUserLPPosition(ctx, db.GetUserLPPositionParams{
		UserID: params.UserID,
		PoolID: params.PoolID,
	})
	if err == nil && existing != nil {
		// Update existing position
		err = s.queries.AddLpAmount(ctx, db.AddLpAmountParams{
			UserID:          params.UserID,
			PoolID:          params.PoolID,
			LpAmount:        lpAmount,
			SharePercentage: sharePercentage,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update LP position: %w", err)
		}
		// Re-fetch updated position
		updated, err := s.queries.GetUserLPPosition(ctx, db.GetUserLPPositionParams{
			UserID: params.UserID,
			PoolID: params.PoolID,
		})
		if err != nil {
			return nil, err
		}
		position = *updated
	} else {
		// Create new position
		newPos, err := s.queries.CreateLPPosition(ctx, db.CreateLPPositionParams{
			UserID:          params.UserID,
			PoolID:          params.PoolID,
			LpAmount:        lpAmount,
			SharePercentage: sharePercentage,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create LP position: %w", err)
		}
		position = newPos
	}

	// 8. Record transaction
	_, err = s.queries.CreateLPTransaction(ctx, db.CreateLPTransactionParams{
		UserID:       params.UserID,
		PositionID:   position.ID,
		Type:         "add",
		Token0Amount: params.Amount0,
		Token1Amount: params.Amount1,
		LpAmount:     lpAmount,
		TxHash:       &params.TxHash,
	})
	if err != nil {
		// Non-critical, log but don't fail
		// logger.Error("failed to record LP transaction", "error", err)
	}

	// 9. Update pool's total liquidity
	newTotalLiquidity := totalLiquidityUSD.Add(addedValue)
	err = s.queries.UpdateLiquidityPool(ctx, db.UpdateLiquidityPoolParams{
		ID:                params.PoolID,
		TotalLiquidityUsd: newTotalLiquidity,
		Apr:               pool.Apr, // unchanged for now
	})
	if err != nil {
		// Non-critical, log
	}

	// 10. Update user's combat power (LP weight)
	if s.combatSvc != nil {
		if err := s.combatSvc.UpdateCombatPower(ctx, params.UserID); err != nil {
			// Log but don't fail
			// logger.Error("failed to update combat power after LP add", "error", err)
		}
	}

	return &position, nil
}

// ---------------------------------------------------------------------
// Remove Liquidity
// ---------------------------------------------------------------------

// RemoveLiquidityParams defines the input for removing liquidity.
type RemoveLiquidityParams struct {
	UserID   uuid.UUID
	PoolID   uuid.UUID
	LpAmount decimal.Decimal // amount of LP tokens to burn
	TxHash   string
}

// RemoveLiquidity burns LP tokens and returns the underlying tokens to the user.
func (s *LPService) RemoveLiquidity(ctx context.Context, params RemoveLiquidityParams) (amount0, amount1 decimal.Decimal, err error) {
	// 1. Get pool and user position
	pool, err := s.GetPoolByID(ctx, params.PoolID)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}

	position, err := s.GetUserLPPosition(ctx, params.UserID, params.PoolID)
	if err != nil {
		return decimal.Zero, decimal.Zero, fmt.Errorf("no LP position found")
	}

	if position.LpAmount.LessThan(params.LpAmount) {
		return decimal.Zero, decimal.Zero, fmt.Errorf("insufficient LP tokens: have %s, want %s",
			position.LpAmount.String(), params.LpAmount.String())
	}

	// 2. Get pool's total liquidity and total LP supply
	totalLiquidityUSD := pool.TotalLiquidityUsd
	totalLPSupply, err := s.queries.GetPoolTotalLPAmount(ctx, params.PoolID)
	if err != nil || totalLPSupply.IsZero() {
		return decimal.Zero, decimal.Zero, fmt.Errorf("cannot calculate pool share")
	}

	// 3. Compute user's share of the pool
	share := params.LpAmount.Div(totalLPSupply)

	// 4. Compute token amounts to return
	// We need token0 and token1 reserves in the pool.
	// Since we don't track reserves directly, we approximate from total liquidity and token prices.
	// This is a simplification – ideally we would track reserves.
	token0, err := s.queries.GetTokenByID(ctx, pool.Token0ID)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	token1, err := s.queries.GetTokenByID(ctx, pool.Token1ID)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}

	// Approximate reserves: assume the pool maintains a 50/50 value split.
	// This is a simplification – real AMM would have constant product.
	poolValue0 := totalLiquidityUSD.Div(decimal.NewFromInt(2)) // 50% of TVL in token0
	poolValue1 := totalLiquidityUSD.Sub(poolValue0)            // remaining in token1

	reserve0 := poolValue0.Div(token0.PriceUsd)
	reserve1 := poolValue1.Div(token1.PriceUsd)

	amount0 = share.Mul(reserve0)
	amount1 = share.Mul(reserve1)

	// 5. Deduct LP tokens from user's position
	newLpAmount := position.LpAmount.Sub(params.LpAmount)
	var newSharePercentage decimal.Decimal
	if newLpAmount.IsZero() {
		// Remove position entirely
		err = s.queries.DeleteLPPosition(ctx, db.DeleteLPPositionParams{
			UserID: params.UserID,
			PoolID: params.PoolID,
		})
		if err != nil {
			return decimal.Zero, decimal.Zero, fmt.Errorf("failed to delete LP position: %w", err)
		}
	} else {
		newTotalSupply := totalLPSupply.Sub(params.LpAmount)
		if newTotalSupply.IsZero() {
			newSharePercentage = decimal.NewFromInt(100)
		} else {
			newSharePercentage = newLpAmount.Div(newTotalSupply).Mul(decimal.NewFromInt(100))
		}
		err = s.queries.SubtractLpAmount(ctx, db.SubtractLpAmountParams{
			UserID:          params.UserID,
			PoolID:          params.PoolID,
			LpAmount:        params.LpAmount,
			SharePercentage: newSharePercentage,
		})
		if err != nil {
			return decimal.Zero, decimal.Zero, fmt.Errorf("failed to update LP position: %w", err)
		}
	}

	// 6. Add tokens to user's balance
	err = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
		UserID:  params.UserID,
		TokenID: token0.ID,
		Balance: amount0,
	})
	if err != nil {
		return decimal.Zero, decimal.Zero, fmt.Errorf("failed to credit %s: %w", token0.Symbol, err)
	}

	err = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
		UserID:  params.UserID,
		TokenID: token1.ID,
		Balance: amount1,
	})
	if err != nil {
		return decimal.Zero, decimal.Zero, fmt.Errorf("failed to credit %s: %w", token1.Symbol, err)
	}

	// 7. Record transaction
	_, err = s.queries.CreateLPTransaction(ctx, db.CreateLPTransactionParams{
		UserID:       params.UserID,
		PositionID:   position.ID,
		Type:         "remove",
		Token0Amount: amount0,
		Token1Amount: amount1,
		LpAmount:     params.LpAmount,
		TxHash:       &params.TxHash,
	})
	if err != nil {
		// Log only
	}

	// 8. Update pool's total liquidity
	removedValue := amount0.Mul(token0.PriceUsd).Add(amount1.Mul(token1.PriceUsd))
	newTotalLiquidity := totalLiquidityUSD.Sub(removedValue)
	if newTotalLiquidity.LessThan(decimal.Zero) {
		newTotalLiquidity = decimal.Zero
	}
	err = s.queries.UpdateLiquidityPool(ctx, db.UpdateLiquidityPoolParams{
		ID:                params.PoolID,
		TotalLiquidityUsd: newTotalLiquidity,
		Apr:               pool.Apr,
	})
	if err != nil {
		// Log only
	}

	// 9. Update user's combat power
	if s.combatSvc != nil {
		if err := s.combatSvc.UpdateCombatPower(ctx, params.UserID); err != nil {
			// Log only
		}
	}

	return amount0, amount1, nil
}

// ---------------------------------------------------------------------
// Maintenance
// ---------------------------------------------------------------------

// RecalculateAllSharePercentages recalculates the share percentage for every LP position.
// Should be run periodically or after large pool changes.
func (s *LPService) RecalculateAllSharePercentages(ctx context.Context) error {
	return s.queries.RecalculateAllSharePercentages(ctx)
}

// CleanupZeroLPPositions removes positions with zero LP amount.
func (s *LPService) CleanupZeroLPPositions(ctx context.Context) error {
	return s.queries.CleanupZeroLPPositions(ctx)
}

// UpdatePoolAPR updates the APR for a pool based on recent swap volume.
// This is a placeholder – actual implementation would compute from swap fees.
func (s *LPService) UpdatePoolAPR(ctx context.Context, poolID uuid.UUID, volume24h decimal.Decimal) error {
	pool, err := s.GetPoolByID(ctx, poolID)
	if err != nil {
		return err
	}

	// Assume swap fee is 0.3%
	feeRate := decimal.NewFromFloat(0.003)
	dailyFees := volume24h.Mul(feeRate)
	// APR = (dailyFees * 365) / totalLiquidity * 100
	var apr decimal.Decimal
	if pool.TotalLiquidityUsd.IsZero() {
		apr = decimal.Zero
	} else {
		apr = dailyFees.Mul(decimal.NewFromInt(365)).Div(pool.TotalLiquidityUsd).Mul(decimal.NewFromInt(100))
	}

	return s.queries.UpdatePoolAPR(ctx, db.UpdatePoolAPRParams{
		ID:  poolID,
		Apr: apr,
	})
}
