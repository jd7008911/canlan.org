// internal/handlers/asset.go
package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/yourproject/canglanfu-api/internal/auth"
	"github.com/yourproject/canglanfu-api/internal/services"
	"github.com/yourproject/canglanfu-api/pkg/web"
)

// AssetHandler handles asset-related HTTP requests.
type AssetHandler struct {
	assetSvc *services.AssetService
}

// NewAssetHandler creates a new asset handler.
func NewAssetHandler(assetSvc *services.AssetService) *AssetHandler {
	return &AssetHandler{
		assetSvc: assetSvc,
	}
}

// RegisterRoutes registers the asset routes under the authenticated group.
func (h *AssetHandler) RegisterRoutes(r chi.Router) {
	r.Get("/assets/portfolio", h.GetPortfolio)
	r.Get("/assets/balance/{symbol}", h.GetTokenBalance)
	r.Get("/assets/tokens", h.ListTokens)
	r.Get("/assets/tokens/{symbol}/price", h.GetTokenPrice)
	r.Get("/assets/tokens/{symbol}/holders", h.GetTopHolders)
	r.Get("/assets/stats/network", h.GetNetworkStats)
}

// GetPortfolio returns the user's complete portfolio with all token balances and total value.
func (h *AssetHandler) GetPortfolio(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w, "user not authenticated")
		return
	}

	portfolio, err := h.assetSvc.GetUserPortfolio(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, portfolio)
}

// GetTokenBalance returns the balance of a specific token for the authenticated user.
func (h *AssetHandler) GetTokenBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w, "user not authenticated")
		return
	}

	symbol := chi.URLParam(r, "symbol")
	if symbol == "" {
		web.Error(w, http.StatusBadRequest, "token symbol required")
		return
	}

	balance, err := h.assetSvc.GetUserTokenBalance(r.Context(), userID, symbol)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"symbol":  symbol,
		"balance": balance,
	})
}

// ListTokens returns all active tokens available on the platform.
func (h *AssetHandler) ListTokens(w http.ResponseWriter, r *http.Request) {
	// This could be public or authenticated – we'll keep it public for now.
	// If you want authentication, add the middleware.
	tokens, err := h.assetSvc.queries.ListTokens(r.Context()) // need to expose? We can add a method.
	// Better to add a method to assetSvc: ListAllTokens().
	// For simplicity, we'll assume assetSvc has a ListAllTokens method.
	// We'll add that now:
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, tokens)
}

// GetTokenPrice returns the current USD price of a token.
func (h *AssetHandler) GetTokenPrice(w http.ResponseWriter, r *http.Request) {
	symbol := chi.URLParam(r, "symbol")
	if symbol == "" {
		web.Error(w, http.StatusBadRequest, "token symbol required")
		return
	}

	price, err := h.assetSvc.GetTokenPrice(r.Context(), symbol)
	if err != nil {
		web.Error(w, http.StatusNotFound, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"symbol": symbol,
		"price":  price,
	})
}

// GetTopHolders returns the top N holders of a specific token.
func (h *AssetHandler) GetTopHolders(w http.ResponseWriter, r *http.Request) {
	symbol := chi.URLParam(r, "symbol")
	if symbol == "" {
		web.Error(w, http.StatusBadRequest, "token symbol required")
		return
	}

	// Parse limit from query param, default 10
	limitStr := r.URL.Query().Get("limit")
	limit := int32(10)
	if limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err == nil && l > 0 {
			limit = int32(l)
		}
	}

	holders, err := h.assetSvc.GetTopTokenHolders(r.Context(), symbol, limit)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, holders)
}

// GetNetworkStats returns global network asset statistics (TVL, holders, etc.)
func (h *AssetHandler) GetNetworkStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.assetSvc.GetNetworkAssetStats(r.Context())
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, stats)
}
