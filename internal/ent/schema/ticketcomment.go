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

// TicketComment holds the schema definition for the TicketComment entity.
// Comments on tickets including public, internal, and system messages.
type TicketComment struct {
	ent.Schema
}

// Fields of the TicketComment.
func (TicketComment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("ticket_id", uuid.UUID{}).
			Comment("Reference to ticket"),
		field.UUID("author_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Author user ID (null for system comments)"),
		field.Enum("author_type").
			Values("customer", "agent", "system").
			Default("agent").
			Comment("Type of author"),
		field.Enum("comment_type").
			Values("public", "internal", "system").
			Default("public").
			Comment("Comment visibility type"),
		field.Text("content").
			NotEmpty().
			Comment("Comment content (supports markdown)"),
		field.Text("content_html").
			Optional().
			Comment("Rendered HTML content"),
		field.Bool("is_edited").
			Default(false).
			Comment("Whether comment has been edited"),
		field.Time("edited_at").
			Optional().
			Nillable().
			Comment("Last edit timestamp"),
		field.JSON("mentions", []uuid.UUID{}).
			Optional().
			Comment("User IDs mentioned in comment"),
		field.JSON("metadata", map[string]any{}).
			Optional().
			Comment("Additional comment metadata"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("deleted_at").
			Optional().
			Nillable().
			Comment("Soft delete timestamp"),
	}
}

// Edges of the TicketComment.
func (TicketComment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("ticket", Ticket.Type).
			Ref("comments").
			Unique().
			Required().
			Field("ticket_id"),
	}
}

// Annotations of the TicketComment.
func (TicketComment) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "ticket_comments",
		},
	}
}

// Indexes of the TicketComment.
func (TicketComment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ticket_id", "created_at"),
		index.Fields("ticket_id", "comment_type"),
		index.Fields("author_id"),
		index.Fields("deleted_at"),
	}
}
