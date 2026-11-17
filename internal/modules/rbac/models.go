// Package rbac is ticketing's local role-based access control (Trinity Layer 3): the permission
// catalogue, the per-tenant system roles, just-in-time ticketing users with a default role mapped
// from their global roles, agent profiles for staff, and the request access resolution.
package rbac

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

// Modules are the ticketing permission modules.
var Modules = []string{
	"tickets", "comments", "attachments", "categories", "agents", "teams",
	"sla", "kb", "routing", "analytics", "settings", "rbac",
}

// Actions is the fleet's fixed permission action vocabulary.
var Actions = []string{"add", "view", "view_own", "change", "change_own", "delete", "delete_own", "manage", "manage_own"}

// Code builds a permission code: ticketing.{module}.{action}.
func Code(module, action string) string { return "ticketing." + module + "." + action }

// Permission is one catalogue entry.
type Permission struct {
	Code        string
	Module      string
	Action      string
	Description string
}

// actionLabels and moduleLabels give each permission a readable name for the role editor.
var actionLabels = map[string]string{
	"add": "Add", "view": "View", "view_own": "View own", "change": "Change", "change_own": "Change own",
	"delete": "Delete", "delete_own": "Delete own", "manage": "Manage", "manage_own": "Manage own",
}

var moduleLabels = map[string]string{
	"sla": "SLA", "kb": "knowledge base", "rbac": "roles and users",
}

// PermissionName is the readable name of a module and action, such as "View own tickets".
func PermissionName(module, action string) string {
	a, ok := actionLabels[action]
	if !ok {
		a = action
	}
	m, ok := moduleLabels[module]
	if !ok {
		m = module
	}
	return a + " " + m
}

// Catalogue returns every ticketing permission: each module with each action.
func Catalogue() []Permission {
	out := make([]Permission, 0, len(Modules)*len(Actions))
	for _, m := range Modules {
		for _, a := range Actions {
			out = append(out, Permission{Code: Code(m, a), Module: m, Action: a, Description: PermissionName(m, a)})
		}
	}
	return out
}

// AllCodes returns every permission code in the catalogue.
func AllCodes() []string {
	cat := Catalogue()
	out := make([]string, len(cat))
	for i, p := range cat {
		out[i] = p.Code
	}
	return out
}

// Ticketing system role codes.
const (
	RoleAdmin      = "ticketing_admin"
	RoleSupervisor = "supervisor"
	RoleAgent      = "agent"
	RoleCustomer   = "customer"
)

// RoleDef is a system role seeded for every tenant.
type RoleDef struct {
	Code        string
	Name        string
	Description string
	Permissions []string
}

// SystemRoles are the roles every tenant starts with.
var SystemRoles = []RoleDef{
	{
		Code: RoleAdmin, Name: "Ticketing Admin", Description: "Full ticketing administration",
		Permissions: AllCodes(),
	},
	{
		Code: RoleSupervisor, Name: "Supervisor", Description: "Runs the desk: every ticket, the knowledge base and the reports",
		Permissions: []string{
			Code("tickets", "manage"), Code("comments", "manage"), Code("attachments", "manage"),
			Code("categories", "view"), Code("agents", "view"), Code("agents", "change_own"), Code("teams", "view"),
			Code("sla", "view"), Code("routing", "view"), Code("analytics", "view"), Code("kb", "manage"),
		},
	},
	{
		Code: RoleAgent, Name: "Agent", Description: "Works tickets and replies to customers",
		Permissions: []string{
			Code("tickets", "view"), Code("tickets", "add"), Code("tickets", "change"),
			Code("comments", "add"), Code("comments", "change_own"), Code("attachments", "view"),
			Code("attachments", "add"), Code("attachments", "delete_own"),
			Code("categories", "view"), Code("agents", "view"), Code("agents", "change_own"),
			Code("teams", "view"), Code("kb", "view"),
		},
	},
	{
		Code: RoleCustomer, Name: "Customer", Description: "Raises and follows their own tickets",
		Permissions: []string{
			Code("tickets", "add"), Code("tickets", "view_own"), Code("tickets", "change_own"),
			Code("comments", "add"), Code("comments", "change_own"), Code("attachments", "view_own"),
			Code("attachments", "add"), Code("attachments", "delete_own"),
			Code("categories", "view"), Code("kb", "view"),
		},
	},
}

// RoleByCode returns a system role definition.
func RoleByCode(code string) (RoleDef, bool) {
	for _, r := range SystemRoles {
		if r.Code == code {
			return r, true
		}
	}
	return RoleDef{}, false
}

// rolePriority ranks ticketing roles so a user with several global roles gets the strongest.
var rolePriority = map[string]int{RoleAdmin: 4, RoleSupervisor: 3, RoleAgent: 2, RoleCustomer: 1}

// staffGlobalRoles are the global roles that work tickets as agents.
var staffGlobalRoles = map[string]bool{
	"staff": true, "receptionist": true, "cashier": true, "waiter": true, "kitchen": true, "bar": true,
}

// MapGlobalRole maps one auth-api global role to a ticketing role: superuser and admin to
// ticketing_admin, manager to supervisor, staff roles to agent, everything else (member,
// customer, viewer, rider) to customer.
func MapGlobalRole(role string) string {
	switch {
	case role == "superuser" || role == "admin":
		return RoleAdmin
	case role == "manager":
		return RoleSupervisor
	case staffGlobalRoles[role]:
		return RoleAgent
	default:
		return RoleCustomer
	}
}

// MapGlobalRoles returns the strongest ticketing role for a set of global roles; customer when none.
func MapGlobalRoles(roles []string) string {
	best := RoleCustomer
	for _, r := range roles {
		if m := MapGlobalRole(r); rolePriority[m] > rolePriority[best] {
			best = m
		}
	}
	return best
}

// IsStaffRole reports whether a ticketing role works the desk and so has an agent profile.
func IsStaffRole(code string) bool {
	return code == RoleAdmin || code == RoleSupervisor || code == RoleAgent
}

// AgentRole returns the agent profile role for the strongest staff role held, or "" for none.
func AgentRole(roles []string) string {
	best := ""
	for _, r := range roles {
		if IsStaffRole(r) && rolePriority[r] > rolePriority[best] {
			best = r
		}
	}
	switch best {
	case RoleAdmin:
		return "admin"
	case RoleSupervisor:
		return "supervisor"
	case RoleAgent:
		return "agent"
	}
	return ""
}

// PermissionsForRoles returns the union of the system roles' permissions, sorted.
func PermissionsForRoles(codes []string) []string {
	set := map[string]bool{}
	for _, c := range codes {
		if def, ok := RoleByCode(c); ok {
			for _, p := range def.Permissions {
				set[p] = true
			}
		}
	}
	return sortedKeys(set)
}

// Identity is what the token (or an auth event) tells us about a user in a tenant.
type Identity struct {
	TenantID    uuid.UUID
	AuthUserID  uuid.UUID
	Email       string
	Name        string
	GlobalRoles []string
}

// UserStatusActive is the status of every ticketing user: a row exists only for a member of the
// tenant, and auth-api owns suspension and removal.
const UserStatusActive = "active"

// User is a ticketing user row, in treasury-api's user shape plus the display name.
type User struct {
	ID                uuid.UUID  `json:"id"`
	TenantID          uuid.UUID  `json:"tenant_id"`
	AuthServiceUserID uuid.UUID  `json:"auth_service_user_id"`
	Email             string     `json:"email"`
	Name              string     `json:"name,omitempty"`
	Status            string     `json:"status"`
	SyncStatus        string     `json:"sync_status"`
	LastSyncAt        *time.Time `json:"last_sync_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Role is a tenant's ticketing role. A system role is seeded and kept in step with SystemRoles;
// customizing one turns is_system_role off so the seeder leaves the tenant's edits alone.
type Role struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     *uuid.UUID `json:"tenant_id"`
	RoleCode     string     `json:"role_code"`
	Name         string     `json:"name"`
	Description  *string    `json:"description"`
	IsSystemRole bool       `json:"is_system_role"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// PermissionRecord is a catalogue row as the role editor lists it.
type PermissionRecord struct {
	ID             uuid.UUID `json:"id"`
	PermissionCode string    `json:"permission_code"`
	Name           string    `json:"name"`
	Module         string    `json:"module"`
	Action         string    `json:"action"`
	Resource       *string   `json:"resource,omitempty"`
	Description    *string   `json:"description,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// Assignment is one role held, or once held, by a ticketing user.
type Assignment struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserID     uuid.UUID
	RoleID     uuid.UUID
	RoleCode   string
	RoleName   string
	AssignedBy uuid.UUID
	AssignedAt time.Time
	ExpiresAt  *time.Time
}

// AssignmentFilters narrows ListAssignments. ActiveOnly keeps assignments with no end date or one
// still ahead.
type AssignmentFilters struct {
	UserID     *uuid.UUID
	RoleID     *uuid.UUID
	UserIDs    []uuid.UUID
	ActiveOnly bool
}

// AuditEntry is one audit log row.
type AuditEntry struct {
	ID           uuid.UUID
	TenantID     *uuid.UUID
	UserID       uuid.UUID
	UserEmail    string
	Action       string
	ResourceType string
	ResourceID   string
	Changes      *string
	IPAddress    *string
	CreatedAt    time.Time
}

// AuditFilters narrows the audit log list. ResourceTypes is forced by the caller; the others come
// from the query string.
type AuditFilters struct {
	ResourceTypes []string
	ResourceType  string
	UserID        *uuid.UUID
	Action        string
	From          *time.Time
	To            *time.Time
}

// AgentInput is the agent profile created for a staff user.
type AgentInput struct {
	TenantID   uuid.UUID
	AuthUserID uuid.UUID
	Email      string
	Name       string
	Role       string // agent, supervisor or admin
}

// Grants are a user's resolved roles, permissions and agent profile in one tenant.
type Grants struct {
	Roles   []string
	Perms   []string
	AgentID *uuid.UUID
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
