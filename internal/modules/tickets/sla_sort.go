package tickets

import (
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/bengobox/ticketing-service/internal/ent/ticket"
)

// The SLA due order (sort=sla_due).
//
// A ticket's SLA due time is the nearer of its open targets: the response target while the
// ticket has no first response, and the resolution target. The order is
//
//	rank 0: running clock with a due time, soonest first
//	rank 1: running clock with no target
//	rank 2: paused clock with a due time, soonest first
//	rank 3: paused clock with no target
//
// with id breaking ties. The key of a ticket without a due time is the Unix epoch, so the row
// comparison never meets a NULL. The ticket_tenant_sla_due expression index on
// (tenant_id, rank, key, id) WHERE deleted_at IS NULL serves the order and the keyset condition;
// ent cannot declare an expression index, so it lives only in the versioned migration, and the
// SQL below must stay exactly in step with it:
//
//	CREATE INDEX "ticket_tenant_sla_due" ON "tickets" ("tenant_id",
//	  ((CASE WHEN "sla_paused_at" IS NULL THEN 0 ELSE 2 END) + (CASE WHEN LEAST((CASE WHEN "first_response_at" IS NULL THEN "sla_response_due_at" END), "sla_resolution_due_at") IS NULL THEN 1 ELSE 0 END)),
//	  (COALESCE(LEAST((CASE WHEN "first_response_at" IS NULL THEN "sla_response_due_at" END), "sla_resolution_due_at"), '1970-01-01 00:00:00+00'::timestamptz)),
//	  "id") WHERE "deleted_at" IS NULL;

// maxSLARank is the largest SLA rank (paused, no target).
const maxSLARank = 3

// slaEpoch is the due key of a ticket with no open target.
var slaEpoch = time.Unix(0, 0).UTC()

// slaDueSQL is the nearer open target of the row, NULL when it has none.
func slaDueSQL(s *entsql.Selector) string {
	return "LEAST((CASE WHEN " + s.C(ticket.FieldFirstResponseAt) + " IS NULL THEN " + s.C(ticket.FieldSLAResponseDueAt) +
		" END), " + s.C(ticket.FieldSLAResolutionDueAt) + ")"
}

// slaRankSQL is the rank expression of the index.
func slaRankSQL(s *entsql.Selector) string {
	return "((CASE WHEN " + s.C(ticket.FieldSLAPausedAt) + " IS NULL THEN 0 ELSE 2 END) + (CASE WHEN " + slaDueSQL(s) +
		" IS NULL THEN 1 ELSE 0 END))"
}

// slaKeySQL is the due key expression of the index.
func slaKeySQL(s *entsql.Selector) string {
	return "(COALESCE(" + slaDueSQL(s) + ", '1970-01-01 00:00:00+00'::timestamptz))"
}

// orderBySLADue orders the selection by (rank, key, id), all ascending.
func orderBySLADue(s *entsql.Selector) {
	s.OrderExpr(entsql.Expr(strings.Join([]string{slaRankSQL(s), slaKeySQL(s), s.C(ticket.FieldID)}, ", ")))
}

// afterSLARow is the keyset condition (rank, key, id) past the cursor as one row comparison, so
// ticket_tenant_sla_due serves it.
func afterSLARow(s *entsql.Selector, c *Cursor) *entsql.Predicate {
	return entsql.P(func(b *entsql.Builder) {
		b.WriteString("(").WriteString(slaRankSQL(s)).WriteString(", ").WriteString(slaKeySQL(s)).WriteString(", ").
			WriteString(s.C(ticket.FieldID)).WriteString(") > (").
			Arg(c.Rank).WriteString(", ").Arg(c.CreatedAt).WriteString(", ").Arg(c.ID).WriteString(")")
	})
}

// SLADueAt is the ticket's nearer open SLA target: the response target until the first response,
// and the resolution target. Nil when it has neither.
func SLADueAt(t *Ticket) *time.Time {
	var due *time.Time
	if t.FirstResponseAt == nil {
		due = t.SLAResponseDueAt
	}
	if r := t.SLAResolutionDueAt; r != nil && (due == nil || r.Before(*due)) {
		due = r
	}
	return due
}

// slaSortKey is the ticket's place in the SLA due order, as the SQL computes it.
func slaSortKey(t *Ticket) (rank int, key time.Time) {
	if t.SLAPausedAt != nil {
		rank = 2
	}
	due := SLADueAt(t)
	if due == nil {
		return rank + 1, slaEpoch
	}
	return rank, due.UTC()
}

// slaCursor is the cursor after t in the SLA due order.
func slaCursor(t *Ticket) string {
	rank, key := slaSortKey(t)
	return RankedCursorOf(rank, key, t.ID)
}

// slaLess reports whether a comes before b in the SLA due order (tests and in-memory sorts).
func slaLess(a, b *Ticket) bool {
	ra, ka := slaSortKey(a)
	rb, kb := slaSortKey(b)
	if ra != rb {
		return ra < rb
	}
	if !ka.Equal(kb) {
		return ka.Before(kb)
	}
	return strings.Compare(a.ID.String(), b.ID.String()) < 0
}
