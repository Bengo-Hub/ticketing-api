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

// TicketingPermission is the global permission catalogue (ticketing.{module}.{action}). It is
// never tenant scoped; roles are.
type TicketingPermission struct{ ent.Schema }

// Fields of the TicketingPermission.
func (TicketingPermission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("permission_code").NotEmpty().Unique(),
		field.String("module").NotEmpty(),
		field.String("action").NotEmpty(),
		field.String("resource").Optional(),
		field.Text("description").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Edges of the TicketingPermission.
func (TicketingPermission) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("roles", TicketingRole.Type).Ref("permissions").Through("role_permissions", RolePermission.Type),
	}
}

// Indexes of the TicketingPermission.
func (TicketingPermission) Indexes() []ent.Index {
	return []ent.Index{index.Fields("module", "action")}
}

// Annotations of the TicketingPermission.
func (TicketingPermission) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticketing_permissions"}}
}

// TicketingRole is a tenant's role. The system roles are seeded for each tenant the first time
// one of its users signs in; a tenant may add its own.
type TicketingRole struct{ ent.Schema }

// Fields of the TicketingRole.
func (TicketingRole) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("role_code").NotEmpty(),
		field.String("name").NotEmpty(),
		field.Text("description").Optional(),
		field.Bool("is_system_role").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the TicketingRole.
func (TicketingRole) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("permissions", TicketingPermission.Type).Through("role_permissions", RolePermission.Type),
		edge.From("user_assignments", UserRoleAssignment.Type).Ref("role"),
	}
}

// Indexes of the TicketingRole.
func (TicketingRole) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "role_code").Unique(),
	}
}

// Annotations of the TicketingRole.
func (TicketingRole) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticketing_roles"}}
}

// RolePermission is the role to permission junction.
type RolePermission struct{ ent.Schema }

// Fields of the RolePermission.
func (RolePermission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("role_id", uuid.UUID{}),
		field.UUID("permission_id", uuid.UUID{}),
	}
}

// Edges of the RolePermission.
func (RolePermission) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("role", TicketingRole.Type).Field("role_id").Required().Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("permission", TicketingPermission.Type).Field("permission_id").Required().Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the RolePermission.
func (RolePermission) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("role_id", "permission_id").Unique(),
		index.Fields("permission_id"),
	}
}

// Annotations of the RolePermission.
func (RolePermission) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "role_permissions"}}
}

// UserRoleAssignment gives a ticketing user a role within a tenant.
type UserRoleAssignment struct{ ent.Schema }

// Fields of the UserRoleAssignment.
func (UserRoleAssignment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}).Comment("TicketingUser id"),
		field.UUID("role_id", uuid.UUID{}),
		field.UUID("assigned_by", uuid.UUID{}).Comment("auth-api user id of the assigner; the user itself for a sign-in default"),
		field.Time("assigned_at").Default(time.Now).Immutable(),
		field.Time("expires_at").Optional().Nillable(),
	}
}

// Edges of the UserRoleAssignment.
func (UserRoleAssignment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user", TicketingUser.Type).Field("user_id").Required().Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("role", TicketingRole.Type).Field("role_id").Required().Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the UserRoleAssignment.
func (UserRoleAssignment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "user_id", "role_id").Unique(),
		index.Fields("role_id"),
	}
}

// Annotations of the UserRoleAssignment.
func (UserRoleAssignment) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_role_assignments"}}
}

// TicketingUser is the local reference to an auth-api user, created just in time from the token
// on the first authenticated request in a tenant.
type TicketingUser struct{ ent.Schema }

// Fields of the TicketingUser.
func (TicketingUser) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("auth_service_user_id", uuid.UUID{}),
		field.String("email").Optional(),
		field.String("name").Optional(),
		field.String("sync_status").Default("synced"),
		field.Time("last_sync_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the TicketingUser.
func (TicketingUser) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("role_assignments", UserRoleAssignment.Type).Ref("user"),
	}
}

// Indexes of the TicketingUser.
func (TicketingUser) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "auth_service_user_id").Unique(),
		index.Fields("auth_service_user_id"),
	}
}

// Annotations of the TicketingUser.
func (TicketingUser) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticketing_users"}}
}
