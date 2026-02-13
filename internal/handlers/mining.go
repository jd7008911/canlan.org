// internal/handlers/mining.go
package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"jd7008911/canlan.org/internal/auth"
	"jd7008911/canlan.org/internal/services"
	"jd7008911/canlan.org/pkg/web"
)

// MiningHandler handles mining machine and earnings related HTTP requests.
type MiningHandler struct {
	miningSvc *services.MiningService
}

// NewMiningHandler creates a new mining handler.
func NewMiningHandler(miningSvc *services.MiningService) *MiningHandler {
	return &MiningHandler{
		miningSvc: miningSvc,
	}
}

// RegisterRoutes registers mining routes under the authenticated group.
func (h *MiningHandler) RegisterRoutes(r chi.Router) {
	// All mining endpoints require authentication
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware) // Adjust middleware name as needed

		// Mining machine endpoints
		r.Get("/mining/machine", h.GetMiningMachine)
		r.Post("/mining/upgrade", h.UpgradeMachine)

		// Earnings endpoints
		r.Get("/mining/earnings/today", h.GetTodayEarnings)
		r.Get("/mining/earnings/history", h.GetEarningsHistory)
		r.Post("/mining/earnings/accrue", h.AccrueDailyEarnings) // Manual trigger (maybe admin only)

		// Statistics
		r.Get("/mining/stats", h.GetMiningStats)
	})
}

// ---------------------------------------------------------------------
// Mining Machine Handlers
// ---------------------------------------------------------------------

// GetMiningMachine returns the authenticated user's mining machine details.
// GET /mining/machine
func (h *MiningHandler) GetMiningMachine(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	machine, err := h.miningSvc.GetUserMiningMachine(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, machine)
}

// UpgradeMachine upgrades the user's mining machine to the next level.
// POST /mining/upgrade
func (h *MiningHandler) UpgradeMachine(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	machine, err := h.miningSvc.UpgradeMachine(r.Context(), userID)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, machine)
}

// ---------------------------------------------------------------------
// Earnings Handlers
// ---------------------------------------------------------------------

// GetTodayEarnings returns the user's mining earnings for today.
// GET /mining/earnings/today
func (h *MiningHandler) GetTodayEarnings(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// First try to get already accrued earnings from database
	earnings, err := h.miningSvc.GetTodayMiningEarnings(r.Context(), userID)
	if err != nil || earnings == nil {
		// If not yet accrued, calculate projection
		projection, err := h.miningSvc.CalculateDailyEarnings(r.Context(), userID)
		if err != nil {
			web.InternalError(w, err)
			return
		}
		web.Success(w, http.StatusOK, map[string]interface{}{
			"projected": true,
			"earnings":  projection,
		})
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"projected": false,
		"earnings":  earnings,
	})
}

// GetEarningsHistory returns paginated historical mining earnings.
// GET /mining/earnings/history?limit=20&offset=0&days=30
func (h *MiningHandler) GetEarningsHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// Parse pagination
	limit, offset := web.ParsePagination(r)

	// Optional days filter
	days := 30
	if daysStr := r.URL.Query().Get("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	// This assumes the mining service has a method to get earnings history.
	// We'll call it with userID, limit, offset, and days.
	history, err := h.miningSvc.GetEarningsHistory(r.Context(), userID, int32(limit), int32(offset), days)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Get total count for pagination
	total, err := h.miningSvc.CountEarningsHistory(r.Context(), userID, days)
	if err != nil {
		total = 0
	}

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, history, meta)
}

// AccrueDailyEarnings manually triggers daily earnings accrual for the user.
// POST /mining/earnings/accrue
func (h *MiningHandler) AccrueDailyEarnings(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	earnings, err := h.miningSvc.AccrueDailyEarnings(r.Context(), userID)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"message":  "Daily earnings accrued successfully",
		"earnings": earnings,
	})
}

// ---------------------------------------------------------------------
// Statistics Handlers
// ---------------------------------------------------------------------

// GetMiningStats returns comprehensive mining statistics for the user.
// GET /mining/stats
func (h *MiningHandler) GetMiningStats(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	stats, err := h.miningSvc.GetUserMiningStats(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, stats)
}
