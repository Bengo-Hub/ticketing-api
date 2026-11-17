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

// CSATResponse is one customer's rating of a resolved ticket, given once through the survey link
// sent on resolve. The survey token itself lives hashed in the ticket's metadata (csat).
type CSATResponse struct {
	ent.Schema
}

// Fields of the CSATResponse.
func (CSATResponse) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Immutable(),
		field.UUID("ticket_id", uuid.UUID{}).
			Immutable().
			Comment("The rated ticket, no FK"),
		field.Int("score").
			Range(1, 5).
			Immutable().
			Comment("1 (very unhappy) to 5 (very happy)"),
		field.Text("comment").
			Optional().
			Immutable().
			Comment("Optional free text, at most 2000 characters"),
		field.String("token_hash").
			MaxLen(64).
			Unique().
			Immutable().
			Comment("SHA-256 of the survey token that was used, so one token can never give two responses"),
		field.Time("submitted_at").
			Default(time.Now).
			Immutable(),
	}
}

// Indexes of the CSATResponse. (tenant_id, submitted_at) serves the rollup and the latest
// comments; (ticket_id, submitted_at) lets the rollup date a ticket's responses.
func (CSATResponse) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "submitted_at"),
		index.Fields("ticket_id", "submitted_at"),
	}
}

// Annotations of the CSATResponse.
func (CSATResponse) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "csat_responses"}}
}
