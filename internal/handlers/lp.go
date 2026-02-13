// internal/handlers/lp.go
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

// LPHandler handles liquidity pool related HTTP requests.
type LPHandler struct {
	lpSvc *services.LPService
}

// NewLPHandler creates a new LP handler.
func NewLPHandler(lpSvc *services.LPService) *LPHandler {
	return &LPHandler{
		lpSvc: lpSvc,
	}
}

// RegisterRoutes registers LP routes.
func (h *LPHandler) RegisterRoutes(r chi.Router) {
	// Public routes
	r.Get("/lp/pools", h.ListPools)
	r.Get("/lp/pools/{id}", h.GetPool)
	r.Get("/lp/pools/{id}/stats", h.GetPoolStats)

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware) // Assumes your middleware is named AuthMiddleware
		r.Get("/lp/positions", h.GetUserLPPositions)
		r.Get("/lp/positions/{id}", h.GetLPPosition)
		r.Get("/lp/weight", h.GetUserLPWeight)
		r.Post("/lp/add", h.AddLiquidity)
		r.Post("/lp/remove", h.RemoveLiquidity)
		r.Get("/lp/transactions", h.GetLPTransactions)
	})
}

// ---------------------------------------------------------------------
// Public Handlers
// ---------------------------------------------------------------------

// ListPools returns all active liquidity pools.
// GET /lp/pools
func (h *LPHandler) ListPools(w http.ResponseWriter, r *http.Request) {
	pools, err := h.lpSvc.ListPools(r.Context())
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, pools)
}

// GetPool returns details of a specific liquidity pool.
// GET /lp/pools/{id}
func (h *LPHandler) GetPool(w http.ResponseWriter, r *http.Request) {
	poolIDStr := chi.URLParam(r, "id")
	poolID, err := uuid.Parse(poolIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid pool ID")
		return
	}

	pool, err := h.lpSvc.GetPoolByID(r.Context(), poolID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "pool not found")
		return
	}
	web.Success(w, http.StatusOK, pool)
}

// GetPoolStats returns statistical data for a pool (TVL, APR, user count, etc.)
// GET /lp/pools/{id}/stats
func (h *LPHandler) GetPoolStats(w http.ResponseWriter, r *http.Request) {
	poolIDStr := chi.URLParam(r, "id")
	poolID, err := uuid.Parse(poolIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid pool ID")
		return
	}

	// Gather various stats
	pool, _ := h.lpSvc.GetPoolByID(r.Context(), poolID)
	totalLP, _ := h.lpSvc.queries.GetPoolTotalLPAmount(r.Context(), poolID)
	userCount, _ := h.lpSvc.queries.GetPoolUserCount(r.Context(), poolID) // You may need to add this query

	stats := map[string]interface{}{
		"pool_id":             poolID,
		"name":                pool.Name,
		"total_liquidity_usd": pool.TotalLiquidityUsd,
		"apr":                 pool.Apr,
		"total_lp_amount":     totalLP,
		"liquidity_providers": userCount,
	}
	web.Success(w, http.StatusOK, stats)
}

// ---------------------------------------------------------------------
// Authenticated Handlers
// ---------------------------------------------------------------------

// GetUserLPPositions returns all LP positions for the authenticated user.
// GET /lp/positions
func (h *LPHandler) GetUserLPPositions(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	positions, err := h.lpSvc.GetUserLPPositions(r.Context(), userID)
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, positions)
}

// GetLPPosition returns a specific LP position by ID.
// GET /lp/positions/{id}
func (h *LPHandler) GetLPPosition(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	positionIDStr := chi.URLParam(r, "id")
	positionID, err := uuid.Parse(positionIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid position ID")
		return
	}

	// Verify ownership
	position, err := h.lpSvc.queries.GetLPPosition(r.Context(), positionID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "position not found")
		return
	}
	if position.UserID != userID {
		web.Forbidden(w)
		return
	}

	web.Success(w, http.StatusOK, position)
}

// GetUserLPWeight returns the total LP weight (sum of LP amounts) for the authenticated user.
// GET /lp/weight
func (h *LPHandler) GetUserLPWeight(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	weight, err := h.lpSvc.GetUserLPWeight(r.Context(), userID)
	if err != nil {
		weight = decimal.Zero
	}
	web.Success(w, http.StatusOK, map[string]interface{}{
		"user_id":   userID,
		"lp_weight": weight,
	})
}

// AddLiquidity handles adding liquidity to a pool.
// POST /lp/add
// Request body: { "pool_id": "uuid", "amount0": "100.0", "amount1": "100.0", "tx_hash": "0x..." }
func (h *LPHandler) AddLiquidity(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	var req struct {
		PoolID  uuid.UUID       `json:"pool_id"`
		Amount0 decimal.Decimal `json:"amount0"`
		Amount1 decimal.Decimal `json:"amount1"`
		TxHash  string          `json:"tx_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PoolID == uuid.Nil {
		web.Error(w, http.StatusBadRequest, "pool_id is required")
		return
	}
	if req.Amount0.LessThanOrEqual(decimal.Zero) || req.Amount1.LessThanOrEqual(decimal.Zero) {
		web.Error(w, http.StatusBadRequest, "amounts must be positive")
		return
	}
	if req.TxHash == "" {
		web.Error(w, http.StatusBadRequest, "tx_hash is required")
		return
	}

	params := services.AddLiquidityParams{
		UserID:  userID,
		PoolID:  req.PoolID,
		Amount0: req.Amount0,
		Amount1: req.Amount1,
		TxHash:  req.TxHash,
	}

	position, err := h.lpSvc.AddLiquidity(r.Context(), params)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, position)
}

// RemoveLiquidity handles removing liquidity from a pool.
// POST /lp/remove
// Request body: { "pool_id": "uuid", "lp_amount": "50.0", "tx_hash": "0x..." }
func (h *LPHandler) RemoveLiquidity(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	var req struct {
		PoolID   uuid.UUID       `json:"pool_id"`
		LpAmount decimal.Decimal `json:"lp_amount"`
		TxHash   string          `json:"tx_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PoolID == uuid.Nil {
		web.Error(w, http.StatusBadRequest, "pool_id is required")
		return
	}
	if req.LpAmount.LessThanOrEqual(decimal.Zero) {
		web.Error(w, http.StatusBadRequest, "lp_amount must be positive")
		return
	}
	if req.TxHash == "" {
		web.Error(w, http.StatusBadRequest, "tx_hash is required")
		return
	}

	params := services.RemoveLiquidityParams{
		UserID:   userID,
		PoolID:   req.PoolID,
		LpAmount: req.LpAmount,
		TxHash:   req.TxHash,
	}

	amount0, amount1, err := h.lpSvc.RemoveLiquidity(r.Context(), params)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, map[string]interface{}{
		"amount0": amount0,
		"amount1": amount1,
	})
}

// GetLPTransactions returns the user's LP transaction history (add/remove).
// GET /lp/transactions?limit=20&offset=0
func (h *LPHandler) GetLPTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limit, offset := web.ParsePagination(r)
	transactions, err := h.lpSvc.queries.GetUserLPTransactions(r.Context(), db.GetUserLPTransactionsParams{
		UserID: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		web.InternalError(w, err)
		return
	}

	// Get total count for pagination
	total, _ := h.lpSvc.queries.CountUserLPTransactions(r.Context(), userID) // You may need to add this query

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, transactions, meta)
}
