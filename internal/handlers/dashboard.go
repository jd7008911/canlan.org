package handlers

import (
	"net/http"

	"github.com/yourproject/canglanfu-api/internal/auth"
	"github.com/yourproject/canglanfu-api/internal/db"
	"github.com/yourproject/canglanfu-api/internal/services"
	"github.com/yourproject/canglanfu-api/pkg/web"
)

type DashboardHandler struct {
	queries        *db.Queries
	combatSvc      *services.CombatPowerService
	blockRewardSvc *services.BlockRewardService
	assetSvc       *services.AssetService
}

func (h *DashboardHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Combat power
	cp, _ := h.queries.GetCombatPower(r.Context(), userID)
	// Network combat power
	networkCP, _ := h.queries.GetNetworkCombatPower(r.Context())
	// Token balances
	balances, _ := h.queries.GetUserBalancesWithTokens(r.Context(), userID)
	// Mining earnings today
	miningToday, _ := h.queries.GetTodayMiningEarnings(r.Context(), userID)
	// Block countdown
	_, remaining, _ := h.blockRewardSvc.GetCurrentBlockAndCountdown(r.Context())
	// Badges
	badges, _ := h.queries.GetUserActiveBadges(r.Context(), userID)
	// Node info
	node, _ := h.queries.GetUserNode(r.Context(), userID)

	response := map[string]interface{}{
		"combat_power":          cp,
		"network_combat_power":  networkCP,
		"balances":              balances,
		"mining_earnings_today": miningToday,
		"block_countdown_sec":   remaining.Seconds(),
		"badges":                badges,
		"node_level":            node,
	}

	web.Respond(w, http.StatusOK, response)
}
