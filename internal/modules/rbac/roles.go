package rbac

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// Errors the role administration routes translate to a status.
var (
	ErrUnknownRole       = errors.New("unknown role")
	ErrRoleNotFound      = errors.New("role not found")
	ErrUserNotFound      = errors.New("user not found")
	ErrAssignmentMissing = errors.New("this person does not hold that role")
	ErrRoleCodeTaken     = errors.New("a role with this code already exists")
	ErrRoleInUse         = errors.New("people still hold this role; move them to another role first")
	ErrSystemRole        = errors.New("a default role cannot be deleted")
	ErrSelfLockout       = errors.New("this would remove your own access to manage users")
	ErrInvalidRole       = errors.New("invalid role details")
	ErrUnknownPermission = errors.New("unknown permission code")
	ErrInvalidExpiry     = errors.New("the end date must be in the future")
)

// ManagePermission opens role and user administration, so losing it is what locks someone out.
var ManagePermission = Code("rbac", "manage")

var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,49}$`)

// RoleInput describes a new role.
type RoleInput struct {
	Code            string
	Name            string
	Description     *string
	PermissionCodes []string
}

// RoleView is a role as the management screens show it. Scope is "global" for a system role the
// tenant has not customized (the platform default, kept in step by the seeder) and "tenant" for the
// tenant's own roles and customized system roles.
type RoleView struct {
	Role
	Scope           string `json:"scope"`
	IsCustomized    bool   `json:"is_customized"`
	MemberCount     int    `json:"member_count"`
	PermissionCount int    `json:"permission_count"`
}

// UserRoleView is one role a person holds or held.
type UserRoleView struct {
	AssignmentID uuid.UUID  `json:"assignment_id"`
	RoleID       uuid.UUID  `json:"role_id"`
	RoleCode     string     `json:"role_code"`
	Name         string     `json:"name"`
	AssignedAt   time.Time  `json:"assigned_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
	Expired      bool       `json:"expired"`
}

// UserView is a person with their roles.
type UserView struct {
	User
	Roles []UserRoleView `json:"roles"`
}

func roleScope(r Role) string {
	if r.IsSystemRole {
		return "global"
	}
	return "tenant"
}

// isCustomized reports a former system role the tenant took over. A tenant cannot create a role
// with a system code, so a non-system role carrying one must be a customized copy.
func isCustomized(r Role) bool {
	if r.IsSystemRole {
		return false
	}
	_, ok := RoleByCode(r.RoleCode)
	return ok
}

// visibleRole reads one of the tenant's roles.
func (s *Service) visibleRole(ctx context.Context, tenantID, roleID uuid.UUID) (*Role, error) {
	if err := s.ensureTenantRoles(ctx, tenantID); err != nil {
		return nil, err
	}
	role, err := s.repo.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, ErrRoleNotFound
	}
	return role, nil
}

// customize makes a system role the tenant's own. The row keeps its id and code, so people who hold
// it keep it; the seeder stops resetting it.
func (s *Service) customize(ctx context.Context, tenantID uuid.UUID, role *Role) (*Role, error) {
	if !role.IsSystemRole {
		return role, nil
	}
	if err := s.repo.MarkRoleCustomized(ctx, tenantID, role.ID); err != nil {
		return nil, err
	}
	role.IsSystemRole = false
	return role, nil
}

// editableRole returns a role ready to change, customizing a system role first.
func (s *Service) editableRole(ctx context.Context, tenantID, roleID uuid.UUID) (*Role, error) {
	role, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	return s.customize(ctx, tenantID, role)
}

// CustomizeRole makes the tenant's own copy of a system role by code. Calling it again returns
// the same role.
func (s *Service) CustomizeRole(ctx context.Context, tenantID uuid.UUID, roleCode string) (*Role, error) {
	if err := s.ensureTenantRoles(ctx, tenantID); err != nil {
		return nil, err
	}
	role, err := s.repo.GetRoleByCode(ctx, tenantID, strings.TrimSpace(roleCode))
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, fmt.Errorf("%w: %s", ErrRoleNotFound, roleCode)
	}
	return s.customize(ctx, tenantID, role)
}

func normalizeRoleCode(code, name string) string {
	if code = strings.TrimSpace(code); code != "" {
		return strings.ToLower(code)
	}
	var b strings.Builder
	for _, c := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteByte('_')
			}
		}
	}
	return strings.TrimRight(b.String(), "_")
}

func validRoleName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return "", fmt.Errorf("%w: a name of up to 100 characters is required", ErrInvalidRole)
	}
	return name, nil
}

// permissionIDs resolves codes against the catalogue. An unknown code is refused, because skipping
// it would save a role with less access than the editor showed.
func (s *Service) permissionIDs(ctx context.Context, codes []string) ([]uuid.UUID, error) {
	unique := make([]string, 0, len(codes))
	for _, c := range codes {
		if !slices.Contains(unique, c) {
			unique = append(unique, c)
		}
	}
	found, err := s.repo.PermissionIDs(ctx, unique)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(unique))
	for _, c := range unique {
		id, ok := found[c]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownPermission, c)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) newTenantRole(ctx context.Context, tenantID uuid.UUID, in RoleInput, permIDs []uuid.UUID) (*Role, error) {
	name, err := validRoleName(in.Name)
	if err != nil {
		return nil, err
	}
	code := normalizeRoleCode(in.Code, name)
	if !roleCodePattern.MatchString(code) {
		return nil, fmt.Errorf("%w: the code must start with a letter and use letters, numbers and underscores", ErrInvalidRole)
	}
	// A system code stays reserved even after the tenant deleted its customized copy, since the
	// seeder brings the default back.
	if _, system := RoleByCode(code); system {
		return nil, ErrRoleCodeTaken
	}
	if err := s.ensureTenantRoles(ctx, tenantID); err != nil {
		return nil, err
	}
	if existing, err := s.repo.GetRoleByCode(ctx, tenantID, code); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrRoleCodeTaken
	}
	tid := tenantID
	role := &Role{ID: uuid.New(), TenantID: &tid, RoleCode: code, Name: name, Description: in.Description}
	if err := s.repo.CreateRoleWithPermissions(ctx, role, permIDs); err != nil {
		return nil, err
	}
	return role, nil
}

// CreateCustomRole adds a role of the tenant's own.
func (s *Service) CreateCustomRole(ctx context.Context, tenantID uuid.UUID, in RoleInput) (*Role, error) {
	permIDs, err := s.permissionIDs(ctx, in.PermissionCodes)
	if err != nil {
		return nil, err
	}
	return s.newTenantRole(ctx, tenantID, in, permIDs)
}

// CloneRole starts a new role from the permissions of any of the tenant's roles.
func (s *Service) CloneRole(ctx context.Context, tenantID, sourceID uuid.UUID, in RoleInput) (*Role, error) {
	source, err := s.visibleRole(ctx, tenantID, sourceID)
	if err != nil {
		return nil, err
	}
	codes, err := s.repo.RolePermissionCodes(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	permIDs, err := s.permissionIDs(ctx, codes)
	if err != nil {
		return nil, err
	}
	if in.Description == nil {
		in.Description = source.Description
	}
	return s.newTenantRole(ctx, tenantID, in, permIDs)
}

// UpdateRoleDetails renames a role or changes its description, customizing a system role first.
func (s *Service) UpdateRoleDetails(ctx context.Context, tenantID, roleID uuid.UUID, name, description *string) (*Role, error) {
	if name != nil {
		clean, err := validRoleName(*name)
		if err != nil {
			return nil, err
		}
		name = &clean
	}
	role, err := s.editableRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateRoleDetails(ctx, tenantID, role.ID, name, description); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetRole(ctx, tenantID, role.ID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrRoleNotFound
	}
	return updated, nil
}

// RolePermissionCodes reads a role's current permissions, for the editor to start from.
func (s *Service) RolePermissionCodes(ctx context.Context, tenantID, roleID uuid.UUID) ([]string, error) {
	role, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	codes, err := s.repo.RolePermissionCodes(ctx, role.ID)
	if err != nil {
		return nil, err
	}
	if codes == nil {
		codes = []string{}
	}
	slices.Sort(codes)
	return codes, nil
}

func grantsManage(codes []string) bool {
	return slices.ContainsFunc(codes, func(c string) bool { return access.Satisfies(c, ManagePermission) })
}

// losesManage reports whether changing roleID to newCodes (nil when it is being taken away) would
// leave the actor holding that role without any way to manage users. guard is nil for a caller who
// passes permission checks anyway (platform owner, superuser, service key).
func (s *Service) losesManage(ctx context.Context, tenantID uuid.UUID, guard *uuid.UUID, roleID uuid.UUID, newCodes []string) (bool, error) {
	if guard == nil {
		return false, nil
	}
	roles, err := s.repo.UserRoles(ctx, tenantID, *guard, s.now())
	if err != nil {
		return false, err
	}
	holds, keeps := false, false
	for _, role := range roles {
		if role.ID == roleID {
			holds = true
			keeps = keeps || grantsManage(newCodes)
			continue
		}
		codes, err := s.repo.RolePermissionCodes(ctx, role.ID)
		if err != nil {
			return false, err
		}
		keeps = keeps || grantsManage(codes)
	}
	return holds && !keeps, nil
}

// SetRolePermissions replaces a role's permissions, customizing a system role first. It answers
// with the role and the codes it had before.
func (s *Service) SetRolePermissions(ctx context.Context, tenantID uuid.UUID, guard *uuid.UUID, roleID uuid.UUID, codes []string) (*Role, []string, error) {
	permIDs, err := s.permissionIDs(ctx, codes)
	if err != nil {
		return nil, nil, err
	}
	visible, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, nil, err
	}
	if lose, err := s.losesManage(ctx, tenantID, guard, visible.ID, codes); err != nil {
		return nil, nil, err
	} else if lose {
		return nil, nil, ErrSelfLockout
	}
	role, err := s.customize(ctx, tenantID, visible)
	if err != nil {
		return nil, nil, err
	}
	before, err := s.RolePermissionCodes(ctx, tenantID, role.ID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.repo.ReplaceRolePermissions(ctx, role.ID, permIDs); err != nil {
		return nil, nil, err
	}
	s.forgetGrants()
	return role, before, nil
}

// DeleteRole removes one of the tenant's own roles once nobody holds it. A system role cannot be
// deleted; deleting a customized one brings the default back at the next seed.
func (s *Service) DeleteRole(ctx context.Context, tenantID, roleID uuid.UUID) (*Role, error) {
	role, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	if role.IsSystemRole {
		return nil, ErrSystemRole
	}
	held, err := s.repo.ListAssignments(ctx, tenantID, AssignmentFilters{RoleID: &role.ID, ActiveOnly: true}, s.now())
	if err != nil {
		return nil, err
	}
	if len(held) > 0 {
		return nil, ErrRoleInUse
	}
	if err := s.repo.DeleteRole(ctx, tenantID, role.ID); err != nil {
		return nil, err
	}
	s.tenant.Delete(tenantID)
	s.forgetGrants()
	return role, nil
}

// ListRoleViews lists the tenant's roles with their member and permission counts.
func (s *Service) ListRoleViews(ctx context.Context, tenantID uuid.UUID) ([]RoleView, error) {
	if err := s.ensureTenantRoles(ctx, tenantID); err != nil {
		return nil, err
	}
	roles, err := s.repo.ListRoles(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(roles))
	for i, role := range roles {
		ids[i] = role.ID
	}
	permCounts, err := s.repo.RolePermissionCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.RoleMemberCounts(ctx, tenantID, s.now())
	if err != nil {
		return nil, err
	}
	views := make([]RoleView, len(roles))
	for i, role := range roles {
		views[i] = RoleView{
			Role:            role,
			Scope:           roleScope(role),
			IsCustomized:    isCustomized(role),
			MemberCount:     members[role.ID],
			PermissionCount: permCounts[role.ID],
		}
	}
	return views, nil
}

// ListPermissions returns the permission catalogue.
func (s *Service) ListPermissions(ctx context.Context) ([]PermissionRecord, error) {
	perms, err := s.repo.ListPermissions(ctx)
	if perms == nil && err == nil {
		perms = []PermissionRecord{}
	}
	return perms, err
}
