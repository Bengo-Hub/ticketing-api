package events

import (
	"context"
	"database/sql"
	"fmt"

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

// publish writes an event to the outbox for background publishing to NATS.
func (p *Publisher) publish(ctx context.Context, tenantID uuid.UUID, eventType string, data map[string]any) error {
	if p == nil {
		return nil
	}

	event := eventslib.NewEvent(eventType, "ticketing", uuid.New(), tenantID, data)

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

// PublishSLABreach publishes an sla.breach event for a ticket that missed its SLA target.
func (p *Publisher) PublishSLABreach(ctx context.Context, tenantID uuid.UUID, data map[string]any) error {
	eventType := "sla.first_response_breached"
	if t, ok := data["event_type"].(string); ok {
		eventType = t
	}
	return p.publish(ctx, tenantID, eventType, data)
}
