// internal/services/swap.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/db"
)

// SwapService handles token swap operations.
type SwapService struct {
	queries  *db.Queries
	assetSvc *AssetService
}

// NewSwapService creates a new swap service.
func NewSwapService(queries *db.Queries, assetSvc *AssetService) *SwapService {
	return &SwapService{
		queries:  queries,
		assetSvc: assetSvc,
	}
}

// ---------------------------------------------------------------------
// Swap Execution
// ---------------------------------------------------------------------

// SwapParams defines the input for executing a swap.
type SwapParams struct {
	UserID    uuid.UUID
	FromToken string          // e.g., "USDT"
	ToToken   string          // e.g., "CAN"
	Amount    decimal.Decimal // amount of fromToken to swap
	TxHash    string          // on‑chain transaction hash
}

// SwapResult contains the outcome of a swap.
type SwapResult struct {
	FromAmount decimal.Decimal `json:"from_amount"`
	ToAmount   decimal.Decimal `json:"to_amount"`
	Rate       decimal.Decimal `json:"rate"`
	TxHash     string          `json:"tx_hash"`
}

// ExecuteSwap converts one token to another at the current market rate.
// It deducts the fromToken from the user's balance and adds the toToken,
// then records the swap transaction.
func (s *SwapService) ExecuteSwap(ctx context.Context, params SwapParams) (*SwapResult, error) {
	// 1. Validate tokens and get current rate
	fromToken, err := s.queries.GetTokenBySymbol(ctx, params.FromToken)
	if err != nil {
		return nil, fmt.Errorf("from token not found: %s", params.FromToken)
	}
	toToken, err := s.queries.GetTokenBySymbol(ctx, params.ToToken)
	if err != nil {
		return nil, fmt.Errorf("to token not found: %s", params.ToToken)
	}

	// 2. Get swap rate (from token → to token)
	rate, err := s.queries.GetSwapRate(ctx, db.GetSwapRateParams{
		FromTokenID: fromToken.ID,
		ToTokenID:   toToken.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get swap rate: %w", err)
	}
	if rate.IsZero() {
		return nil, fmt.Errorf("swap rate is zero – price unavailable")
	}

	// 3. Calculate output amount
	toAmount := params.Amount.Mul(rate)

	// 4. Check user's fromToken balance
	fromBalance, err := s.assetSvc.GetUserTokenBalance(ctx, params.UserID, params.FromToken)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s balance: %w", params.FromToken, err)
	}
	if fromBalance.LessThan(params.Amount) {
		return nil, fmt.Errorf("insufficient %s balance: have %s, need %s",
			params.FromToken, fromBalance.String(), params.Amount.String())
	}

	// 5. Deduct fromToken, add toToken (atomic operation – should be in a transaction)
	err = s.queries.SubtractUserBalance(ctx, db.SubtractUserBalanceParams{
		UserID:  params.UserID,
		TokenID: fromToken.ID,
		Balance: params.Amount,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to deduct %s: %w", params.FromToken, err)
	}

	err = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
		UserID:  params.UserID,
		TokenID: toToken.ID,
		Balance: toAmount,
	})
	if err != nil {
		// Attempt to rollback the deduction – in production use a real transaction.
		_ = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
			UserID:  params.UserID,
			TokenID: fromToken.ID,
			Balance: params.Amount,
		})
		return nil, fmt.Errorf("failed to credit %s: %w", params.ToToken, err)
	}

	// 6. Record the swap
	swap, err := s.queries.CreateSwap(ctx, db.CreateSwapParams{
		UserID:      params.UserID,
		FromTokenID: fromToken.ID,
		ToTokenID:   toToken.ID,
		FromAmount:  params.Amount,
		ToAmount:    toAmount,
		Rate:        rate,
		TxHash:      params.TxHash,
		Status:      "completed",
	})
	if err != nil {
		// Non‑critical – log but don't revert the balance changes.
		// logger.Error("failed to record swap transaction", "error", err)
	}

	return &SwapResult{
		FromAmount: params.Amount,
		ToAmount:   toAmount,
		Rate:       rate,
		TxHash:     params.TxHash,
	}, nil
}

// ---------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------

// GetUserSwaps returns paginated swap history for a user.
func (s *SwapService) GetUserSwaps(ctx context.Context, userID uuid.UUID, limit, offset int32) ([]db.GetUserSwapsRow, error) {
	return s.queries.GetUserSwaps(ctx, db.GetUserSwapsParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
}

// GetUserSwapsByTimeRange returns swaps within a specific time range.
func (s *SwapService) GetUserSwapsByTimeRange(ctx context.Context, userID uuid.UUID, start, end time.Time) ([]db.GetUserSwapsByTimeRangeRow, error) {
	return s.queries.GetUserSwapsByTimeRange(ctx, db.GetUserSwapsByTimeRangeParams{
		UserID:    userID,
		StartTime: start,
		EndTime:   end,
	})
}

// GetSwapRate returns the current conversion rate between two tokens.
func (s *SwapService) GetSwapRate(ctx context.Context, fromSymbol, toSymbol string) (decimal.Decimal, error) {
	rate, err := s.queries.GetSwapRateWithSymbols(ctx, db.GetSwapRateWithSymbolsParams{
		FromSymbol: fromSymbol,
		ToSymbol:   toSymbol,
	})
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to get rate: %w", err)
	}
	return rate, nil
}

// ---------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------

// SwapVolumeStats contains volume information for swaps.
type SwapVolumeStats struct {
	TotalVolume24h decimal.Decimal              `json:"total_volume_24h"`
	SwapCount24h   int64                        `json:"swap_count_24h"`
	VolumeByToken  []db.GetSwapVolumeByTokenRow `json:"volume_by_token"`
	PopularPairs   []db.GetPopularSwapPairsRow  `json:"popular_pairs"`
}

// GetSwapStats returns global swap statistics.
func (s *SwapService) GetSwapStats(ctx context.Context) (*SwapVolumeStats, error) {
	volume24h, err := s.queries.GetSwapVolume24h(ctx)
	if err != nil {
		volume24h = decimal.Zero
	}

	swapCount24h, err := s.queries.GetSwapCount24h(ctx) // You would need to add this query
	if err != nil {
		swapCount24h = 0
	}

	volumeByToken, err := s.queries.GetSwapVolumeByToken(ctx)
	if err != nil {
		volumeByToken = []db.GetSwapVolumeByTokenRow{}
	}

	popularPairs, err := s.queries.GetPopularSwapPairs(ctx, 5)
	if err != nil {
		popularPairs = []db.GetPopularSwapPairsRow{}
	}

	return &SwapVolumeStats{
		TotalVolume24h: volume24h,
		SwapCount24h:   swapCount24h,
		VolumeByToken:  volumeByToken,
		PopularPairs:   popularPairs,
	}, nil
}
