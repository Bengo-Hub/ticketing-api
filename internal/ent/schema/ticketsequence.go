package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// TicketSequence is the per-tenant counter behind ticket numbers. The ticket-create transaction
// allocates with one INSERT ... ON CONFLICT (tenant_id) DO UPDATE ... RETURNING statement, so two
// creates at the same moment never read the same value.
type TicketSequence struct {
	ent.Schema
}

// Fields of the TicketSequence.
func (TicketSequence) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Unique().
			Immutable().
			Comment("One counter row per tenant"),
		field.Int64("next_value").
			Default(0).
			Comment("The last ticket number issued for the tenant; the next create returns next_value + 1"),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Annotations of the TicketSequence.
func (TicketSequence) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "ticket_sequences"},
	}
}
