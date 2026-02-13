// internal/handlers/swap.go
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

// SwapHandler handles token swap HTTP requests.
type SwapHandler struct {
	swapSvc *services.SwapService
}

// NewSwapHandler creates a new swap handler.
func NewSwapHandler(swapSvc *services.SwapService) *SwapHandler {
	return &SwapHandler{
		swapSvc: swapSvc,
	}
}

// RegisterRoutes registers swap routes.
func (h *SwapHandler) RegisterRoutes(r chi.Router) {
	// Public routes
	r.Get("/swaps/rate", h.GetSwapRate)

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware)
		r.Post("/swaps/execute", h.ExecuteSwap)
		r.Get("/swaps", h.GetUserSwaps)
		r.Get("/swaps/{id}", h.GetSwap)
		r.Get("/swaps/stats", h.GetSwapStats)
	})
}

// ---------------------------------------------------------------------
// Public Handlers
// ---------------------------------------------------------------------

// GetSwapRate returns the current conversion rate between two tokens.
// GET /swaps/rate?from=USDT&to=CAN
func (h *SwapHandler) GetSwapRate(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		web.Error(w, http.StatusBadRequest, "from and to token symbols are required")
		return
	}

	rate, err := h.swapSvc.GetSwapRate(r.Context(), from, to)
	if err != nil {
		web.Error(w, http.StatusNotFound, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"from": from,
		"to":   to,
		"rate": rate,
	})
}

// ---------------------------------------------------------------------
// Authenticated Handlers
// ---------------------------------------------------------------------

// ExecuteSwap performs a token swap.
// POST /swaps/execute
// Request body: { "from_token": "USDT", "to_token": "CAN", "amount": "100.0", "tx_hash": "0x..." }
func (h *SwapHandler) ExecuteSwap(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	var req struct {
		FromToken string          `json:"from_token" validate:"required"`
		ToToken   string          `json:"to_token" validate:"required"`
		Amount    decimal.Decimal `json:"amount" validate:"required,gt=0"`
		TxHash    string          `json:"tx_hash" validate:"required"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Amount.LessThanOrEqual(decimal.Zero) {
		web.Error(w, http.StatusBadRequest, "amount must be positive")
		return
	}
	if req.TxHash == "" {
		web.Error(w, http.StatusBadRequest, "tx_hash is required")
		return
	}

	params := services.SwapParams{
		UserID:    userID,
		FromToken: req.FromToken,
		ToToken:   req.ToToken,
		Amount:    req.Amount,
		TxHash:    req.TxHash,
	}

	result, err := h.swapSvc.ExecuteSwap(r.Context(), params)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, result)
}

// GetUserSwaps returns paginated swap history for the authenticated user.
// GET /swaps?limit=20&offset=0
func (h *SwapHandler) GetUserSwaps(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limit, offset := web.ParsePagination(r)

	swaps, err := h.swapSvc.GetUserSwaps(r.Context(), userID, int32(limit), int32(offset))
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Get total count for pagination (you may need to add this method)
	total, _ := h.swapSvc.CountUserSwaps(r.Context(), userID)

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, swaps, meta)
}

// GetSwap returns a specific swap by ID.
// GET /swaps/{id}
func (h *SwapHandler) GetSwap(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	swapIDStr := chi.URLParam(r, "id")
	swapID, err := uuid.Parse(swapIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid swap ID")
		return
	}

	// You need to implement GetSwapByID in the service with ownership check
	swap, err := h.swapSvc.GetSwapByID(r.Context(), swapID, userID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "swap not found")
		return
	}

	web.Success(w, http.StatusOK, swap)
}

// GetSwapStats returns swap statistics for the authenticated user.
// GET /swaps/stats
func (h *SwapHandler) GetSwapStats(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// You may want to get user-specific stats (total swapped, volume, etc.)
	// For now, we can return global stats or a placeholder.
	// We'll assume the service has a method for user swap summary.
	stats, err := h.swapSvc.GetUserSwapSummary(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, stats)
}
