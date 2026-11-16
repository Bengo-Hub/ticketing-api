package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TicketLink relates two tickets of one tenant. Kind parent means from_ticket is the parent of
// to_ticket; related and duplicate are read in both directions.
type TicketLink struct {
	ent.Schema
}

// Fields of the TicketLink.
func (TicketLink) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.UUID("from_ticket_id", uuid.UUID{}).
			Comment("Source ticket (the parent for kind parent)"),
		field.UUID("to_ticket_id", uuid.UUID{}).
			Comment("Target ticket (the child for kind parent)"),
		field.Enum("kind").
			Values("parent", "related", "duplicate").
			Comment("Link kind"),
		field.UUID("created_by", uuid.UUID{}).
			Comment("auth-api user id of who linked the tickets"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Annotations of the TicketLink.
func (TicketLink) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticket_links"}}
}

// Indexes of the TicketLink.
func (TicketLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "from_ticket_id", "to_ticket_id", "kind").Unique(),
		index.Fields("tenant_id", "to_ticket_id"),
	}
}
