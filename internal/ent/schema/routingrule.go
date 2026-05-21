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

// RoutingRule holds the schema definition for the RoutingRule entity.
// Defines rules for auto-assigning tickets to teams/agents and overriding priority.
type RoutingRule struct {
	ent.Schema
}

// Fields of the RoutingRule.
func (RoutingRule) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Comment("Rule name"),
		field.Text("description").
			Optional().
			Comment("Rule description"),
		field.Bool("is_active").
			Default(true).
			Comment("Whether rule is active"),
		field.Int("sort_order").
			Default(0).
			Comment("Evaluation order (lower = higher priority)"),
		field.JSON("conditions", []map[string]any{}).
			Comment("Array of condition objects"),
		field.JSON("action", map[string]any{}).
			Comment("Single action object"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the RoutingRule.
func (RoutingRule) Edges() []ent.Edge {
	return []ent.Edge{}
}

// Annotations of the RoutingRule.
func (RoutingRule) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "routing_rules",
		},
	}
}

// Indexes of the RoutingRule.
func (RoutingRule) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "is_active"),
		index.Fields("tenant_id", "sort_order"),
	}
}
