package rbac

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func newTestService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	return NewService(repo, zap.NewNop()), repo
}

// signIn provisions a user the way the access middleware does and returns their local id.
func signIn(t *testing.T, s *Service, tenant, authUser uuid.UUID, globalRoles ...string) uuid.UUID {
	t.Helper()
	local, err := s.EnsureUser(context.Background(), Identity{TenantID: tenant, AuthUserID: authUser, Email: authUser.String()[:8] + "@acme.co.ke", GlobalRoles: globalRoles})
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	return local
}

func roleCode(t *testing.T, repo *fakeRepo, tenant uuid.UUID, code string) *Role {
	t.Helper()
	r := repo.roleByCode(tenant, code)
	if r == nil {
		t.Fatalf("role %s not seeded", code)
	}
	return r
}

func TestListRoleViewsScopesAndCounts(t *testing.T) {
	s, _ := newTestService()
	ctx, tenant := context.Background(), uuid.New()
	signIn(t, s, tenant, uuid.New(), "staff")

	views, err := s.ListRoleViews(ctx, tenant)
	if err != nil || len(views) != len(SystemRoles) {
		t.Fatalf("views %d, err %v", len(views), err)
	}
	for _, v := range views {
		if v.Scope != "global" || !v.IsSystemRole || v.IsCustomized {
			t.Errorf("%s: scope %s system %v customized %v", v.RoleCode, v.Scope, v.IsSystemRole, v.IsCustomized)
		}
		def, _ := RoleByCode(v.RoleCode)
		if v.PermissionCount != len(def.Permissions) {
			t.Errorf("%s: %d permissions, want %d", v.RoleCode, v.PermissionCount, len(def.Permissions))
		}
		if want := map[bool]int{true: 1, false: 0}[v.RoleCode == RoleAgent]; v.MemberCount != want {
			t.Errorf("%s: %d members, want %d", v.RoleCode, v.MemberCount, want)
		}
	}
}

func TestEditingSystemRoleCustomizesItAndSurvivesSeeding(t *testing.T) {
	s, repo := newTestService()
	ctx, tenant := context.Background(), uuid.New()
	signIn(t, s, tenant, uuid.New(), "staff")
	agent := roleCode(t, repo, tenant, RoleAgent)

	role, before, err := s.SetRolePermissions(ctx, tenant, nil, agent.ID, []string{Code("tickets", "view")})
	if err != nil {
		t.Fatal(err)
	}
	if role.ID != agent.ID || role.IsSystemRole || len(before) == 0 {
		t.Fatalf("role %+v before %v", role, before)
	}
	// The seeder runs again (another pod, or the cache expired) and must keep the edit.
	s.tenant.Purge()
	views, _ := s.ListRoleViews(ctx, tenant)
	for _, v := range views {
		if v.RoleCode == RoleAgent && (!v.IsCustomized || v.Scope != "tenant" || v.PermissionCount != 1) {
			t.Fatalf("customized agent role after reseed: %+v", v)
		}
	}
	// Customizing again returns the same role.
	again, err := s.CustomizeRole(ctx, tenant, RoleAgent)
	if err != nil || again.ID != agent.ID {
		t.Fatalf("customize again: %v %v", again, err)
	}
}

func TestSystemRoleCannotBeDeletedButCustomizedOneResets(t *testing.T) {
	s, repo := newTestService()
	ctx, tenant := context.Background(), uuid.New()
	signIn(t, s, tenant, uuid.New(), "member")
	supervisor := roleCode(t, repo, tenant, RoleSupervisor)

	if _, err := s.DeleteRole(ctx, tenant, supervisor.ID); !errors.Is(err, ErrSystemRole) {
		t.Fatalf("delete system role: %v", err)
	}
	if _, err := s.UpdateRoleDetails(ctx, tenant, supervisor.ID, strPtr("Team lead"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteRole(ctx, tenant, supervisor.ID); err != nil {
		t.Fatalf("delete customized role: %v", err)
	}
	// The default comes back at the next seed.
	if _, err := s.ListRoleViews(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if r := roleCode(t, repo, tenant, RoleSupervisor); !r.IsSystemRole || r.Name != "Supervisor" {
		t.Fatalf("supervisor not restored: %+v", r)
	}
}

func TestDeleteRoleInUse(t *testing.T) {
	s, _ := newTestService()
	ctx, tenant := context.Background(), uuid.New()
	local := signIn(t, s, tenant, uuid.New(), "member")
	role, err := s.CreateCustomRole(ctx, tenant, RoleInput{Name: "Night desk", PermissionCodes: []string{Code("tickets", "view")}})
	if err != nil || role.RoleCode != "night_desk" {
		t.Fatalf("create: %+v %v", role, err)
	}
	if _, err := s.AssignRole(ctx, tenant, local, role.ID, uuid.New(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteRole(ctx, tenant, role.ID); !errors.Is(err, ErrRoleInUse) {
		t.Fatalf("delete held role: %v", err)
	}
}

func TestCreateCustomRoleValidation(t *testing.T) {
	s, _ := newTestService()
	ctx, tenant := context.Background(), uuid.New()
	cases := map[string]struct {
		in   RoleInput
		want error
	}{
		"system code":        {RoleInput{Name: "Agent", Code: RoleAgent}, ErrRoleCodeTaken},
		"unknown permission": {RoleInput{Name: "X", PermissionCodes: []string{"ticketing.nope.view"}}, ErrUnknownPermission},
		"no name":            {RoleInput{Name: " "}, ErrInvalidRole},
		"bad code":           {RoleInput{Name: "Ok", Code: "9lives"}, ErrInvalidRole},
	}
	for name, c := range cases {
		if _, err := s.CreateCustomRole(ctx, tenant, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	if _, err := s.CreateCustomRole(ctx, tenant, RoleInput{Name: "Billing desk"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCustomRole(ctx, tenant, RoleInput{Name: "Billing Desk"}); !errors.Is(err, ErrRoleCodeTaken) {
		t.Fatalf("duplicate code: %v", err)
	}
}

func TestCloneRoleCopiesPermissions(t *testing.T) {
	s, repo := newTestService()
	ctx, tenant := context.Background(), uuid.New()
	signIn(t, s, tenant, uuid.New(), "member")
	agent := roleCode(t, repo, tenant, RoleAgent)
	clone, err := s.CloneRole(ctx, tenant, agent.ID, RoleInput{Name: "Senior agent"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.RolePermissionCodes(ctx, tenant, clone.ID)
	want, _ := s.RolePermissionCodes(ctx, tenant, agent.ID)
	if !slices.Equal(got, want) || clone.IsSystemRole || clone.Description == nil {
		t.Fatalf("clone %+v perms %v want %v", clone, got, want)
	}
}

func TestRolesAreTenantScoped(t *testing.T) {
	s, repo := newTestService()
	ctx, tenantA, tenantB := context.Background(), uuid.New(), uuid.New()
	signIn(t, s, tenantA, uuid.New(), "member")
	signIn(t, s, tenantB, uuid.New(), "member")
	agentA := roleCode(t, repo, tenantA, RoleAgent)
	if _, err := s.RolePermissionCodes(ctx, tenantB, agentA.ID); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("other tenant's role: %v", err)
	}
	if _, _, err := s.SetRolePermissions(ctx, tenantB, nil, agentA.ID, []string{}); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("edit other tenant's role: %v", err)
	}
}

func TestSelfLockout(t *testing.T) {
	s, repo := newTestService()
	ctx, tenant, admin := context.Background(), uuid.New(), uuid.New()
	local := signIn(t, s, tenant, admin, "manager")
	adminRole := roleCode(t, repo, tenant, RoleAdmin)
	if _, err := s.AssignRole(ctx, tenant, local, adminRole.ID, uuid.New(), nil); err != nil {
		t.Fatal(err)
	}
	guard := admin

	if _, _, err := s.SetRolePermissions(ctx, tenant, &guard, adminRole.ID, []string{Code("tickets", "view")}); !errors.Is(err, ErrSelfLockout) {
		t.Fatalf("stripping own manage: %v", err)
	}
	if _, err := s.RevokeRole(ctx, tenant, &guard, local, adminRole.ID); !errors.Is(err, ErrSelfLockout) {
		t.Fatalf("revoking own admin role: %v", err)
	}
	// A platform owner or superuser (no guard) may do both.
	if _, err := s.RevokeRole(ctx, tenant, nil, local, adminRole.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAssignAndRevoke(t *testing.T) {
	s, repo := newTestService()
	ctx, tenant, authUser := context.Background(), uuid.New(), uuid.New()
	local := signIn(t, s, tenant, authUser, "member")
	supervisor := roleCode(t, repo, tenant, RoleSupervisor)

	past := time.Now().Add(-time.Hour)
	if _, err := s.AssignRole(ctx, tenant, local, supervisor.ID, uuid.New(), &past); !errors.Is(err, ErrInvalidExpiry) {
		t.Fatalf("past expiry: %v", err)
	}
	// The auth-api user id resolves the same person.
	if _, err := s.AssignRole(ctx, tenant, authUser, supervisor.ID, uuid.New(), nil); err != nil {
		t.Fatal(err)
	}
	if repo.agents[authUser] != "supervisor" {
		t.Fatalf("a staff role must give an agent profile: %v", repo.agents)
	}
	roles, _ := s.GetUserRoles(ctx, tenant, local)
	if len(roles) != 2 {
		t.Fatalf("roles %v", roles)
	}
	if _, err := s.RevokeRole(ctx, tenant, nil, local, supervisor.ID); err != nil {
		t.Fatal(err)
	}
	// The last staff role is gone, so the person stops being an agent.
	if _, still := repo.agents[authUser]; still {
		t.Fatalf("revoking the last staff role must deactivate the agent profile: %v", repo.agents)
	}
	if _, err := s.RevokeRole(ctx, tenant, nil, local, supervisor.ID); !errors.Is(err, ErrAssignmentMissing) {
		t.Fatalf("revoke twice: %v", err)
	}
	users, total, err := s.ListUsersWithRoles(ctx, tenant, "", 20, 0)
	if err != nil || total != 1 || len(users[0].Roles) != 2 {
		t.Fatalf("users %+v total %d err %v", users, total, err)
	}
	for _, held := range users[0].Roles {
		if held.RoleCode == RoleSupervisor && !held.Expired {
			t.Error("a revoked role shows as expired")
		}
	}
	if _, err := s.AssignRole(ctx, tenant, uuid.New(), supervisor.ID, uuid.New(), nil); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestSignInDoesNotRestoreRevokedDefault(t *testing.T) {
	s, repo := newTestService()
	ctx, tenant, authUser := context.Background(), uuid.New(), uuid.New()
	local := signIn(t, s, tenant, authUser, "staff")
	agent := roleCode(t, repo, tenant, RoleAgent)
	if _, err := s.RevokeRole(ctx, tenant, nil, local, agent.ID); err != nil {
		t.Fatal(err)
	}
	s.synced.Purge()
	signIn(t, s, tenant, authUser, "staff")
	if roles, _ := s.GetUserRoles(ctx, tenant, local); len(roles) != 0 {
		t.Fatalf("sign-in gave back a revoked role: %v", roles)
	}
}

func TestEnsureUserReseedsDeletedDefaultRole(t *testing.T) {
	s, repo := newTestService()
	ctx, tenant := context.Background(), uuid.New()
	signIn(t, s, tenant, uuid.New(), "member")
	// Another pod deleted the customized customer role; this pod still thinks the tenant is seeded.
	customer := roleCode(t, repo, tenant, RoleCustomer)
	_ = repo.MarkRoleCustomized(ctx, tenant, customer.ID)
	_ = repo.DeleteRole(ctx, tenant, customer.ID)
	signIn(t, s, tenant, uuid.New(), "member")
	if repo.roleByCode(tenant, RoleCustomer) == nil {
		t.Fatal("the default role was not seeded again")
	}
}

func TestAddMember(t *testing.T) {
	s, _ := newTestService()
	ctx, tenant, authUser := context.Background(), uuid.New(), uuid.New()
	u, err := s.AddMember(ctx, tenant, authUser, "new@acme.co.ke")
	if err != nil || u.AuthServiceUserID != authUser || u.Status != UserStatusActive {
		t.Fatalf("member %+v err %v", u, err)
	}
	// Adding the same person again keeps one row.
	again, _ := s.AddMember(ctx, tenant, authUser, "new@acme.co.ke")
	if again.ID != u.ID {
		t.Fatal("adding twice made two users")
	}
	if roles, _ := s.GetUserRoles(ctx, tenant, u.ID); len(roles) != 0 {
		t.Fatal("adding a member must not grant a ticketing role; sign-in or an admin does")
	}
}

func TestPermissionName(t *testing.T) {
	if got := PermissionName("tickets", "view_own"); got != "View own tickets" {
		t.Errorf("got %q", got)
	}
	if got := PermissionName("rbac", "manage"); got != "Manage roles and users" {
		t.Errorf("got %q", got)
	}
}

func strPtr(s string) *string { return &s }
