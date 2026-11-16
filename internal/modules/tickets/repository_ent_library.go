package tickets

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	entagent "github.com/bengobox/ticketing-service/internal/ent/agent"
	"github.com/bengobox/ticketing-service/internal/ent/macro"
	"github.com/bengobox/ticketing-service/internal/ent/team"
	"github.com/bengobox/ticketing-service/internal/ent/ticketlink"
	"github.com/bengobox/ticketing-service/internal/ent/ticketview"
)

// Saved views, macros, ticket links and the assignee checks. Every query is tenant scoped.

func (r *EntRepository) AgentEmails(ctx context.Context, tenantID uuid.UUID, agentIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(agentIDs))
	if len(agentIDs) == 0 {
		return out, nil
	}
	rows, err := r.client.Agent.Query().
		Where(entagent.TenantID(tenantID), entagent.IDIn(agentIDs...), entagent.IsActive(true)).
		Select(entagent.FieldID, entagent.FieldEmail).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range rows {
		out[a.ID] = a.Email
	}
	return out, nil
}

func (r *EntRepository) TeamExists(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID) (bool, error) {
	return r.client.Team.Query().
		Where(team.ID(teamID), team.TenantID(tenantID), team.DeletedAtIsNil()).
		Exist(ctx)
}

// Views

func (r *EntRepository) CreateView(ctx context.Context, tenantID uuid.UUID, v *TicketView) error {
	row, err := r.client.TicketView.Create().
		SetID(v.ID).SetTenantID(tenantID).SetOwnerUserID(v.OwnerUserID).SetName(v.Name).
		SetFilters(v.Filters).SetSort(v.Sort).SetShared(v.Shared).
		Save(ctx)
	if err != nil {
		return err
	}
	v.CreatedAt, v.UpdatedAt = row.CreatedAt, row.UpdatedAt
	return nil
}

func (r *EntRepository) GetView(ctx context.Context, tenantID uuid.UUID, viewID uuid.UUID) (*TicketView, error) {
	row, err := r.client.TicketView.Query().Where(ticketview.ID(viewID), ticketview.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrViewNotFound
	}
	if err != nil {
		return nil, err
	}
	return entViewToModel(row), nil
}

func (r *EntRepository) ListViews(ctx context.Context, tenantID uuid.UUID, ownerUserID uuid.UUID, limit int) ([]*TicketView, error) {
	rows, err := r.client.TicketView.Query().
		Where(ticketview.TenantID(tenantID), ticketview.Or(ticketview.OwnerUserID(ownerUserID), ticketview.Shared(true))).
		Order(ent.Asc(ticketview.FieldName), ent.Asc(ticketview.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*TicketView, len(rows))
	for i, row := range rows {
		out[i] = entViewToModel(row)
	}
	return out, nil
}

func (r *EntRepository) UpdateView(ctx context.Context, tenantID uuid.UUID, v *TicketView) error {
	n, err := r.client.TicketView.Update().
		Where(ticketview.ID(v.ID), ticketview.TenantID(tenantID)).
		SetName(v.Name).SetFilters(v.Filters).SetSort(v.Sort).SetShared(v.Shared).
		Save(ctx)
	if err == nil && n == 0 {
		err = ErrViewNotFound
	}
	return err
}

func (r *EntRepository) DeleteView(ctx context.Context, tenantID uuid.UUID, viewID uuid.UUID) error {
	n, err := r.client.TicketView.Delete().Where(ticketview.ID(viewID), ticketview.TenantID(tenantID)).Exec(ctx)
	if err == nil && n == 0 {
		err = ErrViewNotFound
	}
	return err
}

func entViewToModel(row *ent.TicketView) *TicketView {
	filters := row.Filters
	if filters == nil {
		filters = map[string]any{}
	}
	return &TicketView{
		ID: row.ID, TenantID: row.TenantID, OwnerUserID: row.OwnerUserID, Name: row.Name,
		Filters: filters, Sort: row.Sort, Shared: row.Shared, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// Macros

func actionsToMap(a MacroActions) map[string]any {
	data, _ := json.Marshal(a)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out
}

func actionsFromMap(m map[string]any) MacroActions {
	var a MacroActions
	if data, err := json.Marshal(m); err == nil {
		_ = json.Unmarshal(data, &a)
	}
	return a
}

func (r *EntRepository) CreateMacro(ctx context.Context, tenantID uuid.UUID, m *Macro) error {
	row, err := r.client.Macro.Create().
		SetID(m.ID).SetTenantID(tenantID).SetName(m.Name).SetBody(m.Body).
		SetActions(actionsToMap(m.Actions)).SetIsActive(m.IsActive).SetCreatedBy(m.CreatedBy).
		Save(ctx)
	if ent.IsConstraintError(err) {
		return ErrMacroExists
	}
	if err != nil {
		return err
	}
	m.CreatedAt, m.UpdatedAt = row.CreatedAt, row.UpdatedAt
	return nil
}

func (r *EntRepository) GetMacro(ctx context.Context, tenantID uuid.UUID, macroID uuid.UUID) (*Macro, error) {
	row, err := r.client.Macro.Query().Where(macro.ID(macroID), macro.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrMacroNotFound
	}
	if err != nil {
		return nil, err
	}
	return entMacroToModel(row), nil
}

func (r *EntRepository) ListMacros(ctx context.Context, tenantID uuid.UUID, activeOnly bool, limit int) ([]*Macro, error) {
	query := r.client.Macro.Query().Where(macro.TenantID(tenantID))
	if activeOnly {
		query = query.Where(macro.IsActive(true))
	}
	rows, err := query.Order(ent.Asc(macro.FieldName)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*Macro, len(rows))
	for i, row := range rows {
		out[i] = entMacroToModel(row)
	}
	return out, nil
}

func (r *EntRepository) UpdateMacro(ctx context.Context, tenantID uuid.UUID, m *Macro) error {
	n, err := r.client.Macro.Update().
		Where(macro.ID(m.ID), macro.TenantID(tenantID)).
		SetName(m.Name).SetBody(m.Body).SetActions(actionsToMap(m.Actions)).SetIsActive(m.IsActive).
		Save(ctx)
	switch {
	case ent.IsConstraintError(err):
		return ErrMacroExists
	case err == nil && n == 0:
		return ErrMacroNotFound
	}
	return err
}

func (r *EntRepository) DeleteMacro(ctx context.Context, tenantID uuid.UUID, macroID uuid.UUID) error {
	n, err := r.client.Macro.Delete().Where(macro.ID(macroID), macro.TenantID(tenantID)).Exec(ctx)
	if err == nil && n == 0 {
		err = ErrMacroNotFound
	}
	return err
}

func entMacroToModel(row *ent.Macro) *Macro {
	return &Macro{
		ID: row.ID, TenantID: row.TenantID, Name: row.Name, Body: row.Body, Actions: actionsFromMap(row.Actions),
		IsActive: row.IsActive, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// Links

func (r *EntRepository) CreateLink(ctx context.Context, tenantID uuid.UUID, l *TicketLink) error {
	row, err := r.client.TicketLink.Create().
		SetID(l.ID).SetTenantID(tenantID).SetFromTicketID(l.FromTicketID).SetToTicketID(l.ToTicketID).
		SetKind(ticketlink.Kind(l.Kind)).SetCreatedBy(l.CreatedBy).
		Save(ctx)
	if ent.IsConstraintError(err) {
		return ErrLinkExists
	}
	if err != nil {
		return err
	}
	l.CreatedAt = row.CreatedAt
	return nil
}

func (r *EntRepository) ListLinks(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, limit int) ([]*TicketLink, error) {
	rows, err := r.client.TicketLink.Query().
		Where(ticketlink.TenantID(tenantID), ticketlink.Or(ticketlink.FromTicketID(ticketID), ticketlink.ToTicketID(ticketID))).
		Order(ent.Asc(ticketlink.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*TicketLink, len(rows))
	for i, row := range rows {
		out[i] = &TicketLink{
			ID: row.ID, TenantID: row.TenantID, FromTicketID: row.FromTicketID, ToTicketID: row.ToTicketID,
			Kind: LinkKind(row.Kind), CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
		}
	}
	return out, nil
}

func (r *EntRepository) DeleteLink(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, linkID uuid.UUID) error {
	n, err := r.client.TicketLink.Delete().
		Where(ticketlink.ID(linkID), ticketlink.TenantID(tenantID),
			ticketlink.Or(ticketlink.FromTicketID(ticketID), ticketlink.ToTicketID(ticketID))).
		Exec(ctx)
	if err == nil && n == 0 {
		err = ErrLinkNotFound
	}
	return err
}
