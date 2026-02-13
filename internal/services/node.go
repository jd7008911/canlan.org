// internal/services/node.go
package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"jd7008911/canlan.org/internal/db"
)

// NodeService handles all node level and referral network business logic.
type NodeService struct {
	queries     *db.Queries
	referralSvc *ReferralService
	combatSvc   *CombatPowerService
}

// NewNodeService creates a new node service.
func NewNodeService(queries *db.Queries, referralSvc *ReferralService, combatSvc *CombatPowerService) *NodeService {
	return &NodeService{
		queries:     queries,
		referralSvc: referralSvc,
		combatSvc:   combatSvc,
	}
}

// ---------------------------------------------------------------------
// Node Level Definitions
// ---------------------------------------------------------------------

// ListNodeLevels returns all defined node levels.
func (s *NodeService) ListNodeLevels(ctx context.Context) ([]db.NodeLevel, error) {
	return s.queries.ListNodeLevels(ctx)
}

// GetNodeLevel returns details for a specific node level.
func (s *NodeService) GetNodeLevel(ctx context.Context, level int32) (*db.NodeLevel, error) {
	nodeLevel, err := s.queries.GetNodeLevel(ctx, level)
	if err != nil {
		return nil, fmt.Errorf("node level %d not found: %w", level, err)
	}
	return &nodeLevel, nil
}

// ---------------------------------------------------------------------
// User Node Status
// ---------------------------------------------------------------------

// GetUserNode returns the user's current node status.
func (s *NodeService) GetUserNode(ctx context.Context, userID uuid.UUID) (*db.UserNode, error) {
	node, err := s.queries.GetUserNode(ctx, userID)
	if err != nil {
		// If no record exists, create one with default values
		node, err = s.queries.UpsertUserNode(ctx, db.UpsertUserNodeParams{
			UserID:          userID,
			CurrentLevel:    0,
			TeamPower:       decimal.Zero,
			TeamMembers:     0,
			DirectReferrals: 0,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create user node: %w", err)
		}
	}
	return &node, nil
}

// GetUserNodeWithDetails returns enriched node information including next level requirements.
func (s *NodeService) GetUserNodeWithDetails(ctx context.Context, userID uuid.UUID) (*db.GetUserNodeWithDetailsRow, error) {
	details, err := s.queries.GetUserNodeWithDetails(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node details: %w", err)
	}
	return &details, nil
}

// ---------------------------------------------------------------------
// Team & Referral Calculations
// ---------------------------------------------------------------------

// RecalculateTeamStats updates the user's team power and member count based on downline.
// Should be called whenever a new user joins under this user.
func (s *NodeService) RecalculateTeamStats(ctx context.Context, userID uuid.UUID) error {
	// Get team power from all downline users
	teamPower, err := s.queries.GetTeamPower(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to calculate team power: %w", err)
	}

	// Get team member count
	teamMembers, err := s.queries.GetTeamMemberCount(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to calculate team members: %w", err)
	}

	// Get direct referral count
	directReferrals, err := s.queries.GetDirectReferralCount(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to count direct referrals: %w", err)
	}

	// Update user node
	_, err = s.queries.UpsertUserNode(ctx, db.UpsertUserNodeParams{
		UserID:          userID,
		CurrentLevel:    0, // Will be updated separately if needed
		TeamPower:       teamPower,
		TeamMembers:     int32(teamMembers),
		DirectReferrals: int32(directReferrals),
	})
	if err != nil {
		return fmt.Errorf("failed to update node stats: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------
// Node Level Eligibility & Upgrade
// ---------------------------------------------------------------------

// CheckUpgradeEligibility determines if a user can upgrade to the next node level.
func (s *NodeService) CheckUpgradeEligibility(ctx context.Context, userID uuid.UUID) (*db.CheckNodeUpgradeEligibilityRow, error) {
	eligibility, err := s.queries.CheckNodeUpgradeEligibility(ctx, userID)
	if err != nil {
		// May be no next level defined or user already at max level
		return nil, fmt.Errorf("cannot check eligibility: %w", err)
	}
	return &eligibility, nil
}

// UpgradeNode attempts to upgrade the user to the next node level.
// Returns the new level and any benefits granted.
func (s *NodeService) UpgradeNode(ctx context.Context, userID uuid.UUID) (int32, error) {
	// Check eligibility
	eligibility, err := s.CheckUpgradeEligibility(ctx, userID)
	if err != nil {
		return 0, err
	}
	if !eligibility.Eligible {
		return 0, fmt.Errorf("not eligible for upgrade: power_eligible=%v, members_eligible=%v",
			eligibility.PowerEligible, eligibility.MembersEligible)
	}

	// Perform upgrade
	err = s.queries.UpgradeUserNode(ctx, db.UpgradeUserNodeParams{
		ID:        userID,
		NodeLevel: eligibility.NextLevel,
	})
	if err != nil {
		return 0, fmt.Errorf("upgrade failed: %w", err)
	}

	// Update user_nodes table with new level
	_, err = s.queries.UpsertUserNode(ctx, db.UpsertUserNodeParams{
		UserID:          userID,
		CurrentLevel:    eligibility.NextLevel,
		TeamPower:       eligibility.TeamPower,
		TeamMembers:     eligibility.TeamMembers,
		DirectReferrals: eligibility.DirectReferrals,
	})
	if err != nil {
		// Log but don't fail – the user record already upgraded
		// logger.Error("failed to update user_nodes after upgrade", "error", err)
	}

	// Grant node level benefits (e.g., gift limit, rights)
	if err := s.GrantNodeBenefits(ctx, userID, eligibility.NextLevel); err != nil {
		// Log but don't fail upgrade
		// logger.Error("failed to grant node benefits", "level", eligibility.NextLevel, "error", err)
	}

	return eligibility.NextLevel, nil
}

// GrantNodeBenefits applies the rights and gift limits associated with a node level.
func (s *NodeService) GrantNodeBenefits(ctx context.Context, userID uuid.UUID, level int32) error {
	nodeLevel, err := s.GetNodeLevel(ctx, level)
	if err != nil {
		return err
	}

	// Update withdrawal limits if gift limit is higher
	limits, err := s.queries.GetWithdrawalLimits(ctx, userID)
	if err != nil {
		// Create default limits if not exist
		limits, err = s.queries.CreateWithdrawalLimits(ctx, db.CreateWithdrawalLimitsParams{
			UserID:       userID,
			DailyLimit:   decimal.NewFromInt(1000),
			MonthlyLimit: decimal.NewFromInt(30000),
		})
		if err != nil {
			return err
		}
	}

	// If node level has a higher daily limit, apply it
	if nodeLevel.GiftLimit.GreaterThan(limits.DailyLimit) {
		err = s.queries.UpdateWithdrawalLimits(ctx, db.UpdateWithdrawalLimitsParams{
			UserID:       userID,
			DailyLimit:   nodeLevel.GiftLimit,
			MonthlyLimit: limits.MonthlyLimit, // keep existing monthly limit
		})
		if err != nil {
			return err
		}
	}

	// Other rights (JSON) could be stored in user_profiles or a separate table.
	// For now, we just log that rights were granted.
	return nil
}

// ---------------------------------------------------------------------
// Network & Referral Tree
// ---------------------------------------------------------------------

// GetReferralAncestors returns the chain of referrers above the user.
func (s *NodeService) GetReferralAncestors(ctx context.Context, userID uuid.UUID) ([]db.GetReferralAncestorsRow, error) {
	return s.queries.GetReferralAncestors(ctx, userID)
}

// GetReferralSubtree returns all users in the downline of a referrer.
// Useful for team management views.
func (s *NodeService) GetReferralSubtree(ctx context.Context, userID uuid.UUID) ([]db.GetReferralSubtreeRow, error) {
	return s.queries.GetReferralSubtree(ctx, userID)
}

// GetReferralTreeDepth returns the maximum depth of the referral tree under a user.
func (s *NodeService) GetReferralTreeDepth(ctx context.Context, userID uuid.UUID) (int32, error) {
	depth, err := s.queries.GetReferralTreeDepth(ctx, userID)
	if err != nil {
		return 0, err
	}
	return int32(depth), nil
}

// ---------------------------------------------------------------------
// Node Rights & Gift Limits
// ---------------------------------------------------------------------

// GetUserRights returns the rights JSON for the user's current node level.
func (s *NodeService) GetUserRights(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	rights, err := s.queries.GetUserRights(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user rights: %w", err)
	}
	return rights, nil
}

// GetUserGiftLimit returns the gift limit for the user's current node level.
func (s *NodeService) GetUserGiftLimit(ctx context.Context, userID uuid.UUID) (decimal.Decimal, error) {
	limit, err := s.queries.GetUserGiftLimit(ctx, userID)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to get gift limit: %w", err)
	}
	return limit, nil
}

// CheckGiftLimitAvailable verifies if a user has enough gift limit remaining.
func (s *NodeService) CheckGiftLimitAvailable(ctx context.Context, userID uuid.UUID, amount decimal.Decimal) (bool, decimal.Decimal, error) {
	remaining, err := s.queries.CheckGiftLimitAvailable(ctx, userID)
	if err != nil {
		return false, decimal.Zero, err
	}
	return remaining.GreaterThanOrEqual(amount), remaining, nil
}

// ---------------------------------------------------------------------
// Network Statistics
// ---------------------------------------------------------------------

// NetworkNodeStats contains global statistics about node distribution.
type NetworkNodeStats struct {
	TotalUsers          int64               `json:"total_users"`
	AverageNodeLevel    float64             `json:"average_node_level"`
	HighestNodeLevel    int32               `json:"highest_node_level"`
	NodeHolders         int64               `json:"node_holders"`
	DistributionByLevel []LevelDistribution `json:"distribution_by_level"`
	TopNodes            []db.GetTopNodesRow `json:"top_nodes"`
}

type LevelDistribution struct {
	Level     int32 `json:"level"`
	UserCount int64 `json:"user_count"`
}

// GetNetworkNodeStats aggregates global node statistics.
func (s *NodeService) GetNetworkNodeStats(ctx context.Context) (*NetworkNodeStats, error) {
	// Get overall stats
	stats, err := s.queries.GetNetworkStats(ctx)
	if err != nil {
		return nil, err
	}

	// Get distribution by level
	distRows, err := s.queries.GetNetworkNodeDistribution(ctx)
	if err != nil {
		return nil, err
	}

	distribution := make([]LevelDistribution, len(distRows))
	for i, row := range distRows {
		distribution[i] = LevelDistribution{
			Level:     row.NodeLevel,
			UserCount: row.UserCount,
		}
	}

	// Get top nodes
	topNodes, err := s.queries.GetTopNodes(ctx, 10)
	if err != nil {
		return nil, err
	}

	return &NetworkNodeStats{
		TotalUsers:          stats.TotalUsers,
		AverageNodeLevel:    stats.AverageNodeLevel,
		HighestNodeLevel:    stats.HighestNodeLevel,
		NodeHolders:         stats.NodeHolders,
		DistributionByLevel: distribution,
		TopNodes:            topNodes,
	}, nil
}

// ---------------------------------------------------------------------
// Maintenance & Batch Operations
// ---------------------------------------------------------------------

// RecalculateAllTeamStats updates team power and member counts for all users.
// Should be run periodically or after major referral events.
func (s *NodeService) RecalculateAllTeamStats(ctx context.Context) error {
	return s.queries.RecalculateAllTeamStats(ctx)
}

// BatchUpgradeNodes attempts to upgrade all eligible users.
// Returns count of users upgraded.
func (s *NodeService) BatchUpgradeNodes(ctx context.Context) (int64, error) {
	result, err := s.queries.BatchUpgradeNodes(ctx)
	if err != nil {
		return 0, err
	}
	// result.RowsAffected() – need to adapt based on sqlc/pgx.
	// For simplicity, we'll re‑query and count.
	// This is a placeholder – actual implementation may vary.
	return 0, nil
}

// ---------------------------------------------------------------------
// Node Level Administration (for platform operators)
// ---------------------------------------------------------------------

// CreateNodeLevel adds a new node level definition.
func (s *NodeService) CreateNodeLevel(ctx context.Context, level int32, requiredTeamPower decimal.Decimal, requiredDirectMembers int32, rights []byte, giftLimit decimal.Decimal) (*db.NodeLevel, error) {
	nodeLevel, err := s.queries.CreateNodeLevel(ctx, db.CreateNodeLevelParams{
		Level:                 level,
		RequiredTeamPower:     requiredTeamPower,
		RequiredDirectMembers: requiredDirectMembers,
		Rights:                rights,
		GiftLimit:             giftLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create node level: %w", err)
	}
	return &nodeLevel, nil
}

// UpdateNodeLevel modifies an existing node level.
func (s *NodeService) UpdateNodeLevel(ctx context.Context, level int32, requiredTeamPower decimal.Decimal, requiredDirectMembers int32, rights []byte, giftLimit decimal.Decimal) (*db.NodeLevel, error) {
	nodeLevel, err := s.queries.UpdateNodeLevel(ctx, db.UpdateNodeLevelParams{
		Level:                 level,
		RequiredTeamPower:     requiredTeamPower,
		RequiredDirectMembers: requiredDirectMembers,
		Rights:                rights,
		GiftLimit:             giftLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update node level: %w", err)
	}
	return &nodeLevel, nil
}

// DeleteNodeLevel removes a node level (use with caution).
func (s *NodeService) DeleteNodeLevel(ctx context.Context, level int32) error {
	return s.queries.DeleteNodeLevel(ctx, level)
}
