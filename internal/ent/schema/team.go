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

// Team holds the schema definition for the Team entity.
// Teams group agents together for assignment and reporting purposes.
type Team struct {
	ent.Schema
}

// Fields of the Team.
func (Team) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Comment("Team name"),
		field.Text("description").
			Optional().
			Comment("Team description"),
		field.Bool("is_active").
			Default(true).
			Comment("Whether team is active"),
		field.JSON("metadata", map[string]any{}).
			Optional().
			Comment("Additional team metadata"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.Time("deleted_at").
			Optional().
			Nillable().
			Comment("Soft delete timestamp"),
	}
}

// Edges of the Team.
func (Team) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("agents", Agent.Type),
	}
}

// Annotations of the Team.
func (Team) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "teams",
		},
	}
}

// Indexes of the Team.
func (Team) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").Unique(),
		index.Fields("tenant_id", "is_active"),
		index.Fields("deleted_at"),
	}
}
