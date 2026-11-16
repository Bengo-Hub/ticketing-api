package rbac

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

func TestMapGlobalRoles(t *testing.T) {
	cases := map[string]struct {
		roles []string
		want  string
	}{
		"superuser":           {[]string{"superuser"}, RoleAdmin},
		"admin":               {[]string{"admin"}, RoleAdmin},
		"manager":             {[]string{"manager"}, RoleSupervisor},
		"staff":               {[]string{"staff"}, RoleAgent},
		"receptionist":        {[]string{"receptionist"}, RoleAgent},
		"cashier":             {[]string{"cashier"}, RoleAgent},
		"member":              {[]string{"member"}, RoleCustomer},
		"viewer":              {[]string{"viewer"}, RoleCustomer},
		"none":                {nil, RoleCustomer},
		"strongest wins":      {[]string{"member", "staff", "manager"}, RoleSupervisor},
		"admin beats manager": {[]string{"manager", "admin"}, RoleAdmin},
		"rider":               {[]string{"rider"}, RoleCustomer},
	}
	for name, c := range cases {
		if got := MapGlobalRoles(c.roles); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestAgentRole(t *testing.T) {
	if AgentRole([]string{RoleCustomer}) != "" {
		t.Error("a customer has no agent profile")
	}
	if AgentRole([]string{RoleAgent, RoleSupervisor}) != "supervisor" || AgentRole([]string{RoleAdmin}) != "admin" {
		t.Error("the strongest staff role sets the agent profile role")
	}
}

func TestCatalogueAndRoleDefaults(t *testing.T) {
	codes := AllCodes()
	if len(codes) != len(Modules)*len(Actions) {
		t.Fatalf("catalogue has %d codes", len(codes))
	}
	known := map[string]bool{}
	for _, c := range codes {
		known[c] = true
	}
	for _, r := range SystemRoles {
		for _, p := range r.Permissions {
			if !known[p] {
				t.Errorf("role %s grants %s, which is not in the catalogue", r.Code, p)
			}
		}
	}
	customer := access.Access{Perms: PermissionsForRoles([]string{RoleCustomer})}
	if customer.Has("ticketing.tickets.view") || !customer.Has("ticketing.tickets.view_own") || !customer.Has("ticketing.comments.add") {
		t.Errorf("customer permissions: %v", customer.Perms)
	}
	agent := access.Access{Perms: PermissionsForRoles([]string{RoleAgent})}
	if !agent.Has("ticketing.tickets.view") || agent.Has("ticketing.tickets.delete") || agent.Has("ticketing.rbac.manage") {
		t.Errorf("agent permissions: %v", agent.Perms)
	}
	supervisor := access.Access{Perms: PermissionsForRoles([]string{RoleSupervisor})}
	if !supervisor.Has("ticketing.tickets.delete") || !supervisor.Has("ticketing.kb.change") || supervisor.Has("ticketing.sla.change") {
		t.Errorf("supervisor permissions: %v", supervisor.Perms)
	}
	admin := access.Access{Perms: PermissionsForRoles([]string{RoleAdmin})}
	if !admin.Has("ticketing.rbac.manage") || !admin.Has("ticketing.settings.manage") {
		t.Errorf("admin permissions: %v", admin.Perms)
	}
}

func TestBuildMe(t *testing.T) {
	user, tenant, agentID := uuid.New(), uuid.New(), uuid.New()
	me := BuildMe(&access.Access{
		TenantID: tenant, TenantSlug: "acme", AuthUserID: user, Email: "a@acme.co.ke",
		GlobalRoles: []string{"staff"}, Roles: []string{RoleAgent}, Perms: []string{"ticketing.tickets.view"},
		AgentID: &agentID,
	}, "Amina")
	b, _ := json.Marshal(me)
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	want := map[string]any{
		"user_id": user.String(), "email": "a@acme.co.ke", "name": "Amina", "tenant_id": tenant.String(),
		"tenant_slug": "acme", "service_role": RoleAgent, "agent_id": agentID.String(), "is_platform_owner": false,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for _, k := range []string{"global_roles", "permissions"} {
		if _, ok := got[k].([]any); !ok {
			t.Errorf("%s must be a list: %v", k, got[k])
		}
	}

	owner := BuildMe(&access.Access{TenantID: tenant, IsPlatformOwner: true, Bypass: true, Perms: AllCodes()}, "")
	b, _ = json.Marshal(owner)
	_ = json.Unmarshal(b, &got)
	if got["service_role"] != RoleAdmin || got["agent_id"] != nil || got["is_platform_owner"] != true {
		t.Errorf("platform owner: %v", got)
	}
}

// fakeResolver records JIT calls and returns fixed grants.
type fakeResolver struct {
	ensured []Identity
	grants  Grants
}

func (f *fakeResolver) EnsureUser(_ context.Context, id Identity) (uuid.UUID, error) {
	f.ensured = append(f.ensured, id)
	return uuid.New(), nil
}

func (f *fakeResolver) Grants(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (Grants, error) {
	return f.grants, nil
}

func resolve(t *testing.T, res Resolver, claims *authclient.Claims, tenant uuid.UUID) *access.Access {
	t.Helper()
	var got *access.Access
	h := ResolveAccess(res, zap.NewNop())(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = access.From(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := authclient.ContextWithClaims(req.Context(), claims)
	ctx = httpware.WithTenantID(ctx, tenant.String())
	h.ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
	if got == nil {
		t.Fatal("access not resolved")
	}
	return got
}

func TestResolveAccess(t *testing.T) {
	tenant := uuid.New()
	user := uuid.New()

	res := &fakeResolver{grants: Grants{Roles: []string{RoleCustomer}, Perms: PermissionsForRoles([]string{RoleCustomer})}}
	claims := &authclient.Claims{TenantID: tenant.String(), Roles: []string{"member"}, Email: "c@x.co"}
	claims.Subject = user.String()
	a := resolve(t, res, claims, tenant)
	if a.Bypass || a.AuthUserID != user || len(res.ensured) != 1 || res.ensured[0].TenantID != tenant {
		t.Fatalf("own tenant user: %+v ensured=%v", a, res.ensured)
	}
	if a.Has("ticketing.tickets.view") {
		t.Error("a customer must not see every ticket")
	}

	// A platform owner acting in another tenant gets no local user there and bypasses checks.
	res = &fakeResolver{}
	owner := &authclient.Claims{TenantID: uuid.NewString(), IsPlatformOwner: true}
	owner.Subject = uuid.NewString()
	a = resolve(t, res, owner, tenant)
	if !a.Bypass || len(res.ensured) != 0 || len(a.Perms) != len(AllCodes()) {
		t.Fatalf("platform owner: bypass=%v ensured=%d perms=%d", a.Bypass, len(res.ensured), len(a.Perms))
	}

	// A superuser bypasses permission checks in their own tenant.
	super := &authclient.Claims{TenantID: tenant.String(), Roles: []string{"superuser"}}
	super.Subject = uuid.NewString()
	if a = resolve(t, &fakeResolver{}, super, tenant); !a.Bypass {
		t.Fatal("superuser must bypass permission checks")
	}

	// An app API key without a user gets the permissions of its mapped role.
	app := &authclient.Claims{TenantID: tenant.String(), Roles: []string{"staff"}}
	app.Subject = "client-123"
	a = resolve(t, &fakeResolver{}, app, tenant)
	if a.Bypass || !a.Has("ticketing.tickets.view") || a.Has("ticketing.tickets.delete") {
		t.Fatalf("app key: %+v", a)
	}
}
