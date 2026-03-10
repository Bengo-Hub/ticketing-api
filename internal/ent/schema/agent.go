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

// Agent holds the schema definition for the Agent entity.
// Agents are support staff who handle tickets.
type Agent struct {
	ent.Schema
}

// Fields of the Agent.
func (Agent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.UUID("user_id", uuid.UUID{}).
			Unique().
			Comment("Reference to auth-service user"),
		field.UUID("team_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Primary team assignment"),
		field.String("display_name").
			MaxLen(100).
			NotEmpty().
			Comment("Agent display name"),
		field.String("email").
			MaxLen(255).
			Comment("Agent email"),
		field.String("avatar_url").
			MaxLen(500).
			Optional().
			Comment("Profile picture URL"),
		field.Enum("role").
			Values("agent", "supervisor", "admin").
			Default("agent").
			Comment("Agent role"),
		field.Enum("status").
			Values("available", "busy", "away", "offline").
			Default("offline").
			Comment("Current availability status"),
		field.Int("max_concurrent_tickets").
			Default(10).
			Comment("Maximum tickets agent can handle simultaneously"),
		field.Int("current_ticket_count").
			Default(0).
			Comment("Current number of assigned tickets"),
		field.JSON("skills", []string{}).
			Optional().
			Comment("Agent skills for skill-based routing"),
		field.JSON("metadata", map[string]any{}).
			Optional().
			Comment("Additional agent metadata"),
		field.Bool("is_active").
			Default(true).
			Comment("Whether agent is active"),
		field.Time("last_active_at").
			Optional().
			Nillable().
			Comment("Last activity timestamp"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the Agent.
func (Agent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).
			Ref("agents").
			Unique().
			Field("team_id"),
		edge.To("assigned_tickets", Ticket.Type),
	}
}

// Annotations of the Agent.
func (Agent) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "agents",
		},
	}
}

// Indexes of the Agent.
func (Agent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "user_id").Unique(),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "is_active"),
		index.Fields("team_id"),
		index.Fields("email"),
	}
}
