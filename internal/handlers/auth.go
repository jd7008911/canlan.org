package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourproject/canglanfu-api/internal/auth"
	"github.com/yourproject/canglanfu-api/internal/db"
	"github.com/yourproject/canglanfu-api/internal/services"
	"github.com/yourproject/canglanfu-api/pkg/web"
)

type AuthHandler struct {
	walletAuth  *auth.WalletAuth
	queries     *db.Queries
	referralSvc *services.ReferralService
}

func NewAuthHandler(wa *auth.WalletAuth, q *db.Queries, rs *services.ReferralService) *AuthHandler {
	return &AuthHandler{
		walletAuth:  wa,
		queries:     q,
		referralSvc: rs,
	}
}

func (h *AuthHandler) RegisterRoutes(r chi.Router) {
	r.Post("/auth/nonce", h.GetNonce)
	r.Post("/auth/login", h.Login)
	r.With(h.walletAuth.AuthMiddleware).Get("/auth/me", h.GetMe)
}

type NonceRequest struct {
	Wallet string `json:"wallet"`
}

type NonceResponse struct {
	Nonce string `json:"nonce"`
}

func (h *AuthHandler) GetNonce(w http.ResponseWriter, r *http.Request) {
	var req NonceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	nonce, err := h.walletAuth.GenerateNonce(req.Wallet)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, "failed to generate nonce")
		return
	}

	web.Respond(w, http.StatusOK, NonceResponse{Nonce: nonce})
}

type LoginRequest struct {
	Wallet    string `json:"wallet"`
	Signature string `json:"signature"`
	Referral  string `json:"referral,omitempty"` // optional referral code
}

type LoginResponse struct {
	Token string   `json:"token"`
	User  *db.User `json:"user"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Get nonce from store
	nonceKey := "nonce:" + req.Wallet
	nonce, err := h.walletAuth.nonceStore.Get(r.Context(), nonceKey)
	if err != nil || nonce == "" {
		web.Error(w, http.StatusUnauthorized, "nonce expired or not found")
		return
	}
	defer h.walletAuth.nonceStore.Delete(r.Context(), nonceKey)

	// Verify signature
	ok, err := h.walletAuth.VerifySignature(req.Wallet, req.Signature, nonce)
	if err != nil || !ok {
		web.Error(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// Find or create user
	user, err := h.queries.GetUserByWallet(r.Context(), req.Wallet)
	if err != nil {
		// New user - create account
		code, _ := h.referralSvc.GenerateReferralCode()
		var parentID, invitedBy *uuid.UUID

		// Handle referral
		if req.Referral != "" {
			referrer, err := h.queries.GetReferralByCode(r.Context(), req.Referral)
			if err == nil {
				invitedBy = &referrer.ID
				parentID = &referrer.ID
			}
		}

		user, err = h.queries.CreateUser(r.Context(), db.CreateUserParams{
			WalletAddress: req.Wallet,
			ReferralCode:  code,
			ParentID:      parentID,
			InvitedBy:     invitedBy,
			NodeLevel:     0,
		})
		if err != nil {
			web.Error(w, http.StatusInternalServerError, "failed to create user")
			return
		}

		// Create default mining machine
		h.queries.CreateMiningMachine(r.Context(), user.ID)
		// Create withdrawal limits
		h.queries.CreateWithdrawalLimits(r.Context(), user.ID)
		// Create combat power entry
		h.queries.UpsertCombatPower(r.Context(), db.UpsertCombatPowerParams{
			UserID:        user.ID,
			PersonalPower: decimal.Zero,
			NetworkPower:  decimal.Zero,
			LpWeight:      decimal.Zero,
			BurnPower:     decimal.Zero,
		})

		// Process referral if exists
		if invitedBy != nil {
			h.referralSvc.ProcessReferral(r.Context(), user.ID, *invitedBy, parentID)
		}
	}

	// Generate JWT
	token, err := h.walletAuth.GenerateJWT(user.ID, user.WalletAddress)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	web.Respond(w, http.StatusOK, LoginResponse{
		Token: token,
		User:  &user,
	})
}

func (h *AuthHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.queries.GetUserByID(r.Context(), userID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "user not found")
		return
	}

	web.Respond(w, http.StatusOK, user)
}
