// internal/handlers/purchase.go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/auth"
	"github.com/yourproject/canglanfu-api/internal/services"
	"github.com/yourproject/canglanfu-api/pkg/web"
)

// PurchaseHandler handles token purchase/subscription HTTP requests.
type PurchaseHandler struct {
	purchaseSvc *services.PurchaseService
}

// NewPurchaseHandler creates a new purchase handler.
func NewPurchaseHandler(purchaseSvc *services.PurchaseService) *PurchaseHandler {
	return &PurchaseHandler{
		purchaseSvc: purchaseSvc,
	}
}

// RegisterRoutes registers purchase routes under the authenticated group.
func (h *PurchaseHandler) RegisterRoutes(r chi.Router) {
	// All purchase endpoints require authentication
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware) // Adjust middleware name as needed

		r.Post("/purchases/subscribe", h.Subscribe)
		r.Get("/purchases", h.GetUserPurchases)
		r.Get("/purchases/{id}", h.GetPurchase)
		r.Post("/purchases/{id}/complete", h.CompletePurchase)
		r.Post("/purchases/{id}/cancel", h.CancelPurchase) // Optional
	})
}

// ---------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------

// Subscribe creates a new pending token purchase subscription.
// POST /purchases/subscribe
// Request body: { "token_symbol": "CAN", "amount": "100.0", "payment_token": "USDT" }
func (h *PurchaseHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	var req struct {
		TokenSymbol  string          `json:"token_symbol" validate:"required"`
		Amount       decimal.Decimal `json:"amount" validate:"required,gt=0"`
		PaymentToken string          `json:"payment_token" validate:"required"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Basic validation
	if req.Amount.LessThanOrEqual(decimal.Zero) {
		web.Error(w, http.StatusBadRequest, "amount must be positive")
		return
	}

	params := services.SubscribeParams{
		UserID:       userID,
		TokenSymbol:  req.TokenSymbol,
		Amount:       req.Amount,
		PaymentToken: req.PaymentToken,
	}

	purchase, totalValue, err := h.purchaseSvc.Subscribe(r.Context(), params)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusCreated, map[string]interface{}{
		"purchase":    purchase,
		"total_value": totalValue,
		"status":      "pending",
		"expiry":      purchase.ExpiryDate,
	})
}

// GetUserPurchases returns paginated purchase history for the authenticated user.
// GET /purchases?limit=20&offset=0
func (h *PurchaseHandler) GetUserPurchases(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limit, offset := web.ParsePagination(r)

	purchases, err := h.purchaseSvc.GetUserPurchases(r.Context(), userID, int32(limit), int32(offset))
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Get total count for pagination (you may need to add this method to service)
	total, _ := h.purchaseSvc.CountUserPurchases(r.Context(), userID)

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, purchases, meta)
}

// GetPurchase returns a specific purchase by ID.
// GET /purchases/{id}
func (h *PurchaseHandler) GetPurchase(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	purchaseIDStr := chi.URLParam(r, "id")
	purchaseID, err := uuid.Parse(purchaseIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid purchase ID")
		return
	}

	purchase, err := h.purchaseSvc.GetPurchase(r.Context(), purchaseID, userID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "purchase not found")
		return
	}

	web.Success(w, http.StatusOK, purchase)
}

// CompletePurchase finalizes a pending purchase after on-chain confirmation.
// POST /purchases/{id}/complete
// Request body: { "tx_hash": "0x..." }
func (h *PurchaseHandler) CompletePurchase(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	purchaseIDStr := chi.URLParam(r, "id")
	purchaseID, err := uuid.Parse(purchaseIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid purchase ID")
		return
	}

	var req struct {
		TxHash string `json:"tx_hash" validate:"required"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TxHash == "" {
		web.Error(w, http.StatusBadRequest, "tx_hash is required")
		return
	}

	// Verify ownership before completing
	_, err = h.purchaseSvc.GetPurchase(r.Context(), purchaseID, userID)
	if err != nil {
		web.Error(w, http.StatusForbidden, "purchase does not belong to you")
		return
	}

	purchase, err := h.purchaseSvc.CompletePurchase(r.Context(), purchaseID, req.TxHash)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"message":  "Purchase completed successfully",
		"purchase": purchase,
	})
}

// CancelPurchase cancels a pending purchase (optional).
// POST /purchases/{id}/cancel
func (h *PurchaseHandler) CancelPurchase(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	purchaseIDStr := chi.URLParam(r, "id")
	purchaseID, err := uuid.Parse(purchaseIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid purchase ID")
		return
	}

	// Verify ownership
	_, err = h.purchaseSvc.GetPurchase(r.Context(), purchaseID, userID)
	if err != nil {
		web.Error(w, http.StatusForbidden, "purchase does not belong to you")
		return
	}

	// This method should refund the payment and set status to 'cancelled'
	// You need to implement CancelPurchase in the service.
	err = h.purchaseSvc.CancelPurchase(r.Context(), purchaseID, userID)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]string{
		"message": "Purchase cancelled successfully",
	})
}
