// internal/handlers/badge.go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourproject/canglanfu-api/internal/auth"
	"github.com/yourproject/canglanfu-api/internal/services"
	"github.com/yourproject/canglanfu-api/pkg/web"
)

// BadgeHandler handles badge-related HTTP requests.
type BadgeHandler struct {
	badgeSvc *services.BadgeService
}

// NewBadgeHandler creates a new badge handler.
func NewBadgeHandler(badgeSvc *services.BadgeService) *BadgeHandler {
	return &BadgeHandler{
		badgeSvc: badgeSvc,
	}
}

// RegisterRoutes registers badge routes under the authenticated group.
func (h *BadgeHandler) RegisterRoutes(r chi.Router) {
	// Public routes (or optional auth)
	r.Get("/badges", h.ListAvailableBadges)
	r.Get("/badges/{id}", h.GetBadgeDetails)
	r.Get("/badges/stats/network", h.GetNetworkBadgeStats)

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware) // Assuming auth middleware is available via context or passed
		r.Get("/badges/user", h.GetUserBadges)
		r.Get("/badges/user/active", h.GetUserActiveBadges)
		r.Post("/badges/purchase", h.PurchaseBadge)
		r.Get("/badges/records", h.GetBadgeRecords) // could be network badge records
	})
}

// ListAvailableBadges returns all badges available for purchase.
// GET /badges
func (h *BadgeHandler) ListAvailableBadges(w http.ResponseWriter, r *http.Request) {
	badges, err := h.badgeSvc.ListAvailableBadges(r.Context())
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, badges)
}

// GetBadgeDetails returns information about a specific badge.
// GET /badges/{id}
func (h *BadgeHandler) GetBadgeDetails(w http.ResponseWriter, r *http.Request) {
	badgeIDStr := chi.URLParam(r, "id")
	badgeID, err := uuid.Parse(badgeIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid badge ID")
		return
	}

	badge, err := h.badgeSvc.GetBadgeDetails(r.Context(), badgeID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "badge not found")
		return
	}
	web.Success(w, http.StatusOK, badge)
}

// GetUserBadges returns all badges (active and inactive) owned by the authenticated user.
// GET /badges/user
func (h *BadgeHandler) GetUserBadges(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	badges, err := h.badgeSvc.GetUserBadges(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, badges)
}

// GetUserActiveBadges returns only currently active badges for the authenticated user.
// GET /badges/user/active
func (h *BadgeHandler) GetUserActiveBadges(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	badges, err := h.badgeSvc.GetUserActiveBadges(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, badges)
}

// PurchaseBadge handles badge purchase requests.
// POST /badges/purchase
// Request body: { "badge_id": "uuid" }
func (h *BadgeHandler) PurchaseBadge(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	var req struct {
		BadgeID uuid.UUID `json:"badge_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.BadgeID == uuid.Nil {
		web.Error(w, http.StatusBadRequest, "badge_id is required")
		return
	}

	userBadge, err := h.badgeSvc.PurchaseBadge(r.Context(), userID, req.BadgeID)
	if err != nil {
		// Handle specific error types if needed
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusCreated, userBadge)
}

// GetBadgeRecords returns network-wide badge records (badge direct list).
// This is a network statistics endpoint.
// GET /badges/records?limit=10&offset=0
func (h *BadgeHandler) GetBadgeRecords(w http.ResponseWriter, r *http.Request) {
	// Parse pagination
	limit, offset := web.ParsePagination(r)

	records, err := h.badgeSvc.GetBadgeDirectList(r.Context(), int32(limit), int32(offset))
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Get total count for pagination metadata
	total, err := h.badgeSvc.GetTotalBadgesInNetwork(r.Context())
	if err != nil {
		total = 0
	}

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, records, meta)
}

// GetNetworkBadgeStats returns global badge statistics.
// GET /badges/stats/network
func (h *BadgeHandler) GetNetworkBadgeStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.badgeSvc.GetNetworkBadgeStats(r.Context())
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, stats)
}
