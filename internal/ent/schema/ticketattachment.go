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

// TicketAttachment holds the schema definition for the TicketAttachment entity.
// File attachments on tickets.
type TicketAttachment struct {
	ent.Schema
}

// Fields of the TicketAttachment.
func (TicketAttachment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("ticket_id", uuid.UUID{}).
			Comment("Reference to ticket"),
		field.UUID("comment_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Associated comment if any"),
		field.UUID("uploaded_by", uuid.UUID{}).
			Comment("User who uploaded the attachment"),
		field.String("filename").
			MaxLen(255).
			NotEmpty().
			Comment("Original filename"),
		field.String("storage_key").
			MaxLen(500).
			NotEmpty().
			Comment("Storage key/path in S3"),
		field.String("content_type").
			MaxLen(100).
			Comment("MIME type"),
		field.Int64("file_size").
			Comment("File size in bytes"),
		field.String("url").
			MaxLen(1000).
			Optional().
			Comment("Pre-signed URL (if applicable)"),
		field.Time("url_expires_at").
			Optional().
			Nillable().
			Comment("When pre-signed URL expires"),
		field.JSON("metadata", map[string]any{}).
			Optional().
			Comment("Additional attachment metadata"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("deleted_at").
			Optional().
			Nillable().
			Comment("Soft delete timestamp"),
	}
}

// Edges of the TicketAttachment.
func (TicketAttachment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("ticket", Ticket.Type).
			Ref("attachments").
			Unique().
			Required().
			Field("ticket_id"),
	}
}

// Annotations of the TicketAttachment.
func (TicketAttachment) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "ticket_attachments",
		},
	}
}

// Indexes of the TicketAttachment.
func (TicketAttachment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ticket_id"),
		index.Fields("comment_id"),
		index.Fields("uploaded_by"),
		index.Fields("deleted_at"),
	}
}
