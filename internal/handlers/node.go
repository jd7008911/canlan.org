// internal/handlers/node.go
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"jd7008911/canlan.org/internal/auth"
	"jd7008911/canlan.org/internal/services"
	"jd7008911/canlan.org/pkg/web"
)

// NodeHandler handles node level and referral network HTTP requests.
type NodeHandler struct {
	nodeSvc *services.NodeService
}

// NewNodeHandler creates a new node handler.
func NewNodeHandler(nodeSvc *services.NodeService) *NodeHandler {
	return &NodeHandler{
		nodeSvc: nodeSvc,
	}
}

// RegisterRoutes registers node routes.
func (h *NodeHandler) RegisterRoutes(r chi.Router) {
	// Public routes
	r.Get("/node/levels", h.ListNodeLevels)
	r.Get("/node/levels/{level}", h.GetNodeLevel)
	r.Get("/node/stats/network", h.GetNetworkNodeStats)

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware) // Adjust middleware name as needed
		r.Get("/node/my", h.GetMyNode)
		r.Get("/node/team", h.GetTeamInfo)
		r.Get("/node/team/power", h.GetTeamPower)
		r.Get("/node/team/members", h.GetTeamMembers)
		r.Get("/node/team/subtree", h.GetReferralSubtree)
		r.Get("/node/ancestors", h.GetAncestors)
		r.Get("/node/direct", h.GetDirectReferrals)
		r.Post("/node/upgrade", h.UpgradeNode)
		r.Get("/node/eligibility", h.GetUpgradeEligibility)
		r.Get("/node/rights", h.GetUserRights)
		r.Get("/node/gift-limit", h.GetGiftLimit)
	})
}

// ---------------------------------------------------------------------
// Public Handlers
// ---------------------------------------------------------------------

// ListNodeLevels returns all defined node levels.
// GET /node/levels
func (h *NodeHandler) ListNodeLevels(w http.ResponseWriter, r *http.Request) {
	levels, err := h.nodeSvc.ListNodeLevels(r.Context())
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, levels)
}

// GetNodeLevel returns details for a specific node level.
// GET /node/levels/{level}
func (h *NodeHandler) GetNodeLevel(w http.ResponseWriter, r *http.Request) {
	levelStr := chi.URLParam(r, "level")
	level, err := strconv.Atoi(levelStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid node level")
		return
	}

	nodeLevel, err := h.nodeSvc.GetNodeLevel(r.Context(), int32(level))
	if err != nil {
		web.Error(w, http.StatusNotFound, "node level not found")
		return
	}
	web.Success(w, http.StatusOK, nodeLevel)
}

// GetNetworkNodeStats returns global network node statistics.
// GET /node/stats/network
func (h *NodeHandler) GetNetworkNodeStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.nodeSvc.GetNetworkNodeStats(r.Context())
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, stats)
}

// ---------------------------------------------------------------------
// Authenticated Handlers
// ---------------------------------------------------------------------

// GetMyNode returns the authenticated user's node status and next level requirements.
// GET /node/my
func (h *NodeHandler) GetMyNode(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	details, err := h.nodeSvc.GetUserNodeWithDetails(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, details)
}

// GetTeamInfo returns the user's team statistics (team power, member count, etc.)
// GET /node/team
func (h *NodeHandler) GetTeamInfo(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	node, err := h.nodeSvc.GetUserNode(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"team_power":   node.TeamPower,
		"team_members": node.TeamMembers,
		"direct_count": node.DirectReferrals,
	})
}

// GetTeamPower returns the total combat power of the user's entire downline.
// GET /node/team/power
func (h *NodeHandler) GetTeamPower(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	power, err := h.nodeSvc.queries.GetTeamPower(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"team_power": power,
	})
}

// GetTeamMembers returns the number of members in the user's downline.
// GET /node/team/members
func (h *NodeHandler) GetTeamMembers(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	count, err := h.nodeSvc.queries.GetTeamMemberCount(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"team_members": count,
	})
}

// GetDirectReferrals returns the list of users directly referred by the authenticated user.
// GET /node/direct?limit=20&offset=0
func (h *NodeHandler) GetDirectReferrals(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limit, offset := web.ParsePagination(r)
	referrals, err := h.nodeSvc.queries.GetDirectReferrals(r.Context(), db.GetDirectReferralsParams{
		InvitedBy: userID,
		Limit:     int32(limit),
		Offset:    int32(offset),
	})
	if err != nil {
		web.InternalError(w, err)
		return
	}

	total, _ := h.nodeSvc.queries.GetDirectReferralCount(r.Context(), userID)
	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, referrals, meta)
}

// GetReferralSubtree returns the paginated downline tree of the user.
// GET /node/team/subtree?limit=50&offset=0&depth=3
func (h *NodeHandler) GetReferralSubtree(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limit, offset := web.ParsePagination(r)
	// Optional max depth
	depth := 10
	if depthStr := r.URL.Query().Get("depth"); depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil && d > 0 {
			depth = d
		}
	}

	// You may need a more efficient paginated tree query.
	// For simplicity, we assume the query supports limit/offset.
	subtree, err := h.nodeSvc.queries.GetReferralSubtree(r.Context(), db.GetReferralSubtreeParams{
		UserID: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		web.InternalError(w, err)
		return
	}

	total, _ := h.nodeSvc.queries.GetTeamMemberCount(r.Context(), userID)
	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, subtree, meta)
}

// GetAncestors returns the referral chain above the user.
// GET /node/ancestors
func (h *NodeHandler) GetAncestors(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	ancestors, err := h.nodeSvc.GetReferralAncestors(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, ancestors)
}

// GetUpgradeEligibility checks if the user can upgrade to the next node level.
// GET /node/eligibility
func (h *NodeHandler) GetUpgradeEligibility(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	eligibility, err := h.nodeSvc.CheckUpgradeEligibility(r.Context(), userID)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	web.Success(w, http.StatusOK, eligibility)
}

// UpgradeNode performs a node level upgrade for the user.
// POST /node/upgrade
func (h *NodeHandler) UpgradeNode(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	newLevel, err := h.nodeSvc.UpgradeNode(r.Context(), userID)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"message":   "Node upgraded successfully",
		"new_level": newLevel,
	})
}

// GetUserRights returns the rights JSON for the user's current node level.
// GET /node/rights
func (h *NodeHandler) GetUserRights(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	rights, err := h.nodeSvc.GetUserRights(r.Context(), userID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "rights not found")
		return
	}

	// rights is []byte (JSON) – we can return it directly or parse.
	// If it's valid JSON, we can send it as raw JSON.
	var parsed interface{}
	if err := json.Unmarshal(rights, &parsed); err == nil {
		web.Success(w, http.StatusOK, parsed)
	} else {
		// Fallback: send as string
		web.Success(w, http.StatusOK, map[string]string{
			"rights": string(rights),
		})
	}
}

// GetGiftLimit returns the user's gift limit and remaining amount.
// GET /node/gift-limit
func (h *NodeHandler) GetGiftLimit(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limit, err := h.nodeSvc.GetUserGiftLimit(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Check remaining limit (you may add a method to NodeService)
	remaining, _ := h.nodeSvc.CheckGiftLimitAvailable(r.Context(), userID, decimal.Zero)

	web.Success(w, http.StatusOK, map[string]interface{}{
		"gift_limit":    limit,
		"remaining":     remaining,
		"current_level": nil, // You might want to include the current node level
	})
}
