package tickets

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// CreateCategory creates a new category.
func (s *Service) CreateCategory(ctx context.Context, req *CreateCategoryRequest) (*TicketCategory, error) {
	category := &TicketCategory{
		ID:          uuid.New(),
		TenantID:    req.TenantID,
		ParentID:    req.ParentID,
		Name:        req.Name,
		Description: req.Description,
		Icon:        req.Icon,
		SortOrder:   req.SortOrder,
		IsActive:    true,
		Metadata:    req.Metadata,
	}
	if err := s.repo.CreateCategory(ctx, req.TenantID, category); err != nil {
		s.logger.Error("failed to create category", zap.Error(err), zap.String("name", req.Name))
		return nil, err
	}
	return category, nil
}

// GetCategory retrieves a category by ID.
func (s *Service) GetCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (*TicketCategory, error) {
	return s.repo.GetCategory(ctx, tenantID, categoryID)
}

// ListCategories lists categories with filtering.
func (s *Service) ListCategories(ctx context.Context, tenantID uuid.UUID, filter CategoryFilter) ([]*TicketCategory, int, error) {
	filter.TenantID = tenantID
	return s.repo.ListCategories(ctx, tenantID, filter)
}

// UpdateCategory updates a category.
func (s *Service) UpdateCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID, req *UpdateCategoryRequest) error {
	if err := s.repo.UpdateCategory(ctx, tenantID, categoryID, req); err != nil {
		s.logger.Error("failed to update category", zap.Error(err), zap.String("category_id", categoryID.String()))
		return err
	}
	return nil
}

// DeleteCategory deactivates a category that has no active children and no tickets.
func (s *Service) DeleteCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) error {
	childCount, err := s.repo.GetCategoryChildCount(ctx, tenantID, categoryID)
	if err != nil {
		return err
	}
	if childCount > 0 {
		return ErrCategoryHasChildren
	}
	ticketCount, err := s.repo.GetCategoryTicketCount(ctx, tenantID, categoryID)
	if err != nil {
		return err
	}
	if ticketCount > 0 {
		return ErrCategoryHasTickets
	}
	return s.repo.DeleteCategory(ctx, tenantID, categoryID)
}

// GetTicketStats returns ticket counts by status.
func (s *Service) GetTicketStats(ctx context.Context, tenantID uuid.UUID) (map[TicketStatus]int, error) {
	return s.repo.GetTicketCountsByStatus(ctx, tenantID)
}

// GetAnalytics returns combined ticket analytics: by status, by priority and the trend.
func (s *Service) GetAnalytics(ctx context.Context, tenantID uuid.UUID, days int) (*AnalyticsResponse, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	to := s.now().UTC()
	from := to.AddDate(0, 0, -days)
	bucketDays := 1
	if days > 90 {
		bucketDays = 7
	}
	byStatus, err := s.repo.GetTicketCountsByStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byPriority, err := s.repo.GetTicketCountsByPriority(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	trend, err := s.repo.GetTicketTrend(ctx, tenantID, from, to, bucketDays)
	if err != nil {
		return nil, err
	}
	return &AnalyticsResponse{ByStatus: byStatus, ByPriority: byPriority, Trend: trend}, nil
}

// validTransitions lists the statuses each status may move to.
var validTransitions = map[TicketStatus][]TicketStatus{
	StatusNew:             {StatusOpen, StatusInProgress, StatusCancelled},
	StatusOpen:            {StatusInProgress, StatusPendingCustomer, StatusPendingInternal, StatusResolved, StatusClosed, StatusCancelled},
	StatusInProgress:      {StatusOpen, StatusPendingCustomer, StatusPendingInternal, StatusResolved, StatusClosed, StatusCancelled},
	StatusPendingCustomer: {StatusOpen, StatusInProgress, StatusResolved, StatusClosed, StatusCancelled},
	StatusPendingInternal: {StatusOpen, StatusInProgress, StatusResolved, StatusClosed, StatusCancelled},
	StatusResolved:        {StatusOpen, StatusClosed},
	StatusClosed:          {StatusOpen},
	StatusCancelled:       {},
}

func isValidStatusTransition(from, to TicketStatus) bool {
	for _, s := range validTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// RecordSLABreach marks a ticket's breached SLA targets and records the breach event in the same
// transaction. It reports false when the breach was already recorded (by another replica or a
// status change), so the event goes out once.
func (s *Service) RecordSLABreach(ctx context.Context, t *Ticket, now time.Time) (bool, error) {
	responseBreached := t.SLAResponseDueAt != nil && now.After(*t.SLAResponseDueAt) && !t.SLAResponseBreached && t.FirstResponseAt == nil
	resolutionBreached := t.SLAResolutionDueAt != nil && now.After(*t.SLAResolutionDueAt) && !t.SLAResolutionBreached
	if !responseBreached && !resolutionBreached {
		return false, nil
	}
	changed := false
	err := s.repo.InTx(ctx, t.TenantID, func(repo Repository, out *Outbox) error {
		var err error
		if changed, err = repo.MarkSLABreached(ctx, t.TenantID, t.ID, responseBreached, resolutionBreached); err != nil || !changed {
			return err
		}
		// The assigned agent's email lets notifications-api alert them; otherwise it alerts the
		// tenant's support contact.
		agentEmail := ""
		if t.AssignedAgentID != nil {
			if agentEmail, err = repo.GetAgentEmail(ctx, t.TenantID, *t.AssignedAgentID); err != nil {
				return err
			}
		}
		out.SLABreach(SLABreachPayload(t, agentEmail, responseBreached, resolutionBreached))
		return nil
	})
	return changed, err
}
