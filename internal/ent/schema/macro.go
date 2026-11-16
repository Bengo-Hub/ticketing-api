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

// Macro is a canned reply with optional field changes an agent applies to a ticket in one step.
type Macro struct {
	ent.Schema
}

// Fields of the Macro.
func (Macro) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Comment("Macro name, unique within the tenant"),
		field.Text("body").
			Optional().
			Comment("Reply posted when the macro is applied (rich text); empty posts no reply"),
		field.JSON("actions", map[string]any{}).
			Optional().
			Comment("Field changes: status, priority, category_id, team_id, agent_id, add_tags, comment_type"),
		field.Bool("is_active").
			Default(true).
			Comment("Inactive macros are hidden from the picker"),
		field.UUID("created_by", uuid.UUID{}).
			Comment("auth-api user id of the author"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Annotations of the Macro.
func (Macro) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "macros"}}
}

// Indexes of the Macro.
func (Macro) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").Unique(),
	}
}
