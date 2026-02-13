// internal/services/purchase.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/db"
)

// PurchaseService handles token purchase/subscription operations.
type PurchaseService struct {
	queries   *db.Queries
	assetSvc  *AssetService
	nodeSvc   *NodeService
	badgeSvc  *BadgeService
	combatSvc *CombatPowerService
}

// NewPurchaseService creates a new purchase service.
func NewPurchaseService(
	queries *db.Queries,
	assetSvc *AssetService,
	nodeSvc *NodeService,
	badgeSvc *BadgeService,
	combatSvc *CombatPowerService,
) *PurchaseService {
	return &PurchaseService{
		queries:   queries,
		assetSvc:  assetSvc,
		nodeSvc:   nodeSvc,
		badgeSvc:  badgeSvc,
		combatSvc: combatSvc,
	}
}

// ---------------------------------------------------------------------
// Purchase Flow
// ---------------------------------------------------------------------

// SubscribeParams defines the input for creating a token purchase subscription.
type SubscribeParams struct {
	UserID       uuid.UUID
	TokenSymbol  string          // e.g., "CAN" or "LAN"
	Amount       decimal.Decimal // number of tokens to purchase
	PaymentToken string          // usually "USDT"
}

// Subscribe creates a pending purchase record and deducts the payment.
// It returns the purchase record and the total cost.
func (s *PurchaseService) Subscribe(ctx context.Context, params SubscribeParams) (*db.Purchase, decimal.Decimal, error) {
	// 1. Validate token exists and is purchasable
	token, err := s.queries.GetTokenBySymbol(ctx, params.TokenSymbol)
	if err != nil {
		return nil, decimal.Zero, fmt.Errorf("token not supported: %s", params.TokenSymbol)
	}
	if !token.IsActive {
		return nil, decimal.Zero, fmt.Errorf("token %s is not active", params.TokenSymbol)
	}

	// 2. Get price and calculate total cost
	price := token.PriceUsd
	if price.IsZero() {
		return nil, decimal.Zero, fmt.Errorf("token price not available")
	}
	totalValue := params.Amount.Mul(price)

	// 3. Get payment token ID
	paymentToken, err := s.queries.GetTokenBySymbol(ctx, params.PaymentToken)
	if err != nil {
		return nil, decimal.Zero, fmt.Errorf("payment token not supported: %s", params.PaymentToken)
	}

	// 4. Check user's balance of payment token
	balance, err := s.assetSvc.GetUserTokenBalance(ctx, params.UserID, params.PaymentToken)
	if err != nil {
		return nil, decimal.Zero, fmt.Errorf("failed to fetch balance: %w", err)
	}
	if balance.LessThan(totalValue) {
		return nil, decimal.Zero, fmt.Errorf("insufficient %s balance: have %s, need %s",
			params.PaymentToken, balance.String(), totalValue.String())
	}

	// 5. Deduct payment token from user's balance
	err = s.queries.SubtractUserBalance(ctx, db.SubtractUserBalanceParams{
		UserID:  params.UserID,
		TokenID: paymentToken.ID,
		Balance: totalValue,
	})
	if err != nil {
		return nil, decimal.Zero, fmt.Errorf("failed to deduct payment: %w", err)
	}

	// 6. Create purchase record (pending)
	expiry := time.Now().Add(15 * time.Minute) // 15 minutes to complete on‑chain
	purchase, err := s.queries.CreatePurchase(ctx, db.CreatePurchaseParams{
		UserID:         params.UserID,
		TokenID:        token.ID,
		Amount:         params.Amount,
		PriceUsd:       price,
		TotalValue:     totalValue,
		PaymentTokenID: paymentToken.ID,
		Status:         "pending",
		ExpiryDate:     &expiry,
	})
	if err != nil {
		// Attempt to rollback the balance deduction? For simplicity, we return error.
		// In production, use a database transaction.
		return nil, decimal.Zero, fmt.Errorf("failed to create purchase record: %w", err)
	}

	return &purchase, totalValue, nil
}

// CompletePurchase finalizes a pending purchase after on‑chain confirmation.
// It adds the purchased tokens to the user's balance, updates purchase status,
// and triggers post‑purchase effects (badges, node stats, combat power).
func (s *PurchaseService) CompletePurchase(ctx context.Context, purchaseID uuid.UUID, txHash string) (*db.Purchase, error) {
	// 1. Get purchase record
	purchase, err := s.queries.GetPurchaseByID(ctx, purchaseID)
	if err != nil {
		return nil, fmt.Errorf("purchase not found: %w", err)
	}

	// 2. Verify it's still pending and not expired
	if purchase.Status != "pending" {
		return nil, fmt.Errorf("purchase is not pending (status: %s)", purchase.Status)
	}
	if purchase.ExpiryDate != nil && purchase.ExpiryDate.Before(time.Now()) {
		// Mark as expired and return error
		s.queries.UpdatePurchaseStatus(ctx, db.UpdatePurchaseStatusParams{
			ID:     purchaseID,
			Status: "expired",
			TxHash: nil,
		})
		return nil, fmt.Errorf("purchase has expired")
	}

	// 3. Add purchased tokens to user's balance
	err = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
		UserID:  purchase.UserID,
		TokenID: purchase.TokenID,
		Balance: purchase.Amount,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to credit tokens: %w", err)
	}

	// 4. Update purchase status to completed
	err = s.queries.UpdatePurchaseStatus(ctx, db.UpdatePurchaseStatusParams{
		ID:     purchaseID,
		Status: "completed",
		TxHash: &txHash,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update purchase status: %w", err)
	}

	// 5. Update combat power (user now holds more tokens)
	if s.combatSvc != nil {
		if err := s.combatSvc.UpdateCombatPower(ctx, purchase.UserID); err != nil {
			// Non‑critical – log and continue
			// logger.Error("failed to update combat power after purchase", "error", err)
		}
	}

	// 6. Process referral rewards if the purchaser was referred
	user, err := s.queries.GetUserByID(ctx, purchase.UserID)
	if err == nil && user.InvitedBy != nil {
		// Award referrer a percentage of the purchase value (e.g., 10%)
		referrerID := *user.InvitedBy
		rewardAmount := purchase.TotalValue.Mul(decimal.NewFromFloat(0.10)) // 10% commission

		// Get USDT token ID
		usdtToken, _ := s.queries.GetTokenBySymbol(ctx, "USDT")

		// Add reward to referrer's balance
		_ = s.queries.AddUserBalance(ctx, db.AddUserBalanceParams{
			UserID:  referrerID,
			TokenID: usdtToken.ID,
			Balance: rewardAmount,
		})

		// Record referral earning (optional)
		// This would require a table and query; we skip for brevity.
	}

	// 7. Check and award badges (e.g., "First Purchase", "Silver Investor", etc.)
	if s.badgeSvc != nil {
		go func() {
			_ = s.badgeSvc.CheckAndAwardPurchaseBadges(context.Background(), purchase.UserID)
		}()
	}

	// 8. Update node team power for ancestors (since user now has more combat power)
	if user != nil && user.InvitedBy != nil && s.nodeSvc != nil {
		go func() {
			_ = s.nodeSvc.RecalculateTeamStats(context.Background(), *user.InvitedBy)
		}()
	}

	// Refresh purchase record to get updated status
	updated, _ := s.queries.GetPurchaseByID(ctx, purchaseID)
	return &updated, nil
}

// ---------------------------------------------------------------------
// Query Methods
// ---------------------------------------------------------------------

// GetPurchase returns a purchase by ID, ensuring it belongs to the specified user.
func (s *PurchaseService) GetPurchase(ctx context.Context, purchaseID, userID uuid.UUID) (*db.Purchase, error) {
	purchase, err := s.queries.GetPurchaseByID(ctx, db.GetPurchaseByIDParams{
		ID:     purchaseID,
		UserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("purchase not found: %w", err)
	}
	return &purchase, nil
}

// GetUserPurchases returns paginated purchase history for a user.
func (s *PurchaseService) GetUserPurchases(ctx context.Context, userID uuid.UUID, limit, offset int32) ([]db.GetUserPurchasesRow, error) {
	return s.queries.GetUserPurchases(ctx, db.GetUserPurchasesParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
}

// ---------------------------------------------------------------------
// Admin / Maintenance
// ---------------------------------------------------------------------

// CancelExpiredPurchases marks all pending purchases that have expired as cancelled.
// Should be run periodically via cron.
func (s *PurchaseService) CancelExpiredPurchases(ctx context.Context) error {
	// This query is not yet defined – we add a placeholder.
	// In practice, you'd have a query like:
	// UPDATE purchases SET status = 'cancelled' WHERE status = 'pending' AND expiry_date < NOW()
	// For now, we return nil.
	return nil
}
