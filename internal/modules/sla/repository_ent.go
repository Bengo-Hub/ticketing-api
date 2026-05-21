package sla

import (
	"context"
	"errors"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	entpkg "github.com/bengobox/ticketing-service/internal/ent/slapolicy"
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
		SetFirstResponseBusinessHours(p.FirstResponseBusinessHours).
		SetResolutionMinutes(p.ResolutionMinutes).
		SetResolutionBusinessHours(p.ResolutionBusinessHours).
		SetIsActive(p.IsActive).
		SetSortOrder(p.SortOrder)

	if p.Description != "" {
		m = m.SetDescription(p.Description)
	}
	if p.CategoryID != nil {
		m = m.SetCategoryID(*p.CategoryID)
	}
	if p.BusinessHours != nil {
		m = m.SetBusinessHours(p.BusinessHours)
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
	row, err := r.client.SLAPolicy.Get(ctx, policyID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrPolicyNotFound
		}
		return nil, err
	}
	if row.TenantID != tenantID {
		return nil, ErrPolicyNotFound
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
	row, err := r.client.SLAPolicy.Get(ctx, policyID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrPolicyNotFound
		}
		return err
	}
	if row.TenantID != tenantID {
		return ErrPolicyNotFound
	}

	u := row.Update()
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
	if req.FirstResponseMinutes != nil {
		u = u.SetFirstResponseMinutes(*req.FirstResponseMinutes)
	}
	if req.FirstResponseBusinessHours != nil {
		u = u.SetFirstResponseBusinessHours(*req.FirstResponseBusinessHours)
	}
	if req.ResolutionMinutes != nil {
		u = u.SetResolutionMinutes(*req.ResolutionMinutes)
	}
	if req.ResolutionBusinessHours != nil {
		u = u.SetResolutionBusinessHours(*req.ResolutionBusinessHours)
	}
	if req.IsActive != nil {
		u = u.SetIsActive(*req.IsActive)
	}
	if req.SortOrder != nil {
		u = u.SetSortOrder(*req.SortOrder)
	}
	if req.BusinessHours != nil {
		u = u.SetBusinessHours(req.BusinessHours)
	}
	if req.Metadata != nil {
		u = u.SetMetadata(req.Metadata)
	}

	_, err = u.Save(ctx)
	return err
}

func (r *EntRepository) Delete(ctx context.Context, tenantID, policyID uuid.UUID) error {
	row, err := r.client.SLAPolicy.Get(ctx, policyID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrPolicyNotFound
		}
		return err
	}
	if row.TenantID != tenantID {
		return ErrPolicyNotFound
	}
	return r.client.SLAPolicy.DeleteOne(row).Exec(ctx)
}

func (r *EntRepository) FindMatchingPolicy(ctx context.Context, tenantID uuid.UUID, priority Priority, categoryID *uuid.UUID) (*Policy, error) {
	q := r.client.SLAPolicy.Query().
		Where(
			entpkg.TenantID(tenantID),
			entpkg.IsActive(true),
			entpkg.PriorityEQ(entpkg.Priority(priority)),
		)

	// Category-specific policy takes precedence (lower sort_order wins)
	if categoryID != nil {
		catQ := r.client.SLAPolicy.Query().
			Where(
				entpkg.TenantID(tenantID),
				entpkg.IsActive(true),
				entpkg.PriorityEQ(entpkg.Priority(priority)),
				func(s *sql.Selector) {
					s.Where(sql.EQ(entpkg.FieldCategoryID, *categoryID))
				},
			).Order(ent.Asc(entpkg.FieldSortOrder))

		row, err := catQ.First(ctx)
		if err == nil {
			return toPolicy(row), nil
		}
		if !ent.IsNotFound(err) {
			return nil, err
		}
	}

	// Fall back to catch-all (no category_id) policy
	q = q.Where(entpkg.CategoryIDIsNil()).Order(ent.Asc(entpkg.FieldSortOrder))
	row, err := q.First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New("no matching sla policy")
		}
		return nil, err
	}
	return toPolicy(row), nil
}

func toPolicy(row *ent.SLAPolicy) *Policy {
	return &Policy{
		ID:                         row.ID,
		TenantID:                   row.TenantID,
		Name:                       row.Name,
		Description:                row.Description,
		Priority:                   Priority(row.Priority),
		CategoryID:                 row.CategoryID,
		FirstResponseMinutes:       row.FirstResponseMinutes,
		FirstResponseBusinessHours: row.FirstResponseBusinessHours,
		ResolutionMinutes:          row.ResolutionMinutes,
		ResolutionBusinessHours:    row.ResolutionBusinessHours,
		IsActive:                   row.IsActive,
		SortOrder:                  row.SortOrder,
		BusinessHours:              row.BusinessHours,
		Metadata:                   row.Metadata,
		CreatedAt:                  row.CreatedAt,
		UpdatedAt:                  row.UpdatedAt,
	}
}
