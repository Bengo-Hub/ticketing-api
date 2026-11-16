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

// SLAPolicy holds the schema definition for the SLAPolicy entity.
// Defines SLA targets for response and resolution times.
type SLAPolicy struct {
	ent.Schema
}

// Fields of the SLAPolicy.
func (SLAPolicy) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Comment("Policy name"),
		field.Text("description").
			Optional().
			Comment("Policy description"),
		field.Enum("priority").
			Values("low", "normal", "high", "urgent", "critical").
			Comment("Priority level this policy applies to"),
		field.UUID("category_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Category this policy applies to (null = all categories)"),
		field.Int("first_response_minutes").
			Comment("Target first response time in minutes of the policy's clock"),
		field.Int("resolution_minutes").
			Comment("Target resolution time in minutes of the policy's clock"),
		field.UUID("calendar_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Business calendar both targets count in; null counts every minute (24x7)"),
		field.Bool("is_active").
			Default(true).
			Comment("Whether policy is active"),
		field.Int("sort_order").
			Default(0).
			Comment("Priority for policy matching (lower = higher priority)"),
		field.JSON("metadata", map[string]any{}).
			Optional().
			Comment("Policy settings: notify_before_breach_min, pause_on_pending_internal, escalation_rules"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the SLAPolicy.
func (SLAPolicy) Edges() []ent.Edge {
	return []ent.Edge{}
}

// Annotations of the SLAPolicy.
func (SLAPolicy) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "sla_policies",
		},
	}
}

// Indexes of the SLAPolicy.
func (SLAPolicy) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "priority"),
		index.Fields("tenant_id", "category_id"),
		index.Fields("tenant_id", "is_active"),
		// A calendar in use by a policy cannot be deleted.
		index.Fields("tenant_id", "calendar_id"),
		index.Fields("sort_order"),
	}
}
