package events

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Publisher handles publishing ticketing events via the transactional outbox pattern.
type Publisher struct {
	repo   eventslib.OutboxRepository
	logger *zap.Logger
}

// NewPublisher creates a new ticketing event publisher backed by the shared-events outbox.
func NewPublisher(sqlDB *sql.DB, logger *zap.Logger) *Publisher {
	return &Publisher{
		repo:   eventslib.NewSQLOutboxRepository(sqlDB),
		logger: logger.Named("ticketing.events"),
	}
}

// OutboxRepo returns the outbox repository for use by the background publisher.
func (p *Publisher) OutboxRepo() eventslib.OutboxRepository {
	return p.repo
}

// publish writes a ticketing-aggregate event to the outbox for background publishing.
func (p *Publisher) publish(ctx context.Context, tenantID uuid.UUID, eventType string, data map[string]any) error {
	return p.publishWithAggregate(ctx, tenantID, "ticketing", eventType, data)
}

// publishWithAggregate writes an event under an explicit aggregate so a cross-domain event
// (crm.*) gets the subject its own stream and consumer listen on ({aggregate}.{event_type})
// instead of ticketing.crm.*.
func (p *Publisher) publishWithAggregate(ctx context.Context, tenantID uuid.UUID, aggregate, eventType string, data map[string]any) error {
	if p == nil {
		return nil
	}

	event := eventslib.NewEvent(eventType, aggregate, uuid.New(), tenantID, data)

	tx, err := p.repo.BeginTx(ctx)
	if err != nil {
		p.logger.Error("failed to begin tx for event", zap.String("event_type", eventType), zap.Error(err))
		return fmt.Errorf("begin tx: %w", err)
	}

	if err := eventslib.CreateOutboxRecordInTx(ctx, tx, p.repo, event); err != nil {
		_ = tx.Rollback()
		p.logger.Error("failed to write event to outbox", zap.String("event_type", eventType), zap.Error(err))
		return fmt.Errorf("write outbox: %w", err)
	}

	if err := tx.Commit(); err != nil {
		p.logger.Error("failed to commit event", zap.String("event_type", eventType), zap.Error(err))
		return fmt.Errorf("commit: %w", err)
	}

	p.logger.Debug("event written to outbox", zap.String("event_type", eventType))
	return nil
}

// PublishTicketAssigned publishes a ticketing.ticket.assigned event.
func (p *Publisher) PublishTicketAssigned(ctx context.Context, tenantID uuid.UUID, data map[string]any) error {
	return p.publish(ctx, tenantID, "ticket.assigned", data)
}

// PublishTicketResolved publishes a ticketing.ticket.resolved event.
func (p *Publisher) PublishTicketResolved(ctx context.Context, tenantID uuid.UUID, data map[string]any) error {
	return p.publish(ctx, tenantID, "ticket.resolved", data)
}

// PublishCRMActivityRequest publishes a crm.activity.log_requested event for CRM integration.
// The CRM service subscribes to this subject to log contact/lead activity. It is published
// under the crm aggregate (subject "crm.activity.log_requested") with the ticket's real
// tenant id — previously it went out as "ticketing.crm.activity.log_requested" with a nil
// tenant, so the CRM service never received it.
func (p *Publisher) PublishCRMActivityRequest(ctx context.Context, tenantID uuid.UUID, data map[string]any) error {
	return p.publishWithAggregate(ctx, tenantID, "crm", "activity.log_requested", data)
}

// PublishSLABreach publishes a ticket's SLA breach on the ticketing stream, subject
// "ticketing.sla.first_response_breached" or "ticketing.sla.resolution_breached", event type
// "sla.<kind>". No stream captures a bare "sla.>" subject, so the earlier "sla" aggregate meant
// notifications-api's ticketing consumer (ticketing.>) never received a breach alert.
func (p *Publisher) PublishSLABreach(ctx context.Context, tenantID uuid.UUID, data map[string]any) error {
	return p.publish(ctx, tenantID, slaBreachEventType(data), data)
}

// slaBreachEventType is "sla.first_response_breached" or "sla.resolution_breached" from the
// payload's event_type, with or without its "sla." prefix; first response is the default.
func slaBreachEventType(data map[string]any) string {
	kind := "first_response_breached"
	if t, ok := data["event_type"].(string); ok && t != "" {
		kind = strings.TrimPrefix(t, "sla.")
	}
	return "sla." + kind
}
