// internal/handlers/withdrawal.go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"jd7008911/canlan.org/internal/auth"
	"jd7008911/canlan.org/internal/services"
	"jd7008911/canlan.org/pkg/web"
)

// WithdrawalHandler handles withdrawal-related HTTP requests.
type WithdrawalHandler struct {
	withdrawalSvc *services.WithdrawalService
}

// NewWithdrawalHandler creates a new withdrawal handler.
func NewWithdrawalHandler(withdrawalSvc *services.WithdrawalService) *WithdrawalHandler {
	return &WithdrawalHandler{
		withdrawalSvc: withdrawalSvc,
	}
}

// RegisterRoutes registers withdrawal routes.
func (h *WithdrawalHandler) RegisterRoutes(r chi.Router) {
	// User routes - require authentication
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware)

		r.Post("/withdrawals", h.CreateWithdrawal)
		r.Get("/withdrawals", h.GetUserWithdrawals)
		r.Get("/withdrawals/{id}", h.GetWithdrawal)
		r.Post("/withdrawals/{id}/cancel", h.CancelWithdrawal)
		r.Get("/withdrawals/limits", h.GetWithdrawalLimits)
	})

	// Admin routes - require admin role (can be in a separate admin router)
	// Uncomment and adjust as needed
	/*
	   r.Group(func(r chi.Router) {
	       r.Use(auth.AuthMiddleware)
	       r.Use(auth.RequireRole("admin"))
	       r.Get("/admin/withdrawals/pending", h.ListPendingWithdrawals)
	       r.Post("/admin/withdrawals/{id}/approve", h.ApproveWithdrawal)
	       r.Post("/admin/withdrawals/{id}/reject", h.RejectWithdrawal)
	   })
	*/
}

// ---------------------------------------------------------------------
// User Handlers
// ---------------------------------------------------------------------

// CreateWithdrawal creates a new withdrawal request.
// POST /withdrawals
// Request body: { "token_symbol": "USDT", "amount": "100.0" }
func (h *WithdrawalHandler) CreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	var req struct {
		TokenSymbol string          `json:"token_symbol" validate:"required"`
		Amount      decimal.Decimal `json:"amount" validate:"required,gt=0"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Amount.LessThanOrEqual(decimal.Zero) {
		web.Error(w, http.StatusBadRequest, "amount must be positive")
		return
	}

	params := services.WithdrawalRequestParams{
		UserID:      userID,
		TokenSymbol: req.TokenSymbol,
		Amount:      req.Amount,
	}

	withdrawal, err := h.withdrawalSvc.CreateWithdrawalRequest(r.Context(), params)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusCreated, withdrawal)
}

// GetUserWithdrawals returns paginated withdrawal history for the authenticated user.
// GET /withdrawals?limit=20&offset=0
func (h *WithdrawalHandler) GetUserWithdrawals(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limit, offset := web.ParsePagination(r)

	withdrawals, err := h.withdrawalSvc.GetUserWithdrawals(r.Context(), userID, int32(limit), int32(offset))
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Get total count for pagination
	total, err := h.withdrawalSvc.CountUserWithdrawals(r.Context(), userID)
	if err != nil {
		total = 0
	}

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, withdrawals, meta)
}

// GetWithdrawal returns a specific withdrawal by ID.
// GET /withdrawals/{id}
func (h *WithdrawalHandler) GetWithdrawal(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	withdrawalIDStr := chi.URLParam(r, "id")
	withdrawalID, err := uuid.Parse(withdrawalIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid withdrawal ID")
		return
	}

	// Service should verify ownership
	withdrawal, err := h.withdrawalSvc.GetUserWithdrawalByID(r.Context(), withdrawalID, userID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "withdrawal not found")
		return
	}

	web.Success(w, http.StatusOK, withdrawal)
}

// CancelWithdrawal cancels a pending withdrawal.
// POST /withdrawals/{id}/cancel
func (h *WithdrawalHandler) CancelWithdrawal(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	withdrawalIDStr := chi.URLParam(r, "id")
	withdrawalID, err := uuid.Parse(withdrawalIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid withdrawal ID")
		return
	}

	err = h.withdrawalSvc.CancelWithdrawal(r.Context(), withdrawalID, userID)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]string{
		"message": "withdrawal cancelled successfully",
	})
}

// GetWithdrawalLimits returns the user's current withdrawal limits.
// GET /withdrawals/limits
func (h *WithdrawalHandler) GetWithdrawalLimits(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limits, err := h.withdrawalSvc.GetWithdrawalLimits(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, limits)
}

// ---------------------------------------------------------------------
// Admin Handlers (optional)
// ---------------------------------------------------------------------

// ListPendingWithdrawals returns all pending withdrawals (admin only).
// GET /admin/withdrawals/pending?limit=20&offset=0
func (h *WithdrawalHandler) ListPendingWithdrawals(w http.ResponseWriter, r *http.Request) {
	limit, offset := web.ParsePagination(r)

	withdrawals, err := h.withdrawalSvc.ListPendingWithdrawals(r.Context(), int32(limit), int32(offset))
	if err != nil {
		web.InternalError(w, err)
		return
	}

	total, err := h.withdrawalSvc.CountPendingWithdrawals(r.Context())
	if err != nil {
		total = 0
	}

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, withdrawals, meta)
}

// ApproveWithdrawal marks a withdrawal as completed.
// POST /admin/withdrawals/{id}/approve
// Request body: { "tx_hash": "0x..." }
func (h *WithdrawalHandler) ApproveWithdrawal(w http.ResponseWriter, r *http.Request) {
	withdrawalIDStr := chi.URLParam(r, "id")
	withdrawalID, err := uuid.Parse(withdrawalIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid withdrawal ID")
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

	err = h.withdrawalSvc.ApproveWithdrawal(r.Context(), withdrawalID, req.TxHash)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]string{
		"message": "withdrawal approved successfully",
	})
}

// RejectWithdrawal cancels a withdrawal and refunds the user.
// POST /admin/withdrawals/{id}/reject
func (h *WithdrawalHandler) RejectWithdrawal(w http.ResponseWriter, r *http.Request) {
	withdrawalIDStr := chi.URLParam(r, "id")
	withdrawalID, err := uuid.Parse(withdrawalIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid withdrawal ID")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req) // ignore error, reason is optional

	err = h.withdrawalSvc.RejectWithdrawal(r.Context(), withdrawalID, req.Reason)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]string{
		"message": "withdrawal rejected and refunded",
	})
}
