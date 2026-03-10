package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TicketCategory holds the schema definition for the TicketCategory entity.
// Categories help organize and route tickets.
type TicketCategory struct {
	ent.Schema
}

// Fields of the TicketCategory.
func (TicketCategory) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.UUID("parent_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Parent category for hierarchical structure"),
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Comment("Category name"),
		field.String("slug").
			MaxLen(100).
			NotEmpty().
			Comment("URL-friendly identifier"),
		field.Text("description").
			Optional().
			Comment("Category description"),
		field.String("icon").
			MaxLen(50).
			Optional().
			Comment("Icon identifier"),
		field.Int("sort_order").
			Default(0).
			Comment("Display order"),
		field.Bool("is_active").
			Default(true).
			Comment("Whether category is active"),
		field.UUID("default_assignee_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Default agent/team to assign tickets in this category"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the TicketCategory.
func (TicketCategory) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("tickets", Ticket.Type),
		edge.To("children", TicketCategory.Type).
			From("parent").
			Unique().
			Field("parent_id"),
	}
}

// Annotations of the TicketCategory.
func (TicketCategory) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "ticket_categories",
		},
	}
}

// Indexes of the TicketCategory.
func (TicketCategory) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "slug").Unique(),
		index.Fields("tenant_id", "is_active"),
		index.Fields("parent_id"),
		index.Fields("sort_order"),
	}
}
