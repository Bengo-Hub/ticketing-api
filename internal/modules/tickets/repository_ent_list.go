package tickets

import (
	"context"
	"database/sql"
	"encoding/json"

	entsql "entgo.io/ent/dialect/sql"
	pagination "github.com/Bengo-Hub/pagination"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/predicate"
	"github.com/bengobox/ticketing-service/internal/ent/ticket"
)

// sqlQuerier is the part of *sql.DB and *sql.Tx the hand-written queries need.
type sqlQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// q is the transaction when the repository is bound to one, else the pool.
func (r *EntRepository) q() sqlQuerier {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

// afterRow is the keyset condition (created_at, id) past the cursor, as one row comparison so
// the (…, created_at, id) indexes serve it.
func afterRow(s *entsql.Selector, createdCol, idCol string, c *Cursor, asc bool) *entsql.Predicate {
	op := " < ("
	if asc {
		op = " > ("
	}
	return entsql.P(func(b *entsql.Builder) {
		b.WriteString("(").WriteString(s.C(createdCol)).WriteString(", ").WriteString(s.C(idCol)).WriteString(")").
			WriteString(op).Arg(c.CreatedAt).WriteString(", ").Arg(c.ID).WriteString(")")
	})
}

// ilike is col ILIKE pattern; the pattern is escaped by PlanSearch.
func ilike(col, pattern string) *entsql.Predicate {
	return entsql.P(func(b *entsql.Builder) {
		b.WriteString(col).WriteString(" ILIKE ").Arg(pattern)
	})
}

// ticketSearch matches the ticket number exactly, and subject, description and requester by
// ILIKE through their trigram indexes (by prefix, without the description, for a short term).
func ticketSearch(plan SearchPlan) predicate.Ticket {
	return func(s *entsql.Selector) {
		preds := []*entsql.Predicate{
			entsql.EQ(s.C(ticket.FieldTicketNumber), plan.Number),
			ilike(s.C(ticket.FieldSubject), plan.Pattern),
			ilike(requesterSearchExpr, plan.Pattern),
		}
		if !plan.Prefix {
			preds = append(preds, ilike(s.C(ticket.FieldDescription), plan.Pattern))
		}
		s.Where(entsql.Or(preds...))
	}
}

// ticketHasTags is tags @> '[...]': the ticket carries every tag (GIN ticket_tags_gin).
func ticketHasTags(tags []string) predicate.Ticket {
	data, _ := json.Marshal(tags)
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString(s.C(ticket.FieldTags)).WriteString(" @> ").Arg(string(data)).WriteString("::jsonb")
		}))
	}
}

func (r *EntRepository) ListTickets(ctx context.Context, tenantID uuid.UUID, filter TicketFilter) ([]*Ticket, error) {
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
	if len(filter.Tags) > 0 {
		query = query.Where(ticketHasTags(filter.Tags))
	}
	if plan, ok := PlanSearch(filter.Search); ok {
		query = query.Where(ticketSearch(plan))
	}
	asc := filter.Sort == SortCreatedAsc
	if c := filter.Page.After; c != nil {
		query = query.Where(func(s *entsql.Selector) {
			s.Where(afterRow(s, ticket.FieldCreatedAt, ticket.FieldID, c, asc))
		})
	}
	order := ent.Desc
	if asc {
		order = ent.Asc
	}
	rows, err := query.
		Order(order(ticket.FieldCreatedAt), order(ticket.FieldID)).
		Limit(filter.Page.Limit + 1).
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

// tagScanWindow bounds the tag list to the tenant's most recent tickets, so it stays fast as
// tickets accumulate (served by ticket_tenant_id_created_at_id).
const tagScanWindow = 5000

const listTagsSQL = `SELECT tag, count(*) AS n
FROM (
	SELECT tags FROM tickets
	WHERE tenant_id = $1 AND deleted_at IS NULL AND tags IS NOT NULL AND jsonb_typeof(tags) = 'array'
	ORDER BY created_at DESC, id DESC
	LIMIT $2
) recent, jsonb_array_elements_text(recent.tags) AS tag
WHERE $3 = '' OR tag LIKE $3 || '%'
GROUP BY tag
ORDER BY n DESC, tag
LIMIT $4`

func (r *EntRepository) ListTags(ctx context.Context, tenantID uuid.UUID, prefix string, limit int) ([]TagCount, error) {
	rows, err := r.q().QueryContext(ctx, listTagsSQL, tenantID, tagScanWindow, pagination.EscapeLike(prefix), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}
