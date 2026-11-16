package events

import (
	"context"
	"database/sql"
	"fmt"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
)

// Outbox writes ticketing events to the shared-events outbox inside the caller's transaction.
// The shared background publisher (eventslib.Publisher over OutboxRepo) relays them to NATS.
type Outbox struct {
	repo eventslib.OutboxRepository
}

// NewOutbox creates the outbox writer over the service database.
func NewOutbox(sqlDB *sql.DB) *Outbox {
	return &Outbox{repo: eventslib.NewSQLOutboxRepository(sqlDB)}
}

// OutboxRepo returns the outbox repository for the background publisher.
func (o *Outbox) OutboxRepo() eventslib.OutboxRepository {
	return o.repo
}

// WriteInTx stores one event in tx with eventslib.CreateOutboxRecordInTx. The event's aggregate id
// is the payload's ticket_id when it has one, so every event of a ticket shares one identity.
func (o *Outbox) WriteInTx(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, aggregate, eventType string, data map[string]any) error {
	aggregateID := uuid.New()
	if s, ok := data["ticket_id"].(string); ok {
		if id, err := uuid.Parse(s); err == nil {
			aggregateID = id
		}
	}
	event := eventslib.NewEvent(eventType, aggregate, aggregateID, tenantID, data)
	if err := eventslib.CreateOutboxRecordInTx(ctx, tx, o.repo, event); err != nil {
		return fmt.Errorf("write %s.%s to outbox: %w", aggregate, eventType, err)
	}
	return nil
}
