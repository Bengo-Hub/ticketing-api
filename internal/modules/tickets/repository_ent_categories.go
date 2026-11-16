package tickets

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/ticket"
	"github.com/bengobox/ticketing-service/internal/ent/ticketcategory"
)

func (r *EntRepository) CreateCategory(ctx context.Context, tenantID uuid.UUID, c *TicketCategory) error {
	create := r.client.TicketCategory.Create().
		SetID(c.ID).
		SetTenantID(tenantID).
		SetName(c.Name).
		SetSlug(generateSlug(c.Name)).
		SetSortOrder(c.SortOrder).
		SetIsActive(true).
		SetNillableParentID(c.ParentID)
	if c.Description != "" {
		create = create.SetDescription(c.Description)
	}
	if c.Icon != "" {
		create = create.SetIcon(c.Icon)
	}
	created, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ErrCategoryExists
		}
		return err
	}
	c.ID = created.ID
	c.CreatedAt = created.CreatedAt
	c.UpdatedAt = created.UpdatedAt
	return nil
}

// generateSlug lowercases a name and joins its words with dashes.
func generateSlug(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		case r == ' ' || r == '-' || r == '_':
			if s := b.String(); len(s) > 0 && s[len(s)-1] != '-' {
				b.WriteByte('-')
			}
		}
	}
	return b.String()
}

func (r *EntRepository) GetCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (*TicketCategory, error) {
	c, err := r.client.TicketCategory.Query().
		Where(ticketcategory.ID(categoryID), ticketcategory.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrCategoryNotFound
		}
		return nil, err
	}
	return entCategoryToModel(c), nil
}

func (r *EntRepository) ListCategories(ctx context.Context, tenantID uuid.UUID, filter CategoryFilter) ([]*TicketCategory, int, error) {
	query := r.client.TicketCategory.Query().Where(ticketcategory.TenantID(tenantID))
	if filter.ParentID != nil {
		query = query.Where(ticketcategory.ParentID(*filter.ParentID))
	}
	if filter.IsActive != nil {
		query = query.Where(ticketcategory.IsActive(*filter.IsActive))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	rows, err := query.Order(ent.Asc(ticketcategory.FieldSortOrder)).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	result := make([]*TicketCategory, len(rows))
	for i, c := range rows {
		result[i] = entCategoryToModel(c)
	}
	return result, total, nil
}

func (r *EntRepository) UpdateCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID, updates *UpdateCategoryRequest) error {
	update := r.client.TicketCategory.Update().
		Where(ticketcategory.ID(categoryID), ticketcategory.TenantID(tenantID))
	if updates.ParentID != nil {
		update = update.SetParentID(*updates.ParentID)
	}
	if updates.Name != nil {
		update = update.SetName(*updates.Name).SetSlug(generateSlug(*updates.Name))
	}
	if updates.Description != nil {
		update = update.SetDescription(*updates.Description)
	}
	if updates.Icon != nil {
		update = update.SetIcon(*updates.Icon)
	}
	if updates.SortOrder != nil {
		update = update.SetSortOrder(*updates.SortOrder)
	}
	if updates.IsActive != nil {
		update = update.SetIsActive(*updates.IsActive)
	}
	n, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

// DeleteCategory deactivates the category; tickets keep their link to it.
func (r *EntRepository) DeleteCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) error {
	n, err := r.client.TicketCategory.Update().
		Where(ticketcategory.ID(categoryID), ticketcategory.TenantID(tenantID)).
		SetIsActive(false).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

func (r *EntRepository) GetCategoryTicketCount(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (int, error) {
	return r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.CategoryID(categoryID), ticket.DeletedAtIsNil()).
		Count(ctx)
}

func (r *EntRepository) GetCategoryChildCount(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (int, error) {
	return r.client.TicketCategory.Query().
		Where(ticketcategory.TenantID(tenantID), ticketcategory.ParentID(categoryID), ticketcategory.IsActive(true)).
		Count(ctx)
}

func entCategoryToModel(c *ent.TicketCategory) *TicketCategory {
	return &TicketCategory{
		ID:          c.ID,
		TenantID:    c.TenantID,
		ParentID:    c.ParentID,
		Name:        c.Name,
		Description: c.Description,
		Icon:        c.Icon,
		SortOrder:   c.SortOrder,
		IsActive:    c.IsActive,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}
