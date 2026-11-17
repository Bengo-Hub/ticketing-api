package tickets

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/predicate"
	"github.com/bengobox/ticketing-service/internal/ent/ticket"
	"github.com/bengobox/ticketing-service/internal/ent/ticketcomment"
)

// Lookups behind the channel ingest. Each is written in the same form as the expression index
// that serves it (20261117120000_sprint11.sql), so Postgres can use the index.

// LatestOpenTicketByPhone is served by ticket_tenant_requester_phone (tenant_id, metadata
// requester_phone).
func (r *EntRepository) LatestOpenTicketByPhone(ctx context.Context, tenantID uuid.UUID, channel TicketChannel, phone string, since time.Time) (*Ticket, error) {
	t, err := r.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			metadataTextEQ("->> 'requester_phone'", phone),
			ticket.ChannelEQ(ticket.Channel(channel)),
			ticket.StatusNotIn(ticket.Status(StatusClosed), ticket.Status(StatusCancelled)),
			ticket.UpdatedAtGTE(since),
			ticket.DeletedAtIsNil(),
		).
		Order(ent.Desc(ticket.FieldUpdatedAt)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}
	return entTicketToModel(t), nil
}

// mergeMetadataSQL merges a patch into the ticket metadata at its top level, so a concurrent
// change to other keys is never overwritten.
const mergeMetadataSQL = `UPDATE tickets SET metadata = COALESCE(metadata, '{}'::jsonb) || $1::jsonb, updated_at = now()
WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`

// execer is the part of *sql.DB and *sql.Tx a raw statement needs.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (r *EntRepository) MergeTicketMetadata(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, patch map[string]any) error {
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("encode metadata patch: %w", err)
	}
	var q execer = r.db
	if r.tx != nil {
		q = r.tx
	}
	res, err := q.ExecContext(ctx, mergeMetadataSQL, string(body), ticketID, tenantID)
	if err != nil {
		return fmt.Errorf("merge ticket metadata: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrTicketNotFound
	}
	return nil
}

// FindCommentByExternalID is served by ticketcomment_tenant_channel_external_id. A deleted
// comment still counts, so deleting a comment never lets its message in again.
func (r *EntRepository) FindCommentByExternalID(ctx context.Context, tenantID uuid.UUID, channel, externalID string) (*TicketComment, error) {
	c, err := r.client.TicketComment.Query().
		Where(
			ticketcomment.TenantID(tenantID),
			commentMetadataTextEQ("->> 'channel'", channel),
			commentMetadataTextEQ("->> 'external_id'", externalID),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrCommentNotFound
		}
		return nil, err
	}
	return entCommentToModel(c), nil
}

// commentMetadataTextEQ compares one text value inside ticket_comments.metadata, in the form of
// the comment expression index.
func commentMetadataTextEQ(path, value string) predicate.TicketComment {
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("(").WriteString(s.C(ticketcomment.FieldMetadata)).WriteString(" " + path + ") = ").Arg(value)
		}))
	}
}

// isDuplicateMessage reports the unique violation of a channel message stored twice as a comment.
func isDuplicateMessage(err error) bool {
	return ent.IsConstraintError(err) && strings.Contains(err.Error(), "ticketcomment_tenant_channel_external_id")
}
