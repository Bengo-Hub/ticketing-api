package tickets

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	entagent "github.com/bengobox/ticketing-service/internal/ent/agent"
	"github.com/bengobox/ticketing-service/internal/ent/predicate"
	"github.com/bengobox/ticketing-service/internal/ent/ticket"
)

func (r *EntRepository) GetTicketCountsByStatus(ctx context.Context, tenantID uuid.UUID) (map[TicketStatus]int, error) {
	var rows []struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	err := r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.DeletedAtIsNil()).
		GroupBy(ticket.FieldStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	counts := make(map[TicketStatus]int, len(rows))
	for _, row := range rows {
		counts[TicketStatus(row.Status)] = row.Count
	}
	return counts, nil
}

func (r *EntRepository) GetTicketCountsByPriority(ctx context.Context, tenantID uuid.UUID) (map[TicketPriority]int, error) {
	var rows []struct {
		Priority string `json:"priority"`
		Count    int    `json:"count"`
	}
	err := r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.DeletedAtIsNil()).
		GroupBy(ticket.FieldPriority).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	counts := make(map[TicketPriority]int, len(rows))
	for _, row := range rows {
		counts[TicketPriority(row.Priority)] = row.Count
	}
	return counts, nil
}

func (r *EntRepository) GetTicketTrend(ctx context.Context, tenantID uuid.UUID, from, to time.Time, bucketDays int) ([]TrendPoint, error) {
	rows, err := r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.DeletedAtIsNil(), ticket.CreatedAtGTE(from), ticket.CreatedAtLTE(to)).
		Select(ticket.FieldCreatedAt, ticket.FieldStatus).
		All(ctx)
	if err != nil {
		return nil, err
	}
	type bucket struct{ created, resolved int }
	buckets := make(map[string]*bucket)
	for d := from; !d.After(to); d = d.AddDate(0, 0, bucketDays) {
		buckets[d.Format("2006-01-02")] = &bucket{}
	}
	bucketKey := func(t time.Time) string {
		diff := int(t.Sub(from).Hours() / 24)
		return from.AddDate(0, 0, (diff/bucketDays)*bucketDays).Format("2006-01-02")
	}
	for _, t := range rows {
		if b, ok := buckets[bucketKey(t.CreatedAt)]; ok {
			b.created++
			if t.Status == ticket.StatusResolved || t.Status == ticket.StatusClosed {
				b.resolved++
			}
		}
	}
	var points []TrendPoint
	for d := from; !d.After(to); d = d.AddDate(0, 0, bucketDays) {
		key := d.Format("2006-01-02")
		b := buckets[key]
		points = append(points, TrendPoint{Date: key, Created: b.created, Resolved: b.resolved})
	}
	return points, nil
}

func (r *EntRepository) GetAgentTicketCount(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) (int, error) {
	return r.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			ticket.AssignedAgentID(agentID),
			ticket.DeletedAtIsNil(),
			ticket.StatusNotIn(ticket.StatusClosed, ticket.StatusCancelled, ticket.StatusResolved),
		).
		Count(ctx)
}

// GetAgentEmail returns the agent's email so ticket.assigned events reach the agent directly
// (notifications-api reads payload.agent_email). Returns "" when the agent is not found.
func (r *EntRepository) GetAgentEmail(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) (string, error) {
	return r.agentEmail(ctx, entagent.TenantID(tenantID), entagent.ID(agentID))
}

// GetAgentEmailByUser returns the email of the tenant's agent profile for an auth-api user.
func (r *EntRepository) GetAgentEmailByUser(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (string, error) {
	return r.agentEmail(ctx, entagent.TenantID(tenantID), entagent.UserID(userID))
}

func (r *EntRepository) agentEmail(ctx context.Context, where ...predicate.Agent) (string, error) {
	a, err := r.client.Agent.Query().Where(where...).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return a.Email, nil
}

func (r *EntRepository) ListTicketsPendingBreach(ctx context.Context, now time.Time, limit int) ([]*Ticket, error) {
	rows, err := r.client.Ticket.Query().
		Where(
			ticket.DeletedAtIsNil(),
			ticket.StatusNotIn(ticket.StatusResolved, ticket.StatusClosed, ticket.StatusCancelled),
			ticket.Or(
				ticket.And(ticket.SLAResponseDueAtLTE(now), ticket.SLAResponseBreached(false), ticket.FirstResponseAtIsNil()),
				ticket.And(ticket.SLAResolutionDueAtLTE(now), ticket.SLAResolutionBreached(false)),
			),
		).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*Ticket, len(rows))
	for i, t := range rows {
		result[i] = entTicketToModel(t)
	}
	return result, nil
}

func entTicketToModel(t *ent.Ticket) *Ticket {
	return &Ticket{
		ID:                    t.ID,
		TenantID:              t.TenantID,
		TicketNumber:          t.TicketNumber,
		CustomerID:            t.CustomerID,
		CategoryID:            t.CategoryID,
		AssignedAgentID:       t.AssignedAgentID,
		AssignedTeamID:        t.AssignedTeamID,
		Subject:               t.Subject,
		Description:           t.Description,
		Status:                TicketStatus(t.Status),
		Priority:              TicketPriority(t.Priority),
		Channel:               TicketChannel(t.Channel),
		OrderID:               t.OrderID,
		ExternalReference:     t.ExternalReference,
		CRMContactID:          t.CrmContactID,
		CRMLeadID:             t.CrmLeadID,
		SLAResponseDueAt:      t.SLAResponseDueAt,
		SLAResolutionDueAt:    t.SLAResolutionDueAt,
		SLAResponseBreached:   t.SLAResponseBreached,
		SLAResolutionBreached: t.SLAResolutionBreached,
		FirstResponseAt:       t.FirstResponseAt,
		ResolvedAt:            t.ResolvedAt,
		ClosedAt:              t.ClosedAt,
		ReopenedAt:            t.ReopenedAt,
		EscalationLevel:       t.EscalationLevel,
		EscalatedAt:           t.EscalatedAt,
		Tags:                  t.Tags,
		Metadata:              t.Metadata,
		CreatedAt:             t.CreatedAt,
		UpdatedAt:             t.UpdatedAt,
	}
}
