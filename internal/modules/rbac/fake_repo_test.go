package rbac

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// fakeRepo is an in-memory Repository for service and handler tests.
type fakeRepo struct {
	mu          sync.Mutex
	perms       map[string]uuid.UUID // code to id
	roles       map[uuid.UUID]*Role
	rolePerms   map[uuid.UUID][]string
	users       map[uuid.UUID]*User
	assignments map[uuid.UUID]*Assignment
	audit       []AuditEntry
	agents      map[uuid.UUID]string // auth user id to agent role
	ensureCalls int
}

func newFakeRepo() *fakeRepo {
	f := &fakeRepo{
		perms: map[string]uuid.UUID{}, roles: map[uuid.UUID]*Role{}, rolePerms: map[uuid.UUID][]string{},
		users: map[uuid.UUID]*User{}, assignments: map[uuid.UUID]*Assignment{}, agents: map[uuid.UUID]string{},
	}
	_ = f.SeedCatalogue(context.Background(), Catalogue())
	return f
}

var _ Repository = (*fakeRepo)(nil)

func (f *fakeRepo) SeedCatalogue(_ context.Context, perms []Permission) error {
	for _, p := range perms {
		if _, ok := f.perms[p.Code]; !ok {
			f.perms[p.Code] = uuid.New()
		}
	}
	return nil
}

func (f *fakeRepo) roleByCode(tenantID uuid.UUID, code string) *Role {
	for _, r := range f.roles {
		if *r.TenantID == tenantID && r.RoleCode == code {
			return r
		}
	}
	return nil
}

func (f *fakeRepo) EnsureTenantRoles(_ context.Context, tenantID uuid.UUID, defs []RoleDef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCalls++
	for _, def := range defs {
		r := f.roleByCode(tenantID, def.Code)
		if r != nil && !r.IsSystemRole {
			continue
		}
		if r == nil {
			tid, desc := tenantID, def.Description
			r = &Role{ID: uuid.New(), TenantID: &tid, RoleCode: def.Code, Description: &desc}
			f.roles[r.ID] = r
		}
		r.Name, r.IsSystemRole = def.Name, true
		f.rolePerms[r.ID] = slices.Clone(def.Permissions)
	}
	return nil
}

func (f *fakeRepo) UpsertUser(_ context.Context, id Identity) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.TenantID == id.TenantID && u.AuthServiceUserID == id.AuthUserID {
			if id.Email != "" {
				u.Email = id.Email
			}
			return u.ID, nil
		}
	}
	u := &User{ID: uuid.New(), TenantID: id.TenantID, AuthServiceUserID: id.AuthUserID, Email: id.Email, Name: id.Name, Status: UserStatusActive}
	f.users[u.ID] = u
	return u.ID, nil
}

func (f *fakeRepo) FindUser(_ context.Context, tenantID, authUserID uuid.UUID) (*User, error) {
	for _, u := range f.users {
		if u.TenantID == tenantID && u.AuthServiceUserID == authUserID {
			c := *u
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) GetUser(_ context.Context, tenantID, id uuid.UUID) (*User, error) {
	for _, u := range f.users {
		if u.TenantID == tenantID && (u.ID == id || u.AuthServiceUserID == id) {
			c := *u
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) ListUsers(_ context.Context, tenantID uuid.UUID, search string, limit, offset int) ([]User, int, error) {
	var all []User
	for _, u := range f.users {
		if u.TenantID == tenantID && (search == "" || strings.Contains(strings.ToLower(u.Email), strings.ToLower(search))) {
			all = append(all, *u)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Email < all[j].Email })
	total := len(all)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	return all[offset:end], total, nil
}

func (f *fakeRepo) HasAssignments(_ context.Context, tenantID, userID uuid.UUID) (bool, error) {
	for _, a := range f.assignments {
		if a.TenantID == tenantID && a.UserID == userID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRepo) AddRole(ctx context.Context, tenantID, userID, assignedBy uuid.UUID, code string) error {
	r := f.roleByCode(tenantID, code)
	if r == nil {
		return ErrUnknownRole
	}
	return f.SaveAssignment(ctx, &Assignment{TenantID: tenantID, UserID: userID, RoleID: r.ID, AssignedBy: assignedBy})
}

func active(a *Assignment, now time.Time) bool { return a.ExpiresAt == nil || a.ExpiresAt.After(now) }

func (f *fakeRepo) Grants(_ context.Context, tenantID, userID, _ uuid.UUID, now time.Time) (Grants, error) {
	roles, perms := map[string]bool{}, map[string]bool{}
	for _, a := range f.assignments {
		if a.TenantID == tenantID && a.UserID == userID && active(a, now) {
			r := f.roles[a.RoleID]
			roles[r.RoleCode] = true
			for _, p := range f.rolePerms[r.ID] {
				perms[p] = true
			}
		}
	}
	return Grants{Roles: sortedKeys(roles), Perms: sortedKeys(perms)}, nil
}

func (f *fakeRepo) ListRoles(_ context.Context, tenantID uuid.UUID) ([]Role, error) {
	var out []Role
	for _, r := range f.roles {
		if *r.TenantID == tenantID {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeRepo) GetRole(_ context.Context, tenantID, roleID uuid.UUID) (*Role, error) {
	if r, ok := f.roles[roleID]; ok && *r.TenantID == tenantID {
		c := *r
		return &c, nil
	}
	return nil, nil
}

func (f *fakeRepo) GetRoleByCode(_ context.Context, tenantID uuid.UUID, code string) (*Role, error) {
	if r := f.roleByCode(tenantID, code); r != nil {
		c := *r
		return &c, nil
	}
	return nil, nil
}

func (f *fakeRepo) codesFor(ids []uuid.UUID) []string {
	var codes []string
	for code, id := range f.perms {
		if slices.Contains(ids, id) {
			codes = append(codes, code)
		}
	}
	return codes
}

func (f *fakeRepo) CreateRoleWithPermissions(_ context.Context, role *Role, ids []uuid.UUID) error {
	if f.roleByCode(*role.TenantID, role.RoleCode) != nil {
		return ErrRoleCodeTaken
	}
	c := *role
	f.roles[role.ID] = &c
	f.rolePerms[role.ID] = f.codesFor(ids)
	return nil
}

func (f *fakeRepo) UpdateRoleDetails(_ context.Context, _, roleID uuid.UUID, name, description *string) error {
	r := f.roles[roleID]
	if name != nil {
		r.Name = *name
	}
	if description != nil {
		r.Description = description
	}
	return nil
}

func (f *fakeRepo) MarkRoleCustomized(_ context.Context, _, roleID uuid.UUID) error {
	f.roles[roleID].IsSystemRole = false
	return nil
}

func (f *fakeRepo) ReplaceRolePermissions(_ context.Context, roleID uuid.UUID, ids []uuid.UUID) error {
	f.rolePerms[roleID] = f.codesFor(ids)
	return nil
}

func (f *fakeRepo) DeleteRole(_ context.Context, _, roleID uuid.UUID) error {
	delete(f.roles, roleID)
	delete(f.rolePerms, roleID)
	for id, a := range f.assignments {
		if a.RoleID == roleID {
			delete(f.assignments, id)
		}
	}
	return nil
}

func (f *fakeRepo) RolePermissionCounts(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	for _, id := range ids {
		out[id] = len(f.rolePerms[id])
	}
	return out, nil
}

func (f *fakeRepo) RoleMemberCounts(_ context.Context, tenantID uuid.UUID, now time.Time) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	for _, a := range f.assignments {
		if a.TenantID == tenantID && active(a, now) {
			out[a.RoleID]++
		}
	}
	return out, nil
}

func (f *fakeRepo) RolePermissionCodes(_ context.Context, roleID uuid.UUID) ([]string, error) {
	return slices.Clone(f.rolePerms[roleID]), nil
}

func (f *fakeRepo) ListPermissions(context.Context) ([]PermissionRecord, error) {
	var out []PermissionRecord
	for _, p := range Catalogue() {
		out = append(out, PermissionRecord{ID: f.perms[p.Code], PermissionCode: p.Code, Name: PermissionName(p.Module, p.Action), Module: p.Module, Action: p.Action})
	}
	return out, nil
}

func (f *fakeRepo) PermissionIDs(_ context.Context, codes []string) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	for _, c := range codes {
		if id, ok := f.perms[c]; ok {
			out[c] = id
		}
	}
	return out, nil
}

func (f *fakeRepo) SaveAssignment(_ context.Context, a *Assignment) error {
	for _, existing := range f.assignments {
		if existing.TenantID == a.TenantID && existing.UserID == a.UserID && existing.RoleID == a.RoleID {
			existing.AssignedBy, existing.ExpiresAt = a.AssignedBy, a.ExpiresAt
			return nil
		}
	}
	c := *a
	c.ID, c.AssignedAt = uuid.New(), time.Now()
	f.assignments[c.ID] = &c
	return nil
}

func (f *fakeRepo) EndAssignment(_ context.Context, _, id uuid.UUID, at time.Time) error {
	f.assignments[id].ExpiresAt = &at
	return nil
}

func (f *fakeRepo) ListAssignments(_ context.Context, tenantID uuid.UUID, fl AssignmentFilters, now time.Time) ([]Assignment, error) {
	var out []Assignment
	for _, a := range f.assignments {
		if a.TenantID != tenantID || (fl.UserID != nil && a.UserID != *fl.UserID) || (fl.RoleID != nil && a.RoleID != *fl.RoleID) ||
			(len(fl.UserIDs) > 0 && !slices.Contains(fl.UserIDs, a.UserID)) || (fl.ActiveOnly && !active(a, now)) {
			continue
		}
		c := *a
		if r, ok := f.roles[a.RoleID]; ok {
			c.RoleCode, c.RoleName = r.RoleCode, r.Name
		}
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeRepo) UserRoles(ctx context.Context, tenantID, id uuid.UUID, now time.Time) ([]Role, error) {
	u, _ := f.GetUser(ctx, tenantID, id)
	if u == nil {
		return nil, nil
	}
	var out []Role
	for _, a := range f.assignments {
		if a.TenantID == tenantID && a.UserID == u.ID && active(a, now) {
			out = append(out, *f.roles[a.RoleID])
		}
	}
	return out, nil
}

func (f *fakeRepo) WriteAudit(_ context.Context, e AuditEntry) error {
	e.ID, e.CreatedAt = uuid.New(), time.Now()
	f.audit = append(f.audit, e)
	return nil
}

func (f *fakeRepo) ListAudit(_ context.Context, tenantID uuid.UUID, fl AuditFilters, limit, offset int) ([]AuditEntry, int, error) {
	var out []AuditEntry
	for i := len(f.audit) - 1; i >= 0; i-- {
		e := f.audit[i]
		if e.TenantID != nil && *e.TenantID == tenantID && slices.Contains(fl.ResourceTypes, e.ResourceType) &&
			(fl.Action == "" || e.Action == fl.Action) {
			out = append(out, e)
		}
	}
	total := len(out)
	end := min(offset+limit, total)
	if offset > total {
		offset = total
	}
	return out[offset:end], total, nil
}

func (f *fakeRepo) EnsureAgent(_ context.Context, in AgentInput) error {
	if agentRank[in.Role] > agentRank[f.agents[in.AuthUserID]] {
		f.agents[in.AuthUserID] = in.Role
	}
	return nil
}

func (f *fakeRepo) DeactivateAgent(_ context.Context, _ uuid.UUID, authUserID uuid.UUID) error {
	delete(f.agents, authUserID)
	return nil
}

func (f *fakeRepo) UpdateAgentContact(context.Context, uuid.UUID, uuid.UUID, string, string) error {
	return nil
}
