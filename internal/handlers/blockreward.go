// internal/handlers/blockreward.go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"jd7008911/canlan.org/internal/auth"
	"jd7008911/canlan.org/internal/services"
	"jd7008911/canlan.org/pkg/web"
)

// BlockRewardHandler handles mint block and reward-related HTTP requests.
type BlockRewardHandler struct {
	blockRewardSvc *services.BlockRewardService
}

// NewBlockRewardHandler creates a new block reward handler.
func NewBlockRewardHandler(blockRewardSvc *services.BlockRewardService) *BlockRewardHandler {
	return &BlockRewardHandler{
		blockRewardSvc: blockRewardSvc,
	}
}

// RegisterRoutes registers block reward routes.
func (h *BlockRewardHandler) RegisterRoutes(r chi.Router) {
	// Public endpoints
	r.Get("/block/countdown", h.GetCountdown)
	r.Get("/block/current", h.GetCurrentBlock)

	// Authenticated endpoints
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware) // Use your actual auth middleware name
		r.Get("/block/rewards", h.GetUserRewards)
		r.Get("/block/rewards/history", h.GetRewardHistory)
		r.Post("/block/rewards/claim", h.ClaimRewards)
		r.Post("/block/rewards/claim-all", h.ClaimAllRewards)
	})

	// Admin endpoints (optional, can be protected by role middleware)
	// r.With(auth.RequireRole("admin")).Get("/block/stats", h.GetNetworkStats)
}

// ---------------------------------------------------------------------
// Public Handlers
// ---------------------------------------------------------------------

// GetCountdown returns the current mint block countdown.
// GET /block/countdown
func (h *BlockRewardHandler) GetCountdown(w http.ResponseWriter, r *http.Request) {
	block, remaining, err := h.blockRewardSvc.GetCurrentBlockAndCountdown(r.Context())
	if err != nil {
		// No active block is a normal state, just return zero values
		web.Success(w, http.StatusOK, map[string]interface{}{
			"block":         nil,
			"remaining_sec": remaining.Seconds(),
			"remaining_hms": remaining.String(),
		})
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"block":         block,
		"remaining_sec": remaining.Seconds(),
		"remaining_hms": remaining.String(),
	})
}

// GetCurrentBlock returns details of the current (undistributed) mint block.
// GET /block/current
func (h *BlockRewardHandler) GetCurrentBlock(w http.ResponseWriter, r *http.Request) {
	block, _, err := h.blockRewardSvc.GetCurrentBlockAndCountdown(r.Context())
	if err != nil || block == nil {
		web.Error(w, http.StatusNotFound, "no active mint block")
		return
	}
	web.Success(w, http.StatusOK, block)
}

// ---------------------------------------------------------------------
// Authenticated Handlers
// ---------------------------------------------------------------------

// GetUserRewards returns all unclaimed rewards for the authenticated user.
// GET /block/rewards
func (h *BlockRewardHandler) GetUserRewards(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	rewards, err := h.blockRewardSvc.GetUserUnclaimedRewards(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Also get summary for convenience
	summary, _ := h.blockRewardSvc.GetUserRewardsSummary(r.Context(), userID)

	response := map[string]interface{}{
		"rewards": rewards,
		"summary": summary,
	}
	web.Success(w, http.StatusOK, response)
}

// GetRewardHistory returns paginated historical rewards (claimed/unclaimed) for the user.
// GET /block/rewards/history?limit=20&offset=0
func (h *BlockRewardHandler) GetRewardHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	// Parse pagination
	limit, offset := web.ParsePagination(r)
	limitInt := int32(limit)
	offsetInt := int32(offset)

	rewards, err := h.blockRewardSvc.GetUserBlockRewards(r.Context(), userID, limitInt, offsetInt)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Get total count for pagination
	total, _ := h.blockRewardSvc.GetUserRewardsCount(r.Context(), userID) // you may need to add this method

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, rewards, meta)
}

// ClaimRewards claims a specific set of unclaimed rewards.
// POST /block/rewards/claim
// Request body: { "reward_ids": ["uuid1", "uuid2"] }
func (h *BlockRewardHandler) ClaimRewards(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	var req struct {
		RewardIDs []uuid.UUID `json:"reward_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.RewardIDs) == 0 {
		web.Error(w, http.StatusBadRequest, "reward_ids cannot be empty")
		return
	}

	claimedAmount, err := h.blockRewardSvc.ClaimRewards(r.Context(), userID, req.RewardIDs)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"claimed_amount": claimedAmount,
		"claimed_count":  len(req.RewardIDs),
	})
}

// ClaimAllRewards claims all unclaimed rewards for the user.
// POST /block/rewards/claim-all
func (h *BlockRewardHandler) ClaimAllRewards(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	claimedAmount, err := h.blockRewardSvc.ClaimAllRewards(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Get count of claimed rewards (we don't have that from ClaimAllRewards, maybe return count too)
	// For simplicity, we just return the amount.
	web.Success(w, http.StatusOK, map[string]interface{}{
		"claimed_amount": claimedAmount,
		"message":        "all rewards claimed successfully",
	})
}

// ---------------------------------------------------------------------
// Admin Handlers (optional)
// ---------------------------------------------------------------------

// GetNetworkStats returns global block reward statistics.
// GET /block/stats
func (h *BlockRewardHandler) GetNetworkStats(w http.ResponseWriter, r *http.Request) {
	// Placeholder – you can implement this if needed.
	// Example: total blocks, total rewards distributed, average per block, etc.
	web.Success(w, http.StatusOK, map[string]interface{}{
		"message": "not implemented",
	})
}
