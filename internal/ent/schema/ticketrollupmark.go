package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// TicketRollupMark records the hours a ticket counted in when the rollup last saw it. A deadline
// that moves (a paused SLA clock resumes) or a resolution that moves (a reopened ticket is
// resolved again) leaves the ticket's old hour behind; the worker recomputes the old hours from
// here as well as the new ones, so no cell keeps a ticket that has left it. updated_at holds the
// ticket's updated_at the rollup counted (the rollup writes it, not the default), which its
// overlap re-read compares with the ticket's current updated_at.
type TicketRollupMark struct {
	ent.Schema
}

// Fields of the TicketRollupMark.
func (TicketRollupMark) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Immutable().
			Comment("The ticket id"),
		field.UUID("tenant_id", uuid.UUID{}).
			Immutable(),
		field.JSON("hours", []time.Time{}).
			Comment("UTC hour starts the ticket counted in at the last rollup; empty for a deleted ticket"),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Annotations of the TicketRollupMark.
func (TicketRollupMark) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticket_rollup_marks"}}
}
