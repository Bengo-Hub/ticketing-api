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
			// A paused clock cannot breach; its deadlines move when it resumes.
			ticket.SLAPausedAtIsNil(),
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
	m := &Ticket{
		ID:                      t.ID,
		TenantID:                t.TenantID,
		TicketNumber:            t.TicketNumber,
		CustomerID:              t.CustomerID,
		CategoryID:              t.CategoryID,
		AssignedAgentID:         t.AssignedAgentID,
		AssignedTeamID:          t.AssignedTeamID,
		Subject:                 t.Subject,
		Description:             t.Description,
		Status:                  TicketStatus(t.Status),
		Priority:                TicketPriority(t.Priority),
		Channel:                 TicketChannel(t.Channel),
		OrderID:                 t.OrderID,
		ExternalReference:       t.ExternalReference,
		CRMContactID:            t.CrmContactID,
		CRMLeadID:               t.CrmLeadID,
		SLAResponseDueAt:        t.SLAResponseDueAt,
		SLAResolutionDueAt:      t.SLAResolutionDueAt,
		SLAResponseBreached:     t.SLAResponseBreached,
		SLAResolutionBreached:   t.SLAResolutionBreached,
		SLAPolicyID:             t.SLAPolicyID,
		SLAPausedAt:             t.SLAPausedAt,
		SLAPausedMinutes:        t.SLAPausedMinutes,
		SLAFirstResponseMinutes: t.SLAFirstResponseMinutes,
		SLAResolutionMinutes:    t.SLAResolutionMinutes,
		FirstResponseAt:         t.FirstResponseAt,
		ResolvedAt:              t.ResolvedAt,
		ClosedAt:                t.ClosedAt,
		ReopenedAt:              t.ReopenedAt,
		EscalationLevel:         t.EscalationLevel,
		EscalatedAt:             t.EscalatedAt,
		Tags:                    t.Tags,
		Metadata:                t.Metadata,
		CreatedAt:               t.CreatedAt,
		UpdatedAt:               t.UpdatedAt,
	}
	m.WhatsAppWindowExpiresAt = WhatsAppWindowExpiresAt(m)
	return m
}
