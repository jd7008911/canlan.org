// internal/services/withdrawal.go
package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/db"
)

// WithdrawalService handles all withdrawal-related business logic.
type WithdrawalService struct {
	queries  *db.Queries
	assetSvc *AssetService
}

// NewWithdrawalService creates a new withdrawal service.
func NewWithdrawalService(queries *db.Queries, assetSvc *AssetService) *WithdrawalService {
	return &WithdrawalService{
		queries:  queries,
		assetSvc: assetSvc,
	}
}

// ---------------------------------------------------------------------
// Withdrawal Request
// ---------------------------------------------------------------------

// WithdrawalRequestParams defines the input for creating a withdrawal request.
type WithdrawalRequestParams struct {
	UserID      uuid.UUID
	TokenSymbol string
	Amount      decimal.Decimal
}

// CreateWithdrawalRequest creates a new pending withdrawal after validating balance and limits.
func (s *WithdrawalService) CreateWithdrawalRequest(ctx context.Context, params WithdrawalRequestParams) (*db.Withdrawal, error) {
	// 1. Validate token exists
	token, err := s.queries.GetTokenBySymbol(ctx, params.TokenSymbol)
	if err != nil {
		return nil, fmt.Errorf("token not supported: %s", params.TokenSymbol)
	}

	// 2. Check user's balance
	balance, err := s.assetSvc.GetUserTokenBalance(ctx, params.UserID, params.TokenSymbol)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balance: %w", err)
	}
	if balance.LessThan(params.Amount) {
		return nil, fmt.Errorf("insufficient %s balance: have %s, need %s",
			params.TokenSymbol, balance.String(), params.Amount.String())
	}

	// 3. Ensure withdrawal limits are up‑to‑date (reset if needed)
	if err := s.resetLimitsIfNeeded(ctx, params.UserID); err != nil {
		return nil, fmt.Errorf("failed to update limits: %w", err)
	}

	// 4. Check withdrawal limits
	ok, err := s.queries.CheckWithdrawalLimit(ctx, db.CheckWithdrawalLimitParams{
		UserID: params.UserID,
		Amount: params.Amount,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to check withdrawal limit: %w", err)
	}
	if !ok {
		// Get remaining limits for better error message
		limits, _ := s.queries.GetRemainingWithdrawalLimits(ctx, params.UserID)
		return nil, fmt.Errorf("withdrawal exceeds daily/monthly limit (daily left: %s, monthly left: %s)",
			limits.RemainingDaily.String(), limits.RemainingMonthly.String())
	}

	// 5. Deduct balance immediately (if you want to lock funds; alternatively, deduct on approval)
	err = s.queries.SubtractUserBalance(ctx, db.SubtractUserBalanceParams{
		UserID:  params.UserID,
		TokenID: token.ID,
		Balance: params.Amount,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to deduct balance: %w", err)
	}

	// 6. Create withdrawal request
	withdrawal, err := s.queries.CreateWithdrawal(ctx, db.CreateWithdrawalParams{
		UserID:  params.UserID,
		TokenID: token.ID,
		Amount:  params.Amount,
	})
	if err != nil {
		// Attempt to rollback the balance deduction
		_ = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
			UserID:  params.UserID,
			TokenID: token.ID,
			Balance: params.Amount,
		})
		return nil, fmt.Errorf("failed to create withdrawal request: %w", err)
	}

	// 7. Increment usage counters (daily/monthly)
	_ = s.queries.IncrementWithdrawalUsage(ctx, db.IncrementWithdrawalUsageParams{
		UserID: params.UserID,
		Amount: params.Amount,
	})

	return &withdrawal, nil
}

// ---------------------------------------------------------------------
// Withdrawal Processing (Admin)
// ---------------------------------------------------------------------

// ApproveWithdrawal marks a pending withdrawal as completed and records the transaction hash.
func (s *WithdrawalService) ApproveWithdrawal(ctx context.Context, withdrawalID uuid.UUID, txHash string) error {
	withdrawal, err := s.queries.GetWithdrawalByID(ctx, withdrawalID)
	if err != nil {
		return fmt.Errorf("withdrawal not found: %w", err)
	}
	if withdrawal.Status != "pending" {
		return fmt.Errorf("withdrawal is not pending (status: %s)", withdrawal.Status)
	}

	// Update status to completed
	err = s.queries.UpdateWithdrawalStatus(ctx, db.UpdateWithdrawalStatusParams{
		ID:     withdrawalID,
		Status: "completed",
		TxHash: &txHash,
	})
	if err != nil {
		return fmt.Errorf("failed to update withdrawal status: %w", err)
	}

	return nil
}

// RejectWithdrawal cancels a pending withdrawal and refunds the user's balance.
func (s *WithdrawalService) RejectWithdrawal(ctx context.Context, withdrawalID uuid.UUID, reason string) error {
	withdrawal, err := s.queries.GetWithdrawalByID(ctx, withdrawalID)
	if err != nil {
		return fmt.Errorf("withdrawal not found: %w", err)
	}
	if withdrawal.Status != "pending" {
		return fmt.Errorf("withdrawal is not pending (status: %s)", withdrawal.Status)
	}

	// Refund the user's balance
	err = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
		UserID:  withdrawal.UserID,
		TokenID: withdrawal.TokenID,
		Balance: withdrawal.Amount,
	})
	if err != nil {
		return fmt.Errorf("failed to refund balance: %w", err)
	}

	// Update status to cancelled
	err = s.queries.CancelWithdrawal(ctx, db.CancelWithdrawalParams{
		ID:     withdrawalID,
		UserID: withdrawal.UserID,
	})
	if err != nil {
		return fmt.Errorf("failed to cancel withdrawal: %w", err)
	}

	// Decrement usage counters (since we already incremented on creation)
	// This requires a negative increment query – we'll just not increment on rejection.
	// In a real system, you might store the usage increment and roll it back.

	return nil
}

// ---------------------------------------------------------------------
// Withdrawal Queries
// ---------------------------------------------------------------------

// GetUserWithdrawals returns paginated withdrawal history for a user.
func (s *WithdrawalService) GetUserWithdrawals(ctx context.Context, userID uuid.UUID, limit, offset int32) ([]db.GetUserWithdrawalsRow, error) {
	return s.queries.GetUserWithdrawals(ctx, db.GetUserWithdrawalsParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
}

// GetUserWithdrawalsByStatus returns withdrawals filtered by status.
func (s *WithdrawalService) GetUserWithdrawalsByStatus(ctx context.Context, userID uuid.UUID, status string, limit, offset int32) ([]db.GetUserWithdrawalsByStatusRow, error) {
	return s.queries.GetUserWithdrawalsByStatus(ctx, db.GetUserWithdrawalsByStatusParams{
		UserID: userID,
		Status: status,
		Limit:  limit,
		Offset: offset,
	})
}

// ListPendingWithdrawals returns all pending withdrawals (admin view).
func (s *WithdrawalService) ListPendingWithdrawals(ctx context.Context, limit, offset int32) ([]db.ListPendingWithdrawalsRow, error) {
	return s.queries.ListPendingWithdrawals(ctx, db.ListPendingWithdrawalsParams{
		Limit:  limit,
		Offset: offset,
	})
}

// ---------------------------------------------------------------------
// Withdrawal Limits
// ---------------------------------------------------------------------

// GetWithdrawalLimits returns the user's current daily/monthly limits and usage.
func (s *WithdrawalService) GetWithdrawalLimits(ctx context.Context, userID uuid.UUID) (*db.GetRemainingWithdrawalLimitsRow, error) {
	// Ensure limits exist (create default if not)
	_, err := s.queries.GetWithdrawalLimits(ctx, userID)
	if err != nil {
		// Create default limits
		_, err = s.queries.CreateWithdrawalLimits(ctx, db.CreateWithdrawalLimitsParams{
			UserID:       userID,
			DailyLimit:   decimal.NewFromInt(1000),
			MonthlyLimit: decimal.NewFromInt(30000),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create withdrawal limits: %w", err)
		}
	}

	// Reset if needed
	s.resetLimitsIfNeeded(ctx, userID)

	// Get remaining limits
	limits, err := s.queries.GetRemainingWithdrawalLimits(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch limits: %w", err)
	}
	return &limits, nil
}

// UpdateWithdrawalLimits allows admin to change a user's withdrawal limits.
func (s *WithdrawalService) UpdateWithdrawalLimits(ctx context.Context, userID uuid.UUID, dailyLimit, monthlyLimit decimal.Decimal) error {
	return s.queries.UpdateWithdrawalLimits(ctx, db.UpdateWithdrawalLimitsParams{
		UserID:       userID,
		DailyLimit:   dailyLimit,
		MonthlyLimit: monthlyLimit,
	})
}

// resetLimitsIfNeeded checks and resets daily/monthly counters if the period has rolled over.
func (s *WithdrawalService) resetLimitsIfNeeded(ctx context.Context, userID uuid.UUID) error {
	// Reset daily if last reset was before today
	err := s.queries.ResetDailyWithdrawalLimit(ctx, userID)
	if err != nil {
		return err
	}
	// Reset monthly if last reset was before this month
	err = s.queries.ResetMonthlyWithdrawalLimit(ctx, userID)
	if err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------

// WithdrawalStatsSummary contains user-level withdrawal statistics.
type WithdrawalStatsSummary struct {
	TotalWithdrawals int64                             `json:"total_withdrawals"`
	TotalAmount      decimal.Decimal                   `json:"total_amount"`
	PendingCount     int64                             `json:"pending_count"`
	CompletedCount   int64                             `json:"completed_count"`
	CancelledCount   int64                             `json:"cancelled_count"`
	FailedCount      int64                             `json:"failed_count"`
	StatsByToken     []db.GetWithdrawalStatsByTokenRow `json:"stats_by_token"`
}

// GetUserWithdrawalSummary returns aggregated withdrawal statistics for a user.
func (s *WithdrawalService) GetUserWithdrawalSummary(ctx context.Context, userID uuid.UUID) (*WithdrawalStatsSummary, error) {
	summary, err := s.queries.GetUserWithdrawalSummary(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch withdrawal summary: %w", err)
	}

	statsByToken, err := s.queries.GetWithdrawalStatsByToken(ctx, userID)
	if err != nil {
		statsByToken = []db.GetWithdrawalStatsByTokenRow{}
	}

	return &WithdrawalStatsSummary{
		TotalWithdrawals: summary.TotalWithdrawals,
		TotalAmount:      summary.TotalAmountWithdrawn,
		PendingCount:     summary.PendingCount,
		CompletedCount:   summary.CompletedCount,
		CancelledCount:   summary.CancelledCount,
		FailedCount:      summary.FailedCount,
		StatsByToken:     statsByToken,
	}, nil
}

// ---------------------------------------------------------------------
// Maintenance
// ---------------------------------------------------------------------

// CleanupOldWithdrawals removes withdrawal records older than 90 days (configurable).
func (s *WithdrawalService) CleanupOldWithdrawals(ctx context.Context) error {
	return s.queries.DeleteOldWithdrawals(ctx)
}

// ResetAllDailyLimits can be run as a cron job to reset daily limits for all users.
func (s *WithdrawalService) ResetAllDailyLimits(ctx context.Context) error {
	return s.queries.ResetAllDailyLimits(ctx)
}

// ResetAllMonthlyLimits can be run as a cron job to reset monthly limits for all users.
func (s *WithdrawalService) ResetAllMonthlyLimits(ctx context.Context) error {
	return s.queries.ResetAllMonthlyLimits(ctx)
}
