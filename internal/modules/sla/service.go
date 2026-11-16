package sla

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

// Service provides SLA business logic.
type Service struct {
	repo   Repository
	logger *zap.Logger
	now    func() time.Time
}

// NewService creates a new SLA service.
func NewService(repo Repository, logger *zap.Logger) *Service {
	return &Service{repo: repo, logger: logger.Named("sla"), now: time.Now}
}

func invalidPolicy(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidPolicy, fmt.Sprintf(format, args...))
}

// checkPolicyRefs refuses a calendar outside the tenant, invalid SLA settings in the metadata,
// and escalation rules naming a team outside the tenant.
func (s *Service) checkPolicyRefs(ctx context.Context, tenantID uuid.UUID, calendarID *uuid.UUID, meta map[string]any) error {
	if calendarID != nil {
		if _, err := s.repo.GetCalendar(ctx, tenantID, *calendarID); err != nil {
			if errors.Is(err, ErrCalendarNotFound) {
				return invalidPolicy("calendar_id is not a calendar of this tenant")
			}
			return err
		}
	}
	settings, err := clock.ParseSettings(meta)
	if err != nil {
		return invalidPolicy("%s", err.Error())
	}
	for i, r := range settings.EscalationRules {
		team := r.Team()
		if team == nil {
			continue
		}
		ok, err := s.repo.TeamExists(ctx, tenantID, *team)
		if err != nil {
			return err
		}
		if !ok {
			return invalidPolicy("escalation_rules[%d].team_id is not a team of this tenant", i)
		}
	}
	return nil
}

// CreatePolicy creates a new SLA policy.
func (s *Service) CreatePolicy(ctx context.Context, req *CreatePolicyRequest) (*Policy, error) {
	if req.Name == "" {
		return nil, invalidPolicy("name is required")
	}
	if req.FirstResponseMinutes <= 0 || req.ResolutionMinutes <= 0 {
		return nil, invalidPolicy("first_response_minutes and resolution_minutes must be positive")
	}
	if err := s.checkPolicyRefs(ctx, req.TenantID, req.CalendarID, req.Metadata); err != nil {
		return nil, err
	}
	p := &Policy{
		ID:                   uuid.New(),
		TenantID:             req.TenantID,
		Name:                 req.Name,
		Description:          req.Description,
		Priority:             req.Priority,
		CategoryID:           req.CategoryID,
		CalendarID:           req.CalendarID,
		FirstResponseMinutes: req.FirstResponseMinutes,
		ResolutionMinutes:    req.ResolutionMinutes,
		IsActive:             true,
		SortOrder:            req.SortOrder,
		Metadata:             req.Metadata,
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
	if (req.FirstResponseMinutes != nil && *req.FirstResponseMinutes <= 0) || (req.ResolutionMinutes != nil && *req.ResolutionMinutes <= 0) {
		return invalidPolicy("first_response_minutes and resolution_minutes must be positive")
	}
	calendarID := req.CalendarID
	if req.ClearCalendar {
		calendarID = nil
	}
	if err := s.checkPolicyRefs(ctx, tenantID, calendarID, req.Metadata); err != nil {
		return err
	}
	if err := s.repo.Update(ctx, tenantID, policyID, req); err != nil {
		if !errors.Is(err, ErrPolicyNotFound) {
			s.logger.Error("failed to update sla policy", zap.Error(err), zap.String("policy_id", policyID.String()))
		}
		return err
	}
	return nil
}

// DeletePolicy deletes an SLA policy.
func (s *Service) DeletePolicy(ctx context.Context, tenantID, policyID uuid.UUID) error {
	if err := s.repo.Delete(ctx, tenantID, policyID); err != nil {
		if !errors.Is(err, ErrPolicyNotFound) {
			s.logger.Error("failed to delete sla policy", zap.Error(err), zap.String("policy_id", policyID.String()))
		}
		return err
	}
	return nil
}

// ComputeDeadlines picks the best-matching policy for a ticket's priority and category and adds
// its targets to start in the policy's clock. It returns nil when no policy matches (no SLA for
// this ticket).
func (s *Service) ComputeDeadlines(ctx context.Context, tenantID uuid.UUID, priority Priority, categoryID *uuid.UUID, start time.Time) (*DeadlineResult, error) {
	policy, err := s.repo.FindMatchingPolicy(ctx, tenantID, priority, categoryID)
	if err != nil {
		// No matching policy: the ticket has no SLA.
		return nil, nil //nolint:nilerr
	}
	cal, err := s.calendarClock(ctx, tenantID, policy.CalendarID)
	if err != nil {
		return nil, err
	}
	return &DeadlineResult{
		PolicyID:        policy.ID,
		ResponseDueAt:   cal.Add(start, policy.FirstResponseMinutes),
		ResolutionDueAt: cal.Add(start, policy.ResolutionMinutes),
	}, nil
}

// PolicyClock returns the clock of a ticket's policy. A ticket without a policy, or whose policy
// or calendar is gone, counts every minute and pauses only on pending_customer.
func (s *Service) PolicyClock(ctx context.Context, tenantID uuid.UUID, policyID *uuid.UUID) (PolicyClock, error) {
	if policyID == nil {
		return PolicyClock{}, nil
	}
	policy, err := s.repo.Get(ctx, tenantID, *policyID)
	if errors.Is(err, ErrPolicyNotFound) {
		return PolicyClock{}, nil
	}
	if err != nil {
		return PolicyClock{}, err
	}
	cal, err := s.calendarClock(ctx, tenantID, policy.CalendarID)
	if err != nil {
		return PolicyClock{}, err
	}
	// Settings were validated on save; a malformed legacy value counts as unset.
	settings, _ := clock.ParseSettings(policy.Metadata)
	return PolicyClock{Calendar: cal, PauseOnPendingInternal: settings.PauseOnPendingInternal}, nil
}

// calendarClock loads and parses a calendar; nil (24x7) for no calendar or a deleted one.
func (s *Service) calendarClock(ctx context.Context, tenantID uuid.UUID, calendarID *uuid.UUID) (*clock.Calendar, error) {
	if calendarID == nil {
		return nil, nil
	}
	c, err := s.repo.GetCalendar(ctx, tenantID, *calendarID)
	if errors.Is(err, ErrCalendarNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cal, err := clock.New(c.Timezone, c.WeeklyHours, c.Holidays)
	if err != nil {
		s.logger.Warn("stored calendar does not parse, counting 24x7", zap.String("calendar_id", c.ID.String()), zap.Error(err))
		return nil, nil
	}
	return cal, nil
}
