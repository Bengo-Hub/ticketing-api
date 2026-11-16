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

// TicketView is a saved ticket list filter. A private view belongs to one user; a shared view is
// seen by every agent of the tenant and is edited by holders of tickets.manage.
type TicketView struct {
	ent.Schema
}

// Fields of the TicketView.
func (TicketView) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.UUID("owner_user_id", uuid.UUID{}).
			Comment("auth-api user id of the user who created the view"),
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Comment("View name"),
		field.JSON("filters", map[string]any{}).
			Optional().
			Comment("Ticket list filters as the list endpoint takes them (status, priority, agentId, tags, search)"),
		field.String("sort").
			MaxLen(30).
			Default("created_desc").
			Comment("List order: created_desc or created_asc"),
		field.Bool("shared").
			Default(false).
			Comment("Shared with every agent of the tenant"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Annotations of the TicketView.
func (TicketView) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticket_views"}}
}

// Indexes of the TicketView.
func (TicketView) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "owner_user_id"),
		index.Fields("tenant_id", "shared"),
	}
}
