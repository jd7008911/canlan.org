// internal/handlers/referral.go
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yourproject/canglanfu-api/internal/auth"
	"github.com/yourproject/canglanfu-api/internal/services"
	"github.com/yourproject/canglanfu-api/pkg/web"
)

// ReferralHandler handles referral-related HTTP requests.
type ReferralHandler struct {
	referralSvc *services.ReferralService
	nodeSvc     *services.NodeService // used for team stats
}

// NewReferralHandler creates a new referral handler.
func NewReferralHandler(referralSvc *services.ReferralService, nodeSvc *services.NodeService) *ReferralHandler {
	return &ReferralHandler{
		referralSvc: referralSvc,
		nodeSvc:     nodeSvc,
	}
}

// RegisterRoutes registers referral routes.
func (h *ReferralHandler) RegisterRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware) // All referral endpoints require authentication

		r.Get("/referral/code", h.GetReferralCode)
		r.Post("/referral/generate", h.GenerateReferralCode)
		r.Get("/referral/earnings", h.GetReferralEarnings)
		r.Get("/referral/stats", h.GetReferralStats)
		r.Get("/referral/history", h.GetReferralHistory)
	})
}

// ---------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------

// GetReferralCode returns the user's current referral code.
// GET /referral/code
func (h *ReferralHandler) GetReferralCode(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// Assuming ReferralService has GetReferralCode method
	code, err := h.referralSvc.GetReferralCode(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, map[string]string{
		"referral_code": code,
		"referral_link": generateReferralLink(code), // Helper function
	})
}

// GenerateReferralCode generates a new referral code for the user.
// POST /referral/generate
func (h *ReferralHandler) GenerateReferralCode(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// Generate new code
	newCode, err := h.referralSvc.GenerateReferralCode()
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Update user's referral code in database
	err = h.referralSvc.UpdateReferralCode(r.Context(), userID, newCode)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, map[string]string{
		"referral_code": newCode,
		"referral_link": generateReferralLink(newCode),
	})
}

// GetReferralEarnings returns the user's total referral earnings.
// GET /referral/earnings
func (h *ReferralHandler) GetReferralEarnings(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// Assuming ReferralService has GetTotalEarnings method
	earnings, err := h.referralSvc.GetTotalEarnings(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"total_earnings": earnings,
		"currency":       "USDT", // or whatever token is used for rewards
	})
}

// GetReferralStats returns summary statistics about the user's referral network.
// GET /referral/stats
func (h *ReferralHandler) GetReferralStats(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// Get node info for team stats
	node, err := h.nodeSvc.GetUserNode(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Get earnings from referral service
	earnings, _ := h.referralSvc.GetTotalEarnings(r.Context(), userID)

	stats := map[string]interface{}{
		"direct_referrals": node.DirectReferrals,
		"team_members":     node.TeamMembers,
		"team_power":       node.TeamPower,
		"total_earnings":   earnings,
	}

	web.Success(w, http.StatusOK, stats)
}

// GetReferralHistory returns a list of referral events (e.g., when someone joined using the user's code).
// GET /referral/history?limit=20&offset=0
func (h *ReferralHandler) GetReferralHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limit, offset := web.ParsePagination(r)

	// Assuming ReferralService has GetReferralHistory method
	history, err := h.referralSvc.GetReferralHistory(r.Context(), userID, int32(limit), int32(offset))
	if err != nil {
		web.InternalError(w, err)
		return
	}

	total, _ := h.referralSvc.CountReferralHistory(r.Context(), userID)

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, history, meta)
}

// ---------------------------------------------------------------------
// Helper Functions
// ---------------------------------------------------------------------

// generateReferralLink builds the full referral URL from the code.
func generateReferralLink(code string) string {
	// In production, read base URL from config
	return "https://www.canglanfu.org/?ref=" + code
}
