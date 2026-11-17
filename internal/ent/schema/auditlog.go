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

// AuditLog records who changed what in a tenant. Role administration writes it today (resource
// types role and user); the shape follows treasury-api's audit_logs so one reader serves both.
type AuditLog struct {
	ent.Schema
}

// Fields of the AuditLog.
func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Optional().
			Nillable().
			Immutable().
			Comment("Tenant the action was taken in; nil for a platform-level action"),
		field.UUID("user_id", uuid.UUID{}).
			Immutable().
			Comment("auth-api user id of the person who acted"),
		field.String("user_email").
			Optional().
			MaxLen(255).
			Immutable().
			Comment("Email of the person who acted, for display"),
		field.String("action").
			NotEmpty().
			MaxLen(50).
			Immutable().
			Comment("What happened, for example role.assigned or role.permissions_updated"),
		field.String("resource_type").
			NotEmpty().
			MaxLen(100).
			Immutable().
			Comment("Kind of record changed: role or user"),
		field.String("resource_id").
			NotEmpty().
			MaxLen(255).
			Immutable().
			Comment("Id of the record changed"),
		field.Text("changes").
			Optional().
			Nillable().
			Immutable().
			Comment("JSON summary of the change"),
		field.String("ip_address").
			Optional().
			Nillable().
			MaxLen(45).
			Immutable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Indexes of the AuditLog.
func (AuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "created_at"),
		index.Fields("tenant_id", "resource_type", "resource_id"),
		index.Fields("user_id"),
	}
}

// Annotations of the AuditLog.
func (AuditLog) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "audit_logs"}}
}
