package rbac

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository is the RBAC data access the service needs. Every read and write that touches a
// tenant's rows takes the tenant id; the catalogue alone is global. A lookup that finds nothing
// returns nil and no error.
type Repository interface {
	// SeedCatalogue upserts the permission catalogue.
	SeedCatalogue(ctx context.Context, perms []Permission) error
	// EnsureTenantRoles creates any missing system role for the tenant and resets each system
	// role's permissions to its definition. A role the tenant customized is left alone.
	EnsureTenantRoles(ctx context.Context, tenantID uuid.UUID, roles []RoleDef) error

	// UpsertUser creates or refreshes the tenant's ticketing user for an auth-api user and
	// returns its local id.
	UpsertUser(ctx context.Context, id Identity) (uuid.UUID, error)
	// FindUser returns the tenant's ticketing user for an auth-api user id.
	FindUser(ctx context.Context, tenantID, authUserID uuid.UUID) (*User, error)
	// GetUser returns a tenant's ticketing user by local id or by auth-api user id.
	GetUser(ctx context.Context, tenantID, id uuid.UUID) (*User, error)
	// ListUsers pages the tenant's ticketing users by email; search matches part of the email or
	// name. It returns the page and the total.
	ListUsers(ctx context.Context, tenantID uuid.UUID, search string, limit, offset int) ([]User, int, error)

	// HasAssignments reports whether the user holds or ever held a role in the tenant.
	HasAssignments(ctx context.Context, tenantID, userID uuid.UUID) (bool, error)
	// AddRole assigns one role by code, leaving existing assignments in place.
	AddRole(ctx context.Context, tenantID, userID, assignedBy uuid.UUID, roleCode string) error
	// Grants resolves the user's unexpired roles, their permissions and the agent profile.
	Grants(ctx context.Context, tenantID, userID, authUserID uuid.UUID, now time.Time) (Grants, error)

	// ListRoles returns every role of the tenant, by name.
	ListRoles(ctx context.Context, tenantID uuid.UUID) ([]Role, error)
	// GetRole returns a tenant's role by id.
	GetRole(ctx context.Context, tenantID, roleID uuid.UUID) (*Role, error)
	// GetRoleByCode returns a tenant's role by code.
	GetRoleByCode(ctx context.Context, tenantID uuid.UUID, code string) (*Role, error)
	// CreateRoleWithPermissions writes a tenant role and its permissions in one transaction. A
	// code the tenant already uses gives ErrRoleCodeTaken.
	CreateRoleWithPermissions(ctx context.Context, role *Role, permissionIDs []uuid.UUID) error
	// UpdateRoleDetails changes a role's name or description; nil leaves a value as it is.
	UpdateRoleDetails(ctx context.Context, tenantID, roleID uuid.UUID, name, description *string) error
	// MarkRoleCustomized turns a system role into the tenant's own, so the seeder stops resetting it.
	MarkRoleCustomized(ctx context.Context, tenantID, roleID uuid.UUID) error
	// ReplaceRolePermissions swaps a role's whole permission set in one transaction.
	ReplaceRolePermissions(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error
	// DeleteRole removes a role with its permission rows and its ended assignments.
	DeleteRole(ctx context.Context, tenantID, roleID uuid.UUID) error
	// RolePermissionCounts counts each role's permissions.
	RolePermissionCounts(ctx context.Context, roleIDs []uuid.UUID) (map[uuid.UUID]int, error)
	// RoleMemberCounts counts each role's current holders in the tenant.
	RoleMemberCounts(ctx context.Context, tenantID uuid.UUID, now time.Time) (map[uuid.UUID]int, error)
	// RolePermissionCodes lists a role's permission codes.
	RolePermissionCodes(ctx context.Context, roleID uuid.UUID) ([]string, error)

	// ListPermissions returns the catalogue ordered by code.
	ListPermissions(ctx context.Context) ([]PermissionRecord, error)
	// PermissionIDs resolves codes to catalogue ids; a code not in the catalogue is left out.
	PermissionIDs(ctx context.Context, codes []string) (map[string]uuid.UUID, error)

	// SaveAssignment grants a role, or renews the existing row of that tenant, user and role.
	SaveAssignment(ctx context.Context, a *Assignment) error
	// EndAssignment stops an assignment at the given time and keeps the row, which tells sign-in
	// that an admin took the role away and it must not be added back.
	EndAssignment(ctx context.Context, tenantID, assignmentID uuid.UUID, at time.Time) error
	// ListAssignments lists assignments with their role code and name.
	ListAssignments(ctx context.Context, tenantID uuid.UUID, f AssignmentFilters, now time.Time) ([]Assignment, error)
	// UserRoles returns the roles a user holds now; id is the local or the auth-api user id.
	UserRoles(ctx context.Context, tenantID, id uuid.UUID, now time.Time) ([]Role, error)

	// WriteAudit stores one audit log row.
	WriteAudit(ctx context.Context, e AuditEntry) error
	// ListAudit pages a tenant's audit log, newest first, and returns the total.
	ListAudit(ctx context.Context, tenantID uuid.UUID, f AuditFilters, limit, offset int) ([]AuditEntry, int, error)

	// EnsureAgent creates the agent profile for a staff user when it does not exist yet, raising
	// its role when the user's staff role is stronger.
	EnsureAgent(ctx context.Context, in AgentInput) error
	// DeactivateAgent marks the person's agent profile inactive (their tickets stay assigned).
	DeactivateAgent(ctx context.Context, tenantID, authUserID uuid.UUID) error
	// UpdateAgentContact refreshes an existing agent profile's email and name.
	UpdateAgentContact(ctx context.Context, tenantID, authUserID uuid.UUID, email, name string) error
}
