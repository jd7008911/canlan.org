// internal/services/governance.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourproject/canglanfu-api/internal/db"
)

// GovernanceService handles all governance-related business logic.
type GovernanceService struct {
	queries   *db.Queries
	badgeSvc  *BadgeService
	combatSvc *CombatPowerService
}

// NewGovernanceService creates a new governance service.
func NewGovernanceService(queries *db.Queries, badgeSvc *BadgeService, combatSvc *CombatPowerService) *GovernanceService {
	return &GovernanceService{
		queries:   queries,
		badgeSvc:  badgeSvc,
		combatSvc: combatSvc,
	}
}

// ---------------------------------------------------------------------
// Proposal Creation & Retrieval
// ---------------------------------------------------------------------

// CreateProposalParams defines the input for creating a new proposal.
type CreateProposalParams struct {
	ProposerID   uuid.UUID
	Title        string
	Description  string
	ProposalType string
	VotingEnd    time.Time
	Quorum       decimal.Decimal // percentage (e.g., 50.0 = 50%)
	Threshold    decimal.Decimal // percentage (e.g., 50.0 = 50%)
}

// CreateProposal creates a new governance proposal.
func (s *GovernanceService) CreateProposal(ctx context.Context, params CreateProposalParams) (*db.GovernanceProposal, error) {
	// Validate voting end is in the future
	if params.VotingEnd.Before(time.Now()) {
		return nil, fmt.Errorf("voting end time must be in the future")
	}

	// Validate quorum and threshold ranges
	if params.Quorum.LessThan(decimal.NewFromInt(0)) || params.Quorum.GreaterThan(decimal.NewFromInt(100)) {
		return nil, fmt.Errorf("quorum must be between 0 and 100")
	}
	if params.Threshold.LessThan(decimal.NewFromInt(0)) || params.Threshold.GreaterThan(decimal.NewFromInt(100)) {
		return nil, fmt.Errorf("threshold must be between 0 and 100")
	}

	proposal, err := s.queries.CreateProposal(ctx, db.CreateProposalParams{
		ProposerID:   params.ProposerID,
		Title:        params.Title,
		Description:  params.Description,
		ProposalType: params.ProposalType,
		VotingEnd:    params.VotingEnd,
		Quorum:       params.Quorum,
		Threshold:    params.Threshold,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create proposal: %w", err)
	}

	return &proposal, nil
}

// GetProposal retrieves a proposal by ID.
func (s *GovernanceService) GetProposal(ctx context.Context, proposalID uuid.UUID) (*db.GovernanceProposal, error) {
	proposal, err := s.queries.GetProposalByID(ctx, proposalID)
	if err != nil {
		return nil, fmt.Errorf("proposal not found: %w", err)
	}
	return &proposal, nil
}

// ListActiveProposals returns all currently active proposals.
func (s *GovernanceService) ListActiveProposals(ctx context.Context) ([]db.GovernanceProposal, error) {
	return s.queries.GetActiveProposals(ctx)
}

// ListProposals paginates all proposals.
func (s *GovernanceService) ListProposals(ctx context.Context, limit, offset int32) ([]db.GovernanceProposal, error) {
	return s.queries.ListProposals(ctx, db.ListProposalsParams{
		Limit:  limit,
		Offset: offset,
	})
}

// ---------------------------------------------------------------------
// Voting & Vote Power
// ---------------------------------------------------------------------

// GetUserVotePower calculates the user's voting power based on combat power
// and badge governance multipliers.
func (s *GovernanceService) GetUserVotePower(ctx context.Context, userID uuid.UUID) (decimal.Decimal, error) {
	// Get base vote power from combat power (personal + lp + burn)
	base, err := s.queries.GetUserVotePower(ctx, userID)
	if err != nil {
		// If no combat power record, vote power is zero
		return decimal.Zero, nil
	}

	// Get badge multipliers
	multipliers, err := s.badgeSvc.GetUserActiveBadgeMultipliers(ctx, userID)
	if err != nil {
		// If error, just use base (no multiplier)
		return base, nil
	}

	// Apply governance multiplier
	votePower := base.Mul(multipliers.GovernanceMultiplier)
	return votePower, nil
}

// CastVote records a user's vote on a proposal.
func (s *GovernanceService) CastVote(ctx context.Context, userID uuid.UUID, proposalID uuid.UUID, choice string) (*db.Vote, error) {
	// Verify proposal is active
	proposal, err := s.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	if proposal.Status != "active" {
		return nil, fmt.Errorf("proposal is not active (status: %s)", proposal.Status)
	}
	if proposal.VotingEnd.Before(time.Now()) {
		// Proposal expired – update status and reject
		s.CloseExpiredProposals(ctx) // background fix
		return nil, fmt.Errorf("voting period has ended")
	}

	// Get user's vote power
	votePower, err := s.GetUserVotePower(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate vote power: %w", err)
	}

	if votePower.IsZero() {
		return nil, fmt.Errorf("zero vote power – cannot vote")
	}

	// Cast the vote (upsert)
	vote, err := s.queries.CastVote(ctx, db.CastVoteParams{
		UserID:     userID,
		ProposalID: proposalID,
		VotePower:  votePower,
		VoteChoice: choice,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to cast vote: %w", err)
	}

	// Recalculate proposal totals
	if err := s.recalculateProposalTotals(ctx, proposalID); err != nil {
		// Log but don't fail the vote
		// logger.Error("failed to recalculate proposal totals", "proposal_id", proposalID, "error", err)
	}

	return &vote, nil
}

// recalculateProposalTotals updates the for_votes, against_votes, abstain_votes,
// and total_votes fields of a proposal based on current votes.
func (s *GovernanceService) recalculateProposalTotals(ctx context.Context, proposalID uuid.UUID) error {
	forVotes, err := s.queries.SumVotePower(ctx, db.SumVotePowerParams{
		ProposalID: proposalID,
		VoteChoice: "for",
	})
	if err != nil {
		return err
	}

	againstVotes, err := s.queries.SumVotePower(ctx, db.SumVotePowerParams{
		ProposalID: proposalID,
		VoteChoice: "against",
	})
	if err != nil {
		return err
	}

	abstainVotes, err := s.queries.SumVotePower(ctx, db.SumVotePowerParams{
		ProposalID: proposalID,
		VoteChoice: "abstain",
	})
	if err != nil {
		return err
	}

	totalVotes := forVotes.Add(againstVotes).Add(abstainVotes)

	return s.queries.UpdateProposalVotes(ctx, db.UpdateProposalVotesParams{
		ID:           proposalID,
		ForVotes:     forVotes,
		AgainstVotes: againstVotes,
		AbstainVotes: abstainVotes,
		TotalVotes:   totalVotes,
	})
}

// ---------------------------------------------------------------------
// Proposal Finalization & Execution
// ---------------------------------------------------------------------

// CloseExpiredProposals marks any active proposals that have passed their voting end
// as either 'passed' or 'rejected' based on quorum and threshold.
func (s *GovernanceService) CloseExpiredProposals(ctx context.Context) error {
	expired, err := s.queries.GetExpiredUnfinalizedProposals(ctx)
	if err != nil {
		return err
	}

	totalVotablePower, err := s.queries.GetTotalVotablePower(ctx)
	if err != nil {
		totalVotablePower = decimal.Zero
	}

	for _, proposal := range expired {
		// Recalculate totals (ensure they're fresh)
		s.recalculateProposalTotals(ctx, proposal.ID)

		// Get results
		results, err := s.queries.GetProposalResults(ctx, proposal.ID)
		if err != nil {
			continue
		}

		// Check quorum: total_votes / total_votable_power >= quorum%
		var quorumMet bool
		if totalVotablePower.IsZero() {
			quorumMet = false
		} else {
			participation := results.TotalVotes.Div(totalVotablePower).Mul(decimal.NewFromInt(100))
			quorumMet = participation.GreaterThanOrEqual(proposal.Quorum)
		}

		// Check threshold: for_votes / total_votes >= threshold%
		var thresholdMet bool
		if results.TotalVotes.IsZero() {
			thresholdMet = false
		} else {
			approval := results.ForVotes.Div(results.TotalVotes).Mul(decimal.NewFromInt(100))
			thresholdMet = approval.GreaterThanOrEqual(proposal.Threshold)
		}

		newStatus := "rejected"
		if quorumMet && thresholdMet {
			newStatus = "passed"
		}

		if err := s.queries.UpdateProposalStatus(ctx, db.UpdateProposalStatusParams{
			ID:     proposal.ID,
			Status: newStatus,
		}); err != nil {
			// log error
			continue
		}
	}

	return nil
}

// ExecuteProposal marks a passed proposal as executed.
// In a real implementation, this would trigger on-chain execution or parameter changes.
func (s *GovernanceService) ExecuteProposal(ctx context.Context, proposalID uuid.UUID) (*db.GovernanceProposal, error) {
	proposal, err := s.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	if proposal.Status != "passed" {
		return nil, fmt.Errorf("only passed proposals can be executed")
	}

	// TODO: Perform actual execution logic (e.g., update protocol parameters,
	// call smart contract, etc.) based on proposal_type and description.

	executed, err := s.queries.ExecuteProposal(ctx, proposalID)
	if err != nil {
		return nil, fmt.Errorf("failed to execute proposal: %w", err)
	}
	return &executed, nil
}

// ---------------------------------------------------------------------
// Query Helpers
// ---------------------------------------------------------------------

// GetProposalResults returns the detailed results for a proposal.
func (s *GovernanceService) GetProposalResults(ctx context.Context, proposalID uuid.UUID) (*db.GetProposalResultsRow, error) {
	results, err := s.queries.GetProposalResults(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	return &results, nil
}

// GetUserVotingHistory returns paginated voting history for a user.
func (s *GovernanceService) GetUserVotingHistory(ctx context.Context, userID uuid.UUID, limit, offset int32) ([]db.GetUserVotesRow, error) {
	return s.queries.GetUserVotes(ctx, db.GetUserVotesParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
}

// GetProposalVotes lists all votes cast on a proposal.
func (s *GovernanceService) GetProposalVotes(ctx context.Context, proposalID uuid.UUID) ([]db.GetProposalVotesRow, error) {
	return s.queries.GetProposalVotes(ctx, proposalID)
}

// ---------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------

// GovernanceStats contains global governance metrics.
type GovernanceStats struct {
	TotalProposals       int64           `json:"total_proposals"`
	ActiveProposals      int64           `json:"active_proposals"`
	PassedProposals      int64           `json:"passed_proposals"`
	ExecutedProposals    int64           `json:"executed_proposals"`
	TotalVoters          int64           `json:"total_voters"`
	AverageParticipation decimal.Decimal `json:"average_participation"`
}

// GetGovernanceStats returns aggregate statistics about the governance system.
func (s *GovernanceService) GetGovernanceStats(ctx context.Context) (*GovernanceStats, error) {
	// This is a placeholder – actual implementation would aggregate from tables.
	// For now, we return zero values.
	return &GovernanceStats{}, nil
}
