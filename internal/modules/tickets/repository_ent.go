package tickets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/predicate"
	"github.com/bengobox/ticketing-service/internal/ent/ticket"
)

// OutboxWriter stores an event in the caller's transaction (events.Outbox).
type OutboxWriter interface {
	WriteInTx(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, aggregate, eventType string, data map[string]any) error
}

// rowQuerier is the part of *sql.DB and *sql.Tx the sequence allocation needs.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// EntRepository implements Repository using Ent. A repository bound to a transaction (tx set)
// runs every statement, the ticket-number allocation and the outbox writes in that transaction.
type EntRepository struct {
	client *ent.Client
	db     *sql.DB
	outbox OutboxWriter
	tx     *sql.Tx
}

// NewEntRepository creates a new EntRepository. client and db share one connection pool.
func NewEntRepository(client *ent.Client, db *sql.DB, outbox OutboxWriter) *EntRepository {
	return &EntRepository{client: client, db: db, outbox: outbox}
}

// InTx implements Repository.InTx. Called on a repository already in a transaction, fn joins it.
func (r *EntRepository) InTx(ctx context.Context, tenantID uuid.UUID, fn func(repo Repository, out *Outbox) error) error {
	if r.tx != nil {
		out := &Outbox{}
		if err := fn(r, out); err != nil {
			return err
		}
		return r.writeOutbox(ctx, r.tx, tenantID, out)
	}
	sqlTx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	bound := &EntRepository{
		client: ent.NewClient(ent.Driver(entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: sqlTx}))),
		db:     r.db,
		outbox: r.outbox,
		tx:     sqlTx,
	}
	out := &Outbox{}
	if err := fn(bound, out); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err := r.writeOutbox(ctx, sqlTx, tenantID, out); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	return sqlTx.Commit()
}

func (r *EntRepository) writeOutbox(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, out *Outbox) error {
	if r.outbox == nil {
		if len(out.Events) > 0 {
			return errors.New("outbox writer not configured")
		}
		return nil
	}
	for _, e := range out.Events {
		if err := r.outbox.WriteInTx(ctx, tx, tenantID, e.Aggregate, e.Type, e.Data); err != nil {
			return err
		}
	}
	return nil
}

// allocateTicketNumberSQL claims the tenant's next number in one statement. The row lock it takes
// is held until the surrounding transaction ends, so concurrent creates queue on it and never
// read the same value. A rolled-back create leaves a gap, never a duplicate.
const allocateTicketNumberSQL = `INSERT INTO ticket_sequences (id, tenant_id, next_value, updated_at)
VALUES ($1, $2, 1, now())
ON CONFLICT (tenant_id) DO UPDATE SET next_value = ticket_sequences.next_value + 1, updated_at = now()
RETURNING next_value`

// FormatTicketNumber renders a sequence value as a ticket number (TKT-000042).
func FormatTicketNumber(n int64) string { return fmt.Sprintf("TKT-%06d", n) }

func (r *EntRepository) NextTicketNumber(ctx context.Context, tenantID uuid.UUID) (string, error) {
	var q rowQuerier = r.db
	if r.tx != nil {
		q = r.tx
	}
	var n int64
	if err := q.QueryRowContext(ctx, allocateTicketNumberSQL, uuid.New(), tenantID).Scan(&n); err != nil {
		return "", fmt.Errorf("allocate ticket number: %w", err)
	}
	return FormatTicketNumber(n), nil
}

func (r *EntRepository) CreateTicket(ctx context.Context, tenantID uuid.UUID, t *Ticket) error {
	create := r.client.Ticket.Create().
		SetID(t.ID).
		SetTenantID(tenantID).
		SetTicketNumber(t.TicketNumber).
		SetCustomerID(t.CustomerID).
		SetSubject(t.Subject).
		SetDescription(t.Description).
		SetStatus(ticket.Status(t.Status)).
		SetPriority(ticket.Priority(t.Priority)).
		SetChannel(ticket.Channel(t.Channel)).
		SetNillableCategoryID(t.CategoryID).
		SetNillableAssignedAgentID(t.AssignedAgentID).
		SetNillableAssignedTeamID(t.AssignedTeamID).
		SetNillableOrderID(t.OrderID).
		SetNillableCrmContactID(t.CRMContactID).
		SetNillableCrmLeadID(t.CRMLeadID).
		SetNillableSLAResponseDueAt(t.SLAResponseDueAt).
		SetNillableSLAResolutionDueAt(t.SLAResolutionDueAt)
	if t.ExternalReference != "" {
		create = create.SetExternalReference(t.ExternalReference)
	}
	if t.Tags != nil {
		create = create.SetTags(t.Tags)
	}
	if t.Metadata != nil {
		create = create.SetMetadata(t.Metadata)
	}
	created, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) && strings.Contains(err.Error(), "ticket_tenant_source_ref") {
			return ErrDuplicateSourceRef
		}
		return err
	}
	t.ID = created.ID
	t.CreatedAt = created.CreatedAt
	t.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *EntRepository) GetTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) (*Ticket, error) {
	return r.oneTicket(ctx, ticket.ID(ticketID), ticket.TenantID(tenantID), ticket.DeletedAtIsNil())
}

func (r *EntRepository) GetTicketByNumber(ctx context.Context, tenantID uuid.UUID, ticketNumber string) (*Ticket, error) {
	return r.oneTicket(ctx, ticket.TenantID(tenantID), ticket.TicketNumber(ticketNumber), ticket.DeletedAtIsNil())
}

// FindTicketBySourceRef matches the ticket_tenant_source_ref expression index. A soft-deleted
// ticket still counts, so a repeat never creates a second ticket for the same source record.
func (r *EntRepository) FindTicketBySourceRef(ctx context.Context, tenantID uuid.UUID, source string, ref SourceRef) (*Ticket, error) {
	return r.oneTicket(ctx,
		ticket.TenantID(tenantID),
		metadataTextEQ("->> 'source'", source),
		metadataTextEQ("-> 'source_ref' ->> 'type'", ref.Type),
		metadataTextEQ("-> 'source_ref' ->> 'id'", ref.ID),
	)
}

// metadataTextEQ compares one text value inside tickets.metadata. It is written in the same form
// as the ticket_tenant_source_ref expression index (metadata ->> ... as text) so Postgres can use
// that index; sqljson.ValueEQ compares jsonb values and would not.
func metadataTextEQ(path, value string) predicate.Ticket {
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("(").WriteString(s.C(ticket.FieldMetadata)).WriteString(" " + path + ") = ").Arg(value)
		}))
	}
}

func (r *EntRepository) oneTicket(ctx context.Context, where ...predicate.Ticket) (*Ticket, error) {
	t, err := r.client.Ticket.Query().Where(where...).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}
	return entTicketToModel(t), nil
}

func (r *EntRepository) ListTickets(ctx context.Context, tenantID uuid.UUID, filter TicketFilter) ([]*Ticket, int, error) {
	query := r.client.Ticket.Query().Where(ticket.TenantID(tenantID), ticket.DeletedAtIsNil())
	if filter.Status != nil {
		query = query.Where(ticket.StatusEQ(ticket.Status(*filter.Status)))
	}
	if len(filter.Statuses) > 0 {
		statuses := make([]ticket.Status, len(filter.Statuses))
		for i, s := range filter.Statuses {
			statuses[i] = ticket.Status(s)
		}
		query = query.Where(ticket.StatusIn(statuses...))
	}
	if filter.Priority != nil {
		query = query.Where(ticket.PriorityEQ(ticket.Priority(*filter.Priority)))
	}
	if filter.CustomerID != nil {
		query = query.Where(ticket.CustomerID(*filter.CustomerID))
	}
	if filter.AssignedAgentID != nil {
		query = query.Where(ticket.AssignedAgentID(*filter.AssignedAgentID))
	}
	if filter.AssignedTeamID != nil {
		query = query.Where(ticket.AssignedTeamID(*filter.AssignedTeamID))
	}
	if filter.CategoryID != nil {
		query = query.Where(ticket.CategoryID(*filter.CategoryID))
	}
	if filter.OrderID != nil {
		query = query.Where(ticket.OrderID(*filter.OrderID))
	}
	if filter.Channel != nil {
		query = query.Where(ticket.ChannelEQ(ticket.Channel(*filter.Channel)))
	}
	if filter.CreatedAfter != nil {
		query = query.Where(ticket.CreatedAtGTE(*filter.CreatedAfter))
	}
	if filter.CreatedBefore != nil {
		query = query.Where(ticket.CreatedAtLTE(*filter.CreatedBefore))
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
	rows, err := query.Order(ent.Desc(ticket.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	result := make([]*Ticket, len(rows))
	for i, t := range rows {
		result[i] = entTicketToModel(t)
	}
	return result, total, nil
}

// updateOpen starts an update of one live ticket in the tenant.
func (r *EntRepository) updateOpen(tenantID, ticketID uuid.UUID) *ent.TicketUpdate {
	return r.client.Ticket.Update().Where(ticket.ID(ticketID), ticket.TenantID(tenantID), ticket.DeletedAtIsNil())
}

func savedOne(n int, err error) error {
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTicketNotFound
	}
	return nil
}

func (r *EntRepository) UpdateTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, updates *UpdateTicketRequest) error {
	update := r.updateOpen(tenantID, ticketID)
	if updates.CategoryID != nil {
		update = update.SetCategoryID(*updates.CategoryID)
	}
	if updates.AssignedAgentID != nil {
		update = update.SetAssignedAgentID(*updates.AssignedAgentID)
	}
	if updates.AssignedTeamID != nil {
		update = update.SetAssignedTeamID(*updates.AssignedTeamID)
	}
	if updates.Subject != nil {
		update = update.SetSubject(*updates.Subject)
	}
	if updates.Priority != nil {
		update = update.SetPriority(ticket.Priority(*updates.Priority))
	}
	if updates.Tags != nil {
		update = update.SetTags(updates.Tags)
	}
	if updates.Metadata != nil {
		update = update.SetMetadata(updates.Metadata)
	}
	return savedOne(update.Save(ctx))
}

func (r *EntRepository) UpdateTicketStatus(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, status TicketStatus) error {
	now := time.Now()
	update := r.updateOpen(tenantID, ticketID).SetStatus(ticket.Status(status))
	switch status {
	case StatusResolved:
		update = update.SetResolvedAt(now)
	case StatusClosed:
		update = update.SetClosedAt(now)
	case StatusOpen, StatusInProgress:
		// Reopening a resolved or closed ticket records when.
		if t, err := r.GetTicket(ctx, tenantID, ticketID); err == nil && (t.Status == StatusResolved || t.Status == StatusClosed) {
			update = update.SetReopenedAt(now)
		}
	}
	return savedOne(update.Save(ctx))
}

func (r *EntRepository) AssignTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, agentID *uuid.UUID, teamID *uuid.UUID) error {
	update := r.updateOpen(tenantID, ticketID)
	if agentID != nil {
		update = update.SetAssignedAgentID(*agentID)
	} else {
		update = update.ClearAssignedAgentID()
	}
	if teamID != nil {
		update = update.SetAssignedTeamID(*teamID)
	} else {
		update = update.ClearAssignedTeamID()
	}
	return savedOne(update.Save(ctx))
}

func (r *EntRepository) DeleteTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) error {
	return savedOne(r.updateOpen(tenantID, ticketID).SetDeletedAt(time.Now()).Save(ctx))
}

func (r *EntRepository) MarkFirstResponse(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) error {
	// A ticket that already has a first response is left unchanged.
	_, err := r.client.Ticket.Update().
		Where(ticket.ID(ticketID), ticket.TenantID(tenantID), ticket.FirstResponseAtIsNil()).
		SetFirstResponseAt(time.Now()).
		Save(ctx)
	return err
}

func (r *EntRepository) MarkSLABreached(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, responseBreached, resolutionBreached bool) (bool, error) {
	// Only a flag that is still false flips, and changed reports whether this call did it, so a
	// breach is acted on once even when the worker and the status-change paths both detect it.
	var unmarked []predicate.Ticket
	if responseBreached {
		unmarked = append(unmarked, ticket.SLAResponseBreached(false))
	}
	if resolutionBreached {
		unmarked = append(unmarked, ticket.SLAResolutionBreached(false))
	}
	if len(unmarked) == 0 {
		return false, nil
	}
	update := r.client.Ticket.Update().
		Where(ticket.ID(ticketID), ticket.TenantID(tenantID), ticket.Or(unmarked...))
	if responseBreached {
		update = update.SetSLAResponseBreached(true)
	}
	if resolutionBreached {
		update = update.SetSLAResolutionBreached(true)
	}
	n, err := update.Save(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
