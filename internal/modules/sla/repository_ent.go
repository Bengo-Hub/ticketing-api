package sla

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	entpkg "github.com/bengobox/ticketing-service/internal/ent/slapolicy"
	entteam "github.com/bengobox/ticketing-service/internal/ent/team"
)

// EntRepository implements Repository using the Ent ORM.
type EntRepository struct {
	client *ent.Client
}

// NewEntRepository creates a new Ent-backed SLA repository.
func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client}
}

func (r *EntRepository) Create(ctx context.Context, p *Policy) error {
	m := r.client.SLAPolicy.Create().
		SetID(p.ID).
		SetTenantID(p.TenantID).
		SetName(p.Name).
		SetPriority(entpkg.Priority(p.Priority)).
		SetFirstResponseMinutes(p.FirstResponseMinutes).
		SetResolutionMinutes(p.ResolutionMinutes).
		SetIsActive(p.IsActive).
		SetSortOrder(p.SortOrder).
		SetNillableCategoryID(p.CategoryID).
		SetNillableCalendarID(p.CalendarID)
	if p.Description != "" {
		m = m.SetDescription(p.Description)
	}
	if p.Metadata != nil {
		m = m.SetMetadata(p.Metadata)
	}
	row, err := m.Save(ctx)
	if err != nil {
		return err
	}
	p.CreatedAt = row.CreatedAt
	p.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *EntRepository) Get(ctx context.Context, tenantID, policyID uuid.UUID) (*Policy, error) {
	row, err := r.client.SLAPolicy.Query().
		Where(entpkg.ID(policyID), entpkg.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrPolicyNotFound
		}
		return nil, err
	}
	return toPolicy(row), nil
}

func (r *EntRepository) List(ctx context.Context, filter PolicyFilter) ([]*Policy, int, error) {
	q := r.client.SLAPolicy.Query().
		Where(entpkg.TenantID(filter.TenantID))
	if filter.Priority != nil {
		q = q.Where(entpkg.PriorityEQ(entpkg.Priority(*filter.Priority)))
	}
	if filter.IsActive != nil {
		q = q.Where(entpkg.IsActive(*filter.IsActive))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	rows, err := q.Order(ent.Asc(entpkg.FieldSortOrder)).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	policies := make([]*Policy, len(rows))
	for i, row := range rows {
		policies[i] = toPolicy(row)
	}
	return policies, total, nil
}

func (r *EntRepository) Update(ctx context.Context, tenantID, policyID uuid.UUID, req *UpdatePolicyRequest) error {
	u := r.client.SLAPolicy.Update().Where(entpkg.ID(policyID), entpkg.TenantID(tenantID))
	if req.Name != nil {
		u = u.SetName(*req.Name)
	}
	if req.Description != nil {
		u = u.SetDescription(*req.Description)
	}
	if req.Priority != nil {
		u = u.SetPriority(entpkg.Priority(*req.Priority))
	}
	if req.CategoryID != nil {
		u = u.SetCategoryID(*req.CategoryID)
	}
	switch {
	case req.ClearCalendar:
		u = u.ClearCalendarID()
	case req.CalendarID != nil:
		u = u.SetCalendarID(*req.CalendarID)
	}
	if req.FirstResponseMinutes != nil {
		u = u.SetFirstResponseMinutes(*req.FirstResponseMinutes)
	}
	if req.ResolutionMinutes != nil {
		u = u.SetResolutionMinutes(*req.ResolutionMinutes)
	}
	if req.IsActive != nil {
		u = u.SetIsActive(*req.IsActive)
	}
	if req.SortOrder != nil {
		u = u.SetSortOrder(*req.SortOrder)
	}
	if req.Metadata != nil {
		u = u.SetMetadata(req.Metadata)
	}
	n, err := u.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

func (r *EntRepository) Delete(ctx context.Context, tenantID, policyID uuid.UUID) error {
	n, err := r.client.SLAPolicy.Delete().
		Where(entpkg.ID(policyID), entpkg.TenantID(tenantID)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

func (r *EntRepository) FindMatchingPolicy(ctx context.Context, tenantID uuid.UUID, priority Priority, categoryID *uuid.UUID) (*Policy, error) {
	base := []func() *ent.SLAPolicyQuery{}
	// A category policy takes precedence over the catch-all; lower sort_order wins within each.
	if categoryID != nil {
		base = append(base, func() *ent.SLAPolicyQuery {
			return r.client.SLAPolicy.Query().Where(entpkg.CategoryID(*categoryID))
		})
	}
	base = append(base, func() *ent.SLAPolicyQuery {
		return r.client.SLAPolicy.Query().Where(entpkg.CategoryIDIsNil())
	})
	for _, q := range base {
		row, err := q().
			Where(entpkg.TenantID(tenantID), entpkg.IsActive(true), entpkg.PriorityEQ(entpkg.Priority(priority))).
			Order(ent.Asc(entpkg.FieldSortOrder)).
			First(ctx)
		if err == nil {
			return toPolicy(row), nil
		}
		if !ent.IsNotFound(err) {
			return nil, err
		}
	}
	return nil, errors.New("no matching sla policy")
}

func (r *EntRepository) TeamExists(ctx context.Context, tenantID, teamID uuid.UUID) (bool, error) {
	return r.client.Team.Query().
		Where(entteam.ID(teamID), entteam.TenantID(tenantID), entteam.DeletedAtIsNil()).
		Exist(ctx)
}

func toPolicy(row *ent.SLAPolicy) *Policy {
	return &Policy{
		ID:                   row.ID,
		TenantID:             row.TenantID,
		Name:                 row.Name,
		Description:          row.Description,
		Priority:             Priority(row.Priority),
		CategoryID:           row.CategoryID,
		CalendarID:           row.CalendarID,
		FirstResponseMinutes: row.FirstResponseMinutes,
		ResolutionMinutes:    row.ResolutionMinutes,
		IsActive:             row.IsActive,
		SortOrder:            row.SortOrder,
		Metadata:             row.Metadata,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}
