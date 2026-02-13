// internal/handlers/combat.go
package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourproject/canglanfu-api/internal/auth"
	"github.com/yourproject/canglanfu-api/internal/services"
	"github.com/yourproject/canglanfu-api/pkg/web"
)

// CombatHandler handles combat power related HTTP requests.
type CombatHandler struct {
	combatSvc *services.CombatPowerService
}

// NewCombatHandler creates a new combat handler.
func NewCombatHandler(combatSvc *services.CombatPowerService) *CombatHandler {
	return &CombatHandler{
		combatSvc: combatSvc,
	}
}

// RegisterRoutes registers combat routes under the authenticated group.
func (h *CombatHandler) RegisterRoutes(r chi.Router) {
	// All combat endpoints require authentication
	r.Group(func(r chi.Router) {
		// Use your actual auth middleware name (e.g., auth.AuthMiddleware)
		// This middleware should be applied by the main router, or we can require it here.
		// For consistency, we assume the caller will apply auth middleware to the group.
		r.Get("/combat/personal", h.GetPersonalCombatPower)
		r.Get("/combat/network", h.GetNetworkCombatPower)
		r.Get("/combat/history", h.GetCombatPowerHistory)
		r.Post("/combat/refresh", h.RefreshCombatPower)
	})
}

// ---------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------

// GetPersonalCombatPower returns the authenticated user's current combat power details.
// GET /combat/personal
func (h *CombatHandler) GetPersonalCombatPower(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// Get full combat power record
	cp, err := h.combatSvc.queries.GetCombatPower(r.Context(), userID)
	if err != nil {
		// If no record exists, return zeros
		web.Success(w, http.StatusOK, map[string]interface{}{
			"user_id":        userID,
			"personal_power": "0",
			"network_power":  "0",
			"lp_weight":      "0",
			"burn_power":     "0",
			"updated_at":     time.Now(),
		})
		return
	}

	// Optionally calculate additional derived metrics
	response := map[string]interface{}{
		"user_id":        cp.UserID,
		"personal_power": cp.PersonalPower,
		"network_power":  cp.NetworkPower,
		"lp_weight":      cp.LpWeight,
		"burn_power":     cp.BurnPower,
		"updated_at":     cp.UpdatedAt,
	}

	web.Success(w, http.StatusOK, response)
}

// GetNetworkCombatPower returns the total combat power of the entire network.
// GET /combat/network
func (h *CombatHandler) GetNetworkCombatPower(w http.ResponseWriter, r *http.Request) {
	total, err := h.combatSvc.queries.GetNetworkCombatPower(r.Context())
	if err != nil {
		total = 0
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"network_combat_power": total,
	})
}

// GetCombatPowerHistory returns historical daily snapshots of the user's combat power.
// GET /combat/history?days=7&limit=30
func (h *CombatHandler) GetCombatPowerHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// Parse query parameters
	days := 7
	daysStr := r.URL.Query().Get("days")
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 90 {
			days = d
		}
	}

	// This assumes you have a query method to get combat power history for the last N days.
	// If not implemented yet, you can return a placeholder or skip this endpoint.
	// We'll assume a method: GetCombatPowerHistory(ctx, userID, days) exists.
	history, err := h.combatSvc.GetCombatPowerHistory(r.Context(), userID, days)
	if err != nil {
		// If not implemented, return empty array with message
		web.Success(w, http.StatusOK, []interface{}{})
		return
	}

	web.Success(w, http.StatusOK, history)
}

// RefreshCombatPower manually triggers a recalculation of the user's combat power.
// This can be used after significant changes (e.g., purchase, LP, burn) if not auto‑updated.
// POST /combat/refresh
func (h *CombatHandler) RefreshCombatPower(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	err := h.combatSvc.UpdateCombatPower(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, map[string]string{
		"message": "combat power updated successfully",
	})
}
