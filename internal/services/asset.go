// internal/services/asset.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"jd7008911/canlan.org/internal/db"
)

// AssetService handles all asset-related business logic.
type AssetService struct {
	queries *db.Queries
}

// NewAssetService creates a new asset service.
func NewAssetService(queries *db.Queries) *AssetService {
	return &AssetService{
		queries: queries,
	}
}

// ---------------------------------------------------------------------
// Portfolio & Balance
// ---------------------------------------------------------------------

// UserBalanceDetail represents a user's balance for a single token
// with enriched pricing and valuation.
type UserBalanceDetail struct {
	TokenID        uuid.UUID       `json:"token_id"`
	Symbol         string          `json:"symbol"`
	Name           string          `json:"name"`
	Balance        decimal.Decimal `json:"balance"`
	PriceUSD       decimal.Decimal `json:"price_usd"`
	ValueUSD       decimal.Decimal `json:"value_usd"`
	PriceChange24h decimal.Decimal `json:"price_change_24h,omitempty"`
}

// PortfolioSummary represents a user's complete asset portfolio.
type PortfolioSummary struct {
	TotalValueUSD decimal.Decimal     `json:"total_value_usd"`
	Balances      []UserBalanceDetail `json:"balances"`
	LastUpdated   time.Time           `json:"last_updated"`
}

// GetUserPortfolio retrieves all token balances for a user,
// enriches them with current prices, and calculates total value.
func (s *AssetService) GetUserPortfolio(ctx context.Context, userID uuid.UUID) (*PortfolioSummary, error) {
	// Get all token balances for user with token metadata and price
	balances, err := s.queries.GetUserBalances(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user balances: %w", err)
	}

	details := make([]UserBalanceDetail, 0, len(balances))
	total := decimal.Zero

	for _, b := range balances {
		balance := b.Balance
		price := b.PriceUSD
		value := balance.Mul(price)
		total = total.Add(value)

		details = append(details, UserBalanceDetail{
			TokenID:  b.TokenID,
			Symbol:   b.Symbol,
			Name:     b.Name,
			Balance:  balance,
			PriceUSD: price,
			ValueUSD: value,
			// PriceChange24h can be added later from token_price_history
		})
	}

	return &PortfolioSummary{
		TotalValueUSD: total,
		Balances:      details,
		LastUpdated:   time.Now(),
	}, nil
}

// GetUserTokenBalance returns the balance of a specific token for a user.
func (s *AssetService) GetUserTokenBalance(ctx context.Context, userID uuid.UUID, tokenSymbol string) (decimal.Decimal, error) {
	balance, err := s.queries.GetUserBalanceBySymbol(ctx, db.GetUserBalanceBySymbolParams{
		UserID: userID,
		Symbol: tokenSymbol,
	})
	if err != nil {
		// If no balance record exists, return zero
		return decimal.Zero, nil
	}
	return balance.Balance, nil
}

// ---------------------------------------------------------------------
// Token Prices
// ---------------------------------------------------------------------

// GetTokenPrice returns the current USD price for a token.
func (s *AssetService) GetTokenPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	token, err := s.queries.GetTokenBySymbol(ctx, symbol)
	if err != nil {
		return decimal.Zero, fmt.Errorf("token not found: %s", symbol)
	}
	return token.PriceUsd, nil
}

// RefreshTokenPrices updates token prices from an external oracle.
// This is a placeholder – actual implementation would call a price feed API
// and bulk update the database.
func (s *AssetService) RefreshTokenPrices(ctx context.Context) error {
	// In production, you would:
	// 1. Fetch prices from CoinGecko, Binance, or Chainlink oracle
	// 2. Map symbols to token IDs
	// 3. Call BulkUpdateTokenPrices with slices of IDs and prices

	// Example placeholder:
	// ids := []uuid.UUID{...}
	// prices := []decimal.Decimal{...}
	// return s.queries.BulkUpdateTokenPrices(ctx, db.BulkUpdateTokenPricesParams{
	//    IDs: ids,
	//    Prices: prices,
	// })
	return nil
}

// ---------------------------------------------------------------------
// Network-Wide Asset Statistics
// ---------------------------------------------------------------------

// NetworkAssetSummary returns global statistics about all assets on the platform.
type NetworkAssetSummary struct {
	TotalValueLockedUSD decimal.Decimal `json:"total_value_locked_usd"`
	TotalTokenHolders   int64           `json:"total_token_holders"`
	ActiveTokens        int64           `json:"active_tokens"`
	TopHolders          []TopHolder     `json:"top_holders,omitempty"`
}

// TopHolder represents a user with large token holdings.
type TopHolder struct {
	UserID   uuid.UUID       `json:"user_id"`
	Wallet   string          `json:"wallet"`
	Balance  decimal.Decimal `json:"balance"`
	ValueUSD decimal.Decimal `json:"value_usd"`
}

// GetNetworkAssetStats calculates global TVL and related metrics.
func (s *AssetService) GetNetworkAssetStats(ctx context.Context) (*NetworkAssetSummary, error) {
	// Sum all user balances * current price = TVL
	// This is an expensive query – in production, consider caching.
	users, err := s.queries.GetAllUsers(ctx)
	if err != nil {
		return nil, err
	}

	var tvl decimal.Decimal
	var holderCount int64
	for _, user := range users {
		balances, err := s.queries.GetUserBalances(ctx, user.ID)
		if err != nil {
			continue
		}
		hasPositive := false
		for _, b := range balances {
			if b.Balance.GreaterThan(decimal.Zero) {
				hasPositive = true
				tvl = tvl.Add(b.Balance.Mul(b.PriceUSD))
			}
		}
		if hasPositive {
			holderCount++
		}
	}

	activeTokens, err := s.queries.GetActiveTokensCount(ctx)
	if err != nil {
		activeTokens = 0
	}

	return &NetworkAssetSummary{
		TotalValueLockedUSD: tvl,
		TotalTokenHolders:   holderCount,
		ActiveTokens:        activeTokens,
	}, nil
}

// GetTopTokenHolders retrieves the top N holders for a specific token.
func (s *AssetService) GetTopTokenHolders(ctx context.Context, tokenSymbol string, limit int32) ([]TopHolder, error) {
	token, err := s.queries.GetTokenBySymbol(ctx, tokenSymbol)
	if err != nil {
		return nil, err
	}

	holders, err := s.queries.GetTopTokenHolders(ctx, db.GetTopTokenHoldersParams{
		TokenID: token.ID,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}

	result := make([]TopHolder, len(holders))
	for i, h := range holders {
		result[i] = TopHolder{
			UserID:   h.ID,
			Wallet:   h.WalletAddress,
			Balance:  h.Balance,
			ValueUSD: h.ValueUsd,
		}
	}
	return result, nil
}

// ---------------------------------------------------------------------
// Asset History (Placeholder)
// ---------------------------------------------------------------------

// AssetHistoryPoint represents a single point in a user's asset history.
type AssetHistoryPoint struct {
	Timestamp time.Time       `json:"timestamp"`
	TotalUSD  decimal.Decimal `json:"total_usd"`
}

// GetUserAssetHistory retrieves historical total asset values for a user.
// This would require a separate table to store snapshots.
func (s *AssetService) GetUserAssetHistory(ctx context.Context, userID uuid.UUID, days int) ([]AssetHistoryPoint, error) {
	// Placeholder – implement if daily snapshots are stored.
	return []AssetHistoryPoint{}, nil
}
