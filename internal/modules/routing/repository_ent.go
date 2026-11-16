package routing

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	entpkg "github.com/bengobox/ticketing-service/internal/ent/routingrule"
)

// EntRepository implements Repository using the Ent ORM.
type EntRepository struct {
	client *ent.Client
}

// NewEntRepository creates a new Ent-backed routing repository.
func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client}
}

func (r *EntRepository) Create(ctx context.Context, rule *RoutingRule) error {
	conds := conditionsToRaw(rule.Conditions)
	action := actionToRaw(rule.Action)

	row, err := r.client.RoutingRule.Create().
		SetID(rule.ID).
		SetTenantID(rule.TenantID).
		SetName(rule.Name).
		SetDescription(rule.Description).
		SetIsActive(rule.IsActive).
		SetSortOrder(rule.SortOrder).
		SetConditions(conds).
		SetAction(action).
		Save(ctx)
	if err != nil {
		return err
	}
	rule.CreatedAt = row.CreatedAt
	rule.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *EntRepository) Get(ctx context.Context, tenantID, ruleID uuid.UUID) (*RoutingRule, error) {
	row, err := r.client.RoutingRule.Get(ctx, ruleID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrRuleNotFound
		}
		return nil, err
	}
	if row.TenantID != tenantID {
		return nil, ErrRuleNotFound
	}
	return entToRule(row), nil
}

func (r *EntRepository) List(ctx context.Context, filter RuleFilter) ([]*RoutingRule, int, error) {
	q := r.client.RoutingRule.Query().
		Where(entpkg.TenantID(filter.TenantID))

	if filter.IsActive != nil {
		q = q.Where(entpkg.IsActive(*filter.IsActive))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	rows, err := q.Order(ent.Asc(entpkg.FieldSortOrder)).
		Offset(filter.Offset).
		Limit(filter.Limit).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	rules := make([]*RoutingRule, len(rows))
	for i, row := range rows {
		rules[i] = entToRule(row)
	}
	return rules, total, nil
}

func (r *EntRepository) Update(ctx context.Context, tenantID, ruleID uuid.UUID, req *UpdateRuleRequest) (*RoutingRule, error) {
	existing, err := r.Get(ctx, tenantID, ruleID)
	if err != nil {
		return nil, err
	}

	upd := r.client.RoutingRule.UpdateOneID(existing.ID)

	if req.Name != nil {
		upd = upd.SetName(*req.Name)
	}
	if req.Description != nil {
		upd = upd.SetDescription(*req.Description)
	}
	if req.IsActive != nil {
		upd = upd.SetIsActive(*req.IsActive)
	}
	if req.SortOrder != nil {
		upd = upd.SetSortOrder(*req.SortOrder)
	}
	if req.Conditions != nil {
		upd = upd.SetConditions(conditionsToRaw(req.Conditions))
	}
	if req.Action != nil {
		upd = upd.SetAction(actionToRaw(*req.Action))
	}

	row, err := upd.Save(ctx)
	if err != nil {
		return nil, err
	}
	return entToRule(row), nil
}

func (r *EntRepository) Delete(ctx context.Context, tenantID, ruleID uuid.UUID) error {
	_, err := r.Get(ctx, tenantID, ruleID)
	if err != nil {
		return err
	}
	return r.client.RoutingRule.DeleteOneID(ruleID).Exec(ctx)
}

func (r *EntRepository) ListActive(ctx context.Context, tenantID uuid.UUID, limit int) ([]*RoutingRule, error) {
	rows, err := r.client.RoutingRule.Query().
		Where(entpkg.TenantID(tenantID), entpkg.IsActive(true)).
		Order(ent.Asc(entpkg.FieldSortOrder), ent.Asc(entpkg.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	rules := make([]*RoutingRule, len(rows))
	for i, row := range rows {
		rules[i] = entToRule(row)
	}
	return rules, nil
}

// — helpers —

func entToRule(row *ent.RoutingRule) *RoutingRule {
	return &RoutingRule{
		ID:          row.ID,
		TenantID:    row.TenantID,
		Name:        row.Name,
		Description: row.Description,
		IsActive:    row.IsActive,
		SortOrder:   row.SortOrder,
		Conditions:  rawToConditions(row.Conditions),
		Action:      rawToAction(row.Action),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func conditionsToRaw(conds []Condition) []map[string]any {
	out := make([]map[string]any, len(conds))
	for i, c := range conds {
		b, _ := json.Marshal(c)
		var m map[string]any
		json.Unmarshal(b, &m) //nolint:errcheck
		out[i] = m
	}
	return out
}

func actionToRaw(a Action) map[string]any {
	b, _ := json.Marshal(a)
	var m map[string]any
	json.Unmarshal(b, &m) //nolint:errcheck
	return m
}

func rawToConditions(raw []map[string]any) []Condition {
	out := make([]Condition, 0, len(raw))
	for _, m := range raw {
		b, _ := json.Marshal(m)
		var c Condition
		if err := json.Unmarshal(b, &c); err == nil {
			out = append(out, c)
		}
	}
	return out
}

func rawToAction(raw map[string]any) Action {
	b, _ := json.Marshal(raw)
	var a Action
	_ = json.Unmarshal(b, &a)
	return a
}

// Ensure unused import doesn't cause error.
var _ = errors.New
