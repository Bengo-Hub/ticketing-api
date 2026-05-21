package sla

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service provides SLA business logic.
type Service struct {
	repo   Repository
	logger *zap.Logger
}

// NewService creates a new SLA service.
func NewService(repo Repository, logger *zap.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: logger.Named("sla"),
	}
}

// CreatePolicy creates a new SLA policy.
func (s *Service) CreatePolicy(ctx context.Context, req *CreatePolicyRequest) (*Policy, error) {
	if req.Name == "" {
		return nil, ErrInvalidPolicy
	}
	if req.FirstResponseMinutes <= 0 || req.ResolutionMinutes <= 0 {
		return nil, ErrInvalidPolicy
	}

	p := &Policy{
		ID:                         uuid.New(),
		TenantID:                   req.TenantID,
		Name:                       req.Name,
		Description:                req.Description,
		Priority:                   req.Priority,
		CategoryID:                 req.CategoryID,
		FirstResponseMinutes:       req.FirstResponseMinutes,
		FirstResponseBusinessHours: req.FirstResponseBusinessHours,
		ResolutionMinutes:          req.ResolutionMinutes,
		ResolutionBusinessHours:    req.ResolutionBusinessHours,
		IsActive:                   true,
		SortOrder:                  req.SortOrder,
		BusinessHours:              req.BusinessHours,
		Metadata:                   req.Metadata,
	}

	if err := s.repo.Create(ctx, p); err != nil {
		s.logger.Error("failed to create sla policy", zap.Error(err), zap.String("name", req.Name))
		return nil, err
	}

	s.logger.Info("sla policy created", zap.String("policy_id", p.ID.String()), zap.String("name", p.Name))
	return p, nil
}

// GetPolicy retrieves an SLA policy by ID.
func (s *Service) GetPolicy(ctx context.Context, tenantID, policyID uuid.UUID) (*Policy, error) {
	return s.repo.Get(ctx, tenantID, policyID)
}

// ListPolicies lists SLA policies with filtering.
func (s *Service) ListPolicies(ctx context.Context, filter PolicyFilter) ([]*Policy, int, error) {
	return s.repo.List(ctx, filter)
}

// UpdatePolicy updates an SLA policy.
func (s *Service) UpdatePolicy(ctx context.Context, tenantID, policyID uuid.UUID, req *UpdatePolicyRequest) error {
	if err := s.repo.Update(ctx, tenantID, policyID, req); err != nil {
		s.logger.Error("failed to update sla policy", zap.Error(err), zap.String("policy_id", policyID.String()))
		return err
	}
	return nil
}

// DeletePolicy deletes an SLA policy.
func (s *Service) DeletePolicy(ctx context.Context, tenantID, policyID uuid.UUID) error {
	if err := s.repo.Delete(ctx, tenantID, policyID); err != nil {
		s.logger.Error("failed to delete sla policy", zap.Error(err), zap.String("policy_id", policyID.String()))
		return err
	}
	return nil
}

// ComputeDeadlines looks up the best-matching policy for a ticket's priority and
// category, then returns the computed first-response and resolution deadlines.
// Returns nil if no policy matches (SLA tracking disabled for this ticket).
func (s *Service) ComputeDeadlines(ctx context.Context, tenantID uuid.UUID, priority Priority, categoryID *uuid.UUID, createdAt time.Time) (*DeadlineResult, error) {
	policy, err := s.repo.FindMatchingPolicy(ctx, tenantID, priority, categoryID)
	if err != nil {
		// No matching policy — silently skip SLA tracking
		return nil, nil //nolint:nilerr
	}

	responseDue := createdAt.Add(time.Duration(policy.FirstResponseMinutes) * time.Minute)
	resolutionDue := createdAt.Add(time.Duration(policy.ResolutionMinutes) * time.Minute)

	return &DeadlineResult{
		PolicyID:        policy.ID,
		ResponseDueAt:   responseDue,
		ResolutionDueAt: resolutionDue,
	}, nil
}
