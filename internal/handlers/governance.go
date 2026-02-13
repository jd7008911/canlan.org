// internal/handlers/governance.go
package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourproject/canglanfu-api/internal/auth"
	"github.com/yourproject/canglanfu-api/internal/services"
	"github.com/yourproject/canglanfu-api/pkg/web"
)

// GovernanceHandler handles governance-related HTTP requests.
type GovernanceHandler struct {
	governanceSvc *services.GovernanceService
}

// NewGovernanceHandler creates a new governance handler.
func NewGovernanceHandler(governanceSvc *services.GovernanceService) *GovernanceHandler {
	return &GovernanceHandler{
		governanceSvc: governanceSvc,
	}
}

// RegisterRoutes registers governance routes.
func (h *GovernanceHandler) RegisterRoutes(r chi.Router) {
	// Public routes
	r.Get("/governance/proposals", h.ListProposals)
	r.Get("/governance/proposals/{id}", h.GetProposal)
	r.Get("/governance/proposals/{id}/results", h.GetProposalResults)
	r.Get("/governance/proposals/{id}/votes", h.GetProposalVotes)
	r.Get("/governance/stats", h.GetGovernanceStats)

	// Protected routes (require authentication)
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware) // Assumes your middleware is named AuthMiddleware
		r.Post("/governance/proposals", h.CreateProposal)
		r.Post("/governance/proposals/{id}/vote", h.CastVote)
		r.Get("/governance/user/votes", h.GetUserVotingHistory)
	})
}

// ---------------------------------------------------------------------
// Public Handlers
// ---------------------------------------------------------------------

// ListProposals returns a paginated list of proposals, optionally filtered by status.
// GET /governance/proposals?status=active&limit=10&offset=0
func (h *GovernanceHandler) ListProposals(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	status := r.URL.Query().Get("status")
	limit, offset := web.ParsePagination(r)

	var proposals interface{}
	var err error
	var total int64

	switch status {
	case "active":
		proposals, err = h.governanceSvc.ListActiveProposals(r.Context())
		// For active, we don't paginate? Or we can paginate by applying limit/offset after fetch.
		// For simplicity, we'll just return all active proposals without pagination metadata.
		if err != nil {
			web.InternalError(w, err)
			return
		}
		web.Success(w, http.StatusOK, proposals)
		return
	case "pending", "passed", "rejected", "executed":
		// For completed proposals we have a dedicated query; but we can also use ListProposals with filter.
		// We'll use ListProposals which is paginated.
		proposals, err = h.governanceSvc.ListProposals(r.Context(), int32(limit), int32(offset))
		total, _ = h.governanceSvc.CountProposals(r.Context(), status) // you may need to implement this
	default:
		// All proposals, paginated
		proposals, err = h.governanceSvc.ListProposals(r.Context(), int32(limit), int32(offset))
		total, _ = h.governanceSvc.CountProposals(r.Context(), "") // implement this
	}

	if err != nil {
		web.InternalError(w, err)
		return
	}

	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, proposals, meta)
}

// GetProposal returns details of a specific proposal.
// GET /governance/proposals/{id}
func (h *GovernanceHandler) GetProposal(w http.ResponseWriter, r *http.Request) {
	proposalIDStr := chi.URLParam(r, "id")
	proposalID, err := uuid.Parse(proposalIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	proposal, err := h.governanceSvc.GetProposal(r.Context(), proposalID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "proposal not found")
		return
	}

	web.Success(w, http.StatusOK, proposal)
}

// GetProposalResults returns the voting results and status of a proposal.
// GET /governance/proposals/{id}/results
func (h *GovernanceHandler) GetProposalResults(w http.ResponseWriter, r *http.Request) {
	proposalIDStr := chi.URLParam(r, "id")
	proposalID, err := uuid.Parse(proposalIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	results, err := h.governanceSvc.GetProposalResults(r.Context(), proposalID)
	if err != nil {
		web.Error(w, http.StatusNotFound, "proposal not found")
		return
	}

	web.Success(w, http.StatusOK, results)
}

// GetProposalVotes returns all votes cast on a proposal.
// GET /governance/proposals/{id}/votes
func (h *GovernanceHandler) GetProposalVotes(w http.ResponseWriter, r *http.Request) {
	proposalIDStr := chi.URLParam(r, "id")
	proposalID, err := uuid.Parse(proposalIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	votes, err := h.governanceSvc.GetProposalVotes(r.Context(), proposalID)
	if err != nil {
		web.InternalError(w, err)
		return
	}

	web.Success(w, http.StatusOK, votes)
}

// GetGovernanceStats returns global governance statistics.
// GET /governance/stats
func (h *GovernanceHandler) GetGovernanceStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.governanceSvc.GetGovernanceStats(r.Context())
	if err != nil {
		web.InternalError(w, err)
		return
	}
	web.Success(w, http.StatusOK, stats)
}

// ---------------------------------------------------------------------
// Protected Handlers
// ---------------------------------------------------------------------

// CreateProposal creates a new governance proposal.
// POST /governance/proposals
// Request body: { "title": "...", "description": "...", "proposal_type": "...", "voting_end": "...", "quorum": 50, "threshold": 50 }
func (h *GovernanceHandler) CreateProposal(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	var req struct {
		Title        string    `json:"title" validate:"required"`
		Description  string    `json:"description" validate:"required"`
		ProposalType string    `json:"proposal_type" validate:"required"`
		VotingEnd    time.Time `json:"voting_end" validate:"required"`
		Quorum       float64   `json:"quorum"`    // percentage, default 50
		Threshold    float64   `json:"threshold"` // percentage, default 50
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Set defaults
	if req.Quorum == 0 {
		req.Quorum = 50
	}
	if req.Threshold == 0 {
		req.Threshold = 50
	}

	// Validate voting end is in the future
	if req.VotingEnd.Before(time.Now()) {
		web.Error(w, http.StatusBadRequest, "voting end time must be in the future")
		return
	}

	params := services.CreateProposalParams{
		ProposerID:   userID,
		Title:        req.Title,
		Description:  req.Description,
		ProposalType: req.ProposalType,
		VotingEnd:    req.VotingEnd,
		Quorum:       decimal.NewFromFloat(req.Quorum),
		Threshold:    decimal.NewFromFloat(req.Threshold),
	}

	proposal, err := h.governanceSvc.CreateProposal(r.Context(), params)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusCreated, proposal)
}

// CastVote casts a vote on an active proposal.
// POST /governance/proposals/{id}/vote
// Request body: { "vote_choice": "for|against|abstain" }
func (h *GovernanceHandler) CastVote(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	proposalIDStr := chi.URLParam(r, "id")
	proposalID, err := uuid.Parse(proposalIDStr)
	if err != nil {
		web.Error(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	var req struct {
		VoteChoice string `json:"vote_choice" validate:"required,oneof=for against abstain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vote, err := h.governanceSvc.CastVote(r.Context(), userID, proposalID, req.VoteChoice)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	web.Success(w, http.StatusOK, vote)
}

// GetUserVotingHistory returns the authenticated user's voting history.
// GET /governance/user/votes?limit=10&offset=0
func (h *GovernanceHandler) GetUserVotingHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserID(r.Context())
	if !ok {
		web.Unauthorized(w)
		return
	}

	limit, offset := web.ParsePagination(r)
	votes, err := h.governanceSvc.GetUserVotingHistory(r.Context(), userID, int32(limit), int32(offset))
	if err != nil {
		web.InternalError(w, err)
		return
	}

	total, _ := h.governanceSvc.CountUserVotes(r.Context(), userID) // implement this if needed
	meta := web.NewMeta(limit, offset, total)
	web.SuccessWithMeta(w, http.StatusOK, votes, meta)
}
