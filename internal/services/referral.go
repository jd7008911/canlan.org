package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/google/uuid"
	"github.com/yourproject/canglanfu-api/internal/db"
)

type ReferralService struct {
	queries *db.Queries
}

func NewReferralService(queries *db.Queries) *ReferralService {
	return &ReferralService{queries: queries}
}

// GenerateReferralCode creates a unique referral code
func (s *ReferralService) GenerateReferralCode() (string, error) {
	b := make([]byte, 6)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ProcessReferral handles new user registration with referrer
func (s *ReferralService) ProcessReferral(ctx context.Context, newUserID, referrerID uuid.UUID, parentID *uuid.UUID) error {
	// Update inviter's direct referrals count
	node, err := s.queries.GetUserNode(ctx, referrerID)
	if err != nil {
		node = &db.UserNode{UserID: referrerID}
	}
	node.DirectReferrals++
	s.queries.UpsertUserNode(ctx, db.UpsertUserNodeParams{
		UserID:          referrerID,
		DirectReferrals: int32(node.DirectReferrals),
	})

	// Update team combat power for all ancestors
	return s.updateAncestorsTeamPower(ctx, newUserID, referrerID)
}

// GetReferralNetwork returns a user's referral tree
func (s *ReferralService) GetReferralNetwork(ctx context.Context, userID uuid.UUID) (*db.UserNode, []db.User, error) {
	node, err := s.queries.GetUserNode(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	directs, err := s.queries.GetDirectReferrals(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return node, directs, nil
}

// updateAncestorsTeamPower recalculates team power for all ancestors
func (s *ReferralService) updateAncestorsTeamPower(ctx context.Context, userID, inviterID uuid.UUID) error {
	// Get user's combat power
	cp, err := s.queries.GetCombatPower(ctx, userID)
	if err != nil {
		return err
	}

	// Walk up the referral chain
	currentID := inviterID
	for currentID != uuid.Nil {
		// Add user's power to ancestor's team power
		err = s.queries.AddTeamPower(ctx, db.AddTeamPowerParams{
			UserID:    currentID,
			TeamPower: cp.PersonalPower,
		})
		if err != nil {
			return err
		}

		// Get parent of current
		user, err := s.queries.GetUserByID(ctx, currentID)
		if err != nil {
			break
		}
		if user.ParentID == nil {
			break
		}
		currentID = *user.ParentID
	}
	return nil
}
