package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/shopspring/decimal"
	"jd7008911/canlan.org/internal/auth"
	"jd7008911/canlan.org/internal/db"
	"jd7008911/canlan.org/internal/services"
	"jd7008911/canlan.org/pkg/web"
)

type BurnHandler struct {
	burnSvc *services.BurnService
	queries *db.Queries
}

func (h *BurnHandler) Burn(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		TokenSymbol string          `json:"token_symbol"`
		Amount      decimal.Decimal `json:"amount"`
		TxHash      string          `json:"tx_hash"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	token, err := h.queries.GetTokenBySymbol(r.Context(), req.TokenSymbol)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "token not found")
		return
	}

	err = h.burnSvc.BurnTokens(r.Context(), userID, token.ID, req.Amount, req.TxHash)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Respond(w, http.StatusOK, map[string]string{"status": "burned"})
}
