package rbac

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository is the RBAC data access the service needs.
type Repository interface {
	// SeedCatalogue upserts the permission catalogue.
	SeedCatalogue(ctx context.Context, perms []Permission) error
	// EnsureTenantRoles creates any missing system role for the tenant and resets each system
	// role's permissions to its definition.
	EnsureTenantRoles(ctx context.Context, tenantID uuid.UUID, roles []RoleDef) error

	// UpsertUser creates or refreshes the tenant's ticketing user for an auth-api user and
	// returns its local id.
	UpsertUser(ctx context.Context, id Identity) (uuid.UUID, error)
	// FindUser returns the tenant's ticketing user, or nil when none exists.
	FindUser(ctx context.Context, tenantID, authUserID uuid.UUID) (*User, error)
	// GetUser returns a ticketing user by local id within the tenant, or nil.
	GetUser(ctx context.Context, tenantID, userID uuid.UUID) (*User, error)
	// ListUsers returns the tenant's ticketing users with their role codes.
	ListUsers(ctx context.Context, tenantID uuid.UUID, limit int) ([]User, error)

	// HasAssignments reports whether the user holds any role in the tenant.
	HasAssignments(ctx context.Context, tenantID, userID uuid.UUID) (bool, error)
	// SetRoles replaces the user's role assignments with the given role codes.
	SetRoles(ctx context.Context, tenantID, userID, assignedBy uuid.UUID, roleCodes []string) error
	// AddRole assigns one role, leaving existing assignments in place.
	AddRole(ctx context.Context, tenantID, userID, assignedBy uuid.UUID, roleCode string) error
	// Grants resolves the user's unexpired roles, their permissions and the agent profile.
	Grants(ctx context.Context, tenantID, userID, authUserID uuid.UUID, now time.Time) (Grants, error)
	// ListRoles returns the tenant's roles with their permission codes.
	ListRoles(ctx context.Context, tenantID uuid.UUID) ([]RoleView, error)

	// EnsureAgent creates the agent profile for a staff user when it does not exist yet, raising
	// its role when the user's staff role is stronger.
	EnsureAgent(ctx context.Context, in AgentInput) error
	// UpdateAgentContact refreshes an existing agent profile's email and name.
	UpdateAgentContact(ctx context.Context, tenantID, authUserID uuid.UUID, email, name string) error
}
