package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// TenantSetting is one settings document per tenant. Each section is a top-level key of settings
// ("channels" holds channels.whatsapp.auto_ticket); writing a section replaces that key only.
type TenantSetting struct {
	ent.Schema
}

// Fields of the TenantSetting.
func (TenantSetting) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Unique().
			Immutable().
			Comment("One settings row per tenant"),
		field.JSON("settings", map[string]any{}).
			Optional().
			Comment("Settings sections keyed by name"),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Annotations of the TenantSetting.
func (TenantSetting) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "tenant_settings"},
	}
}
