package rbac

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// fakeMembers stands in for auth-api's S2S member add.
type fakeMembers struct {
	userID uuid.UUID
	roles  []string
	calls  int
	err    error
}

func (f *fakeMembers) AddMember(_ context.Context, _ uuid.UUID, _ string, roles []string) (uuid.UUID, error) {
	f.calls++
	f.roles = roles
	return f.userID, f.err
}

type adminFixture struct {
	svc     *Service
	repo    *fakeRepo
	members *fakeMembers
	h       *Handler
	tenant  uuid.UUID
}

func newAdminFixture() *adminFixture {
	svc, repo := newTestService()
	members := &fakeMembers{userID: uuid.New()}
	return &adminFixture{svc: svc, repo: repo, members: members, h: NewHandler(svc, members, zap.NewNop()), tenant: uuid.New()}
}

// caller builds the resolved access of a signed-in user holding the given ticketing roles.
func (f *adminFixture) caller(t *testing.T, globalRoles []string, ticketingRoles ...string) *access.Access {
	t.Helper()
	authUser := uuid.New()
	claims := &authclient.Claims{TenantID: f.tenant.String(), Roles: globalRoles, Email: "caller@acme.co.ke"}
	claims.Subject = authUser.String()
	local := signIn(t, f.svc, f.tenant, authUser, globalRoles...)
	for _, code := range ticketingRoles {
		role := f.repo.roleByCode(f.tenant, code)
		if _, err := f.svc.AssignRole(context.Background(), f.tenant, local, role.ID, uuid.New(), nil); err != nil {
			t.Fatal(err)
		}
	}
	g, _ := f.repo.Grants(context.Background(), f.tenant, local, authUser, f.svc.now())
	return &access.Access{
		TenantID: f.tenant, AuthUserID: authUser, LocalUserID: local, Email: claims.Email,
		Roles: g.Roles, Perms: g.Perms, Claims: claims,
		Bypass: claims.IsSuperuser(),
	}
}

func (f *adminFixture) do(a *access.Access, method, path string, body any) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(access.With(req.Context(), a)))
		})
	})
	f.h.RegisterRoutes(r)
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

func TestAdminRoutesPermissionGates(t *testing.T) {
	f := newAdminFixture()
	agent := f.caller(t, []string{"staff"})
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/ticketing/roles"},
		{http.MethodGet, "/ticketing/permissions"},
		{http.MethodGet, "/ticketing/users"},
		{http.MethodGet, "/ticketing/rbac/audit-logs"},
		{http.MethodPost, "/ticketing/roles"},
		{http.MethodPost, "/ticketing/users"},
		{http.MethodDelete, "/ticketing/users/" + uuid.NewString() + "/roles/" + uuid.NewString()},
	} {
		if rec := f.do(agent, c.method, c.path, map[string]any{}); rec.Code != http.StatusForbidden {
			t.Errorf("agent %s %s: %d", c.method, c.path, rec.Code)
		}
	}
	// The old replace-all route is gone.
	if rec := f.do(agent, http.MethodPut, "/ticketing/users/"+uuid.NewString()+"/roles", map[string]any{"roles": []string{"agent"}}); rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Errorf("PUT /users/{id}/roles must be gone: %d", rec.Code)
	}
	if rec := f.do(agent, http.MethodGet, "/ticketing/auth/me", nil); rec.Code != http.StatusOK {
		t.Errorf("auth/me: %d", rec.Code)
	}
}

func TestRoleEditingNeedsAdminTier(t *testing.T) {
	f := newAdminFixture()
	// A supervisor is not admin tier and cannot edit roles.
	supervisor := f.caller(t, []string{"manager"}, RoleSupervisor)
	rec := f.do(supervisor, http.MethodPost, "/ticketing/roles", map[string]any{"name": "Night desk"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("supervisor creating a role: %d %s", rec.Code, rec.Body)
	}
	// ticketing_admin edits roles without being an organisation admin (owner decision 2026-11-17).
	delegated := f.caller(t, []string{"manager"}, RoleAdmin)
	rec = f.do(delegated, http.MethodPost, "/ticketing/roles", map[string]any{"name": "Night shift"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("ticketing_admin creating a role: %d %s", rec.Code, rec.Body)
	}
	admin := f.caller(t, []string{"admin"})
	rec = f.do(admin, http.MethodPost, "/ticketing/roles", map[string]any{"name": "Night desk", "permission_codes": []string{Code("tickets", "view")}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin creating a role: %d %s", rec.Code, rec.Body)
	}
	var role map[string]any
	decode(t, rec, &role)
	if role["role_code"] != "night_desk" || role["is_system_role"] != false {
		t.Errorf("role %v", role)
	}
	// One audit row per role created: the delegated ticketing_admin's and the admin's.
	if len(f.repo.audit) != 2 || f.repo.audit[1].Action != "role.created" || f.repo.audit[1].ResourceType != "role" {
		t.Errorf("audit %+v", f.repo.audit)
	}
}

func TestListRolesShape(t *testing.T) {
	f := newAdminFixture()
	admin := f.caller(t, []string{"admin"})
	rec := f.do(admin, http.MethodGet, "/ticketing/roles", nil)
	var body struct {
		Roles []map[string]any `json:"roles"`
	}
	decode(t, rec, &body)
	if rec.Code != http.StatusOK || len(body.Roles) != len(SystemRoles) {
		t.Fatalf("%d %v", rec.Code, body)
	}
	for _, k := range []string{"id", "tenant_id", "role_code", "name", "description", "is_system_role", "scope", "is_customized", "member_count", "permission_count", "created_at", "updated_at"} {
		if _, ok := body.Roles[0][k]; !ok {
			t.Errorf("role is missing %s: %v", k, body.Roles[0])
		}
	}
	if _, ok := body.Roles[0]["permissions"]; ok {
		t.Error("the role list carries counts, not permission lists")
	}
}

func TestSystemRoleDeleteRefusedOverHTTP(t *testing.T) {
	f := newAdminFixture()
	admin := f.caller(t, []string{"admin"})
	agent := f.repo.roleByCode(f.tenant, RoleAgent)
	if rec := f.do(admin, http.MethodDelete, "/ticketing/roles/"+agent.ID.String(), nil); rec.Code != http.StatusConflict {
		t.Fatalf("delete system role: %d", rec.Code)
	}
	rec := f.do(admin, http.MethodPut, "/ticketing/roles/"+agent.ID.String()+"/permissions", map[string]any{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing permission_codes: %d", rec.Code)
	}
	rec = f.do(admin, http.MethodPost, "/ticketing/roles/customize", map[string]any{"role_code": RoleAgent})
	var role map[string]any
	decode(t, rec, &role)
	if rec.Code != http.StatusOK || role["id"] != agent.ID.String() || role["is_system_role"] != false {
		t.Fatalf("customize: %d %v", rec.Code, role)
	}
	rec = f.do(admin, http.MethodGet, "/ticketing/roles/"+agent.ID.String()+"/permissions", nil)
	var perms struct {
		PermissionCodes []string `json:"permission_codes"`
	}
	decode(t, rec, &perms)
	if len(perms.PermissionCodes) == 0 {
		t.Fatal("customizing keeps the role's permissions")
	}
}

func TestAssignRoleRefusesEscalation(t *testing.T) {
	f := newAdminFixture()
	target := signIn(t, f.svc, f.tenant, uuid.New(), "member")
	adminRole := f.repo.roleByCode(f.tenant, RoleAdmin)
	agentRole := f.repo.roleByCode(f.tenant, RoleAgent)

	// A custom role that may manage users but holds only some of the desk.
	limited, err := f.svc.CreateCustomRole(context.Background(), f.tenant, RoleInput{
		Name: "People lead", PermissionCodes: append([]string{ManagePermission, Code("rbac", "view")}, SystemRoles[2].Permissions...),
	})
	if err != nil {
		t.Fatal(err)
	}
	lead := f.caller(t, []string{"member"}, limited.RoleCode)
	path := "/ticketing/users/" + target.String() + "/roles"
	if rec := f.do(lead, http.MethodPost, path, map[string]any{"role_id": adminRole.ID}); rec.Code != http.StatusForbidden {
		t.Fatalf("granting a wider role: %d %s", rec.Code, rec.Body)
	}
	rec := f.do(lead, http.MethodPost, path, map[string]any{"role_id": agentRole.ID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("granting a role within their own access: %d %s", rec.Code, rec.Body)
	}
	if f.repo.audit[len(f.repo.audit)-1].Action != "role.assigned" {
		t.Errorf("audit %+v", f.repo.audit)
	}
	// Revoking is admin tier only.
	if rec := f.do(lead, http.MethodDelete, path+"/"+agentRole.ID.String(), nil); rec.Code != http.StatusForbidden {
		t.Fatalf("lead revoking: %d", rec.Code)
	}
	admin := f.caller(t, []string{"admin"})
	if rec := f.do(admin, http.MethodDelete, path+"/"+agentRole.ID.String(), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("admin revoking: %d %s", rec.Code, rec.Body)
	}
}

func TestCreateUser(t *testing.T) {
	f := newAdminFixture()
	admin := f.caller(t, []string{"admin"})

	if rec := f.do(admin, http.MethodPost, "/ticketing/users", map[string]any{"email": "a@acme.co.ke", "roles": []string{"Super-Admin"}}); rec.Code != http.StatusForbidden || f.members.calls != 0 {
		t.Fatalf("admin-equivalent role: %d calls %d", rec.Code, f.members.calls)
	}
	if rec := f.do(admin, http.MethodPost, "/ticketing/users", map[string]any{"email": "not an email"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad email: %d", rec.Code)
	}
	rec := f.do(admin, http.MethodPost, "/ticketing/users", map[string]any{"email": "new@acme.co.ke", "roles": []string{" Staff "}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var body map[string]any
	decode(t, rec, &body)
	if body["email"] != "new@acme.co.ke" || body["tenant_id"] != f.tenant.String() || body["created"] != true || body["user_id"] == nil {
		t.Errorf("body %v", body)
	}
	if len(f.members.roles) != 1 || f.members.roles[0] != "staff" {
		t.Errorf("roles sent to auth-api: %v", f.members.roles)
	}
	u, _ := f.repo.FindUser(context.Background(), f.tenant, f.members.userID)
	if u == nil {
		t.Fatal("the member was not recorded")
	}

	supervisor := f.caller(t, []string{"manager"}, RoleSupervisor)
	if rec := f.do(supervisor, http.MethodPost, "/ticketing/users", map[string]any{"email": "b@acme.co.ke"}); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin adding a user: %d", rec.Code)
	}

	f.members.err = errors.New("auth-api down")
	if rec := f.do(admin, http.MethodPost, "/ticketing/users", map[string]any{"email": "c@acme.co.ke"}); rec.Code != http.StatusInternalServerError {
		t.Fatalf("auth-api failure: %d", rec.Code)
	}
}

func TestListUsersAndAuditPages(t *testing.T) {
	f := newAdminFixture()
	admin := f.caller(t, []string{"admin"})
	for range 3 {
		signIn(t, f.svc, f.tenant, uuid.New(), "member")
	}
	rec := f.do(admin, http.MethodGet, "/ticketing/users?limit=2&page=1", nil)
	var page struct {
		Data    []map[string]any `json:"data"`
		Total   int              `json:"total"`
		Limit   int              `json:"limit"`
		Page    int              `json:"page"`
		HasMore bool             `json:"hasMore"`
	}
	decode(t, rec, &page)
	if rec.Code != http.StatusOK || page.Total != 4 || len(page.Data) != 2 || !page.HasMore || page.Limit != 2 {
		t.Fatalf("users page: %d %+v", rec.Code, page)
	}
	for _, k := range []string{"id", "auth_service_user_id", "email", "status", "roles"} {
		if _, ok := page.Data[0][k]; !ok {
			t.Errorf("user is missing %s", k)
		}
	}

	userID := page.Data[0]["id"].(string)
	if rec := f.do(admin, http.MethodGet, "/ticketing/users/"+userID, nil); rec.Code != http.StatusOK {
		t.Fatalf("get user: %d", rec.Code)
	}
	if rec := f.do(admin, http.MethodGet, "/ticketing/users/"+uuid.NewString(), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown user: %d", rec.Code)
	}
	rec = f.do(admin, http.MethodGet, "/ticketing/users/"+userID+"/roles", nil)
	var roles struct {
		Roles []map[string]any `json:"roles"`
	}
	decode(t, rec, &roles)
	if rec.Code != http.StatusOK || len(roles.Roles) != 1 {
		t.Fatalf("user roles: %d %v", rec.Code, roles)
	}

	f.do(admin, http.MethodPost, "/ticketing/roles", map[string]any{"name": "Night desk"})
	rec = f.do(admin, http.MethodGet, "/ticketing/rbac/audit-logs", nil)
	var logs struct {
		Data  []AuditLogResponse `json:"data"`
		Total int                `json:"total"`
	}
	decode(t, rec, &logs)
	if rec.Code != http.StatusOK || logs.Total != 1 || logs.Data[0].Action != "role.created" || logs.Data[0].UserEmail != "caller@acme.co.ke" {
		t.Fatalf("audit logs: %d %+v", rec.Code, logs)
	}
}

func TestListPermissionsShape(t *testing.T) {
	f := newAdminFixture()
	admin := f.caller(t, []string{"admin"})
	rec := f.do(admin, http.MethodGet, "/ticketing/permissions", nil)
	var body struct {
		Permissions []PermissionRecord `json:"permissions"`
	}
	decode(t, rec, &body)
	if rec.Code != http.StatusOK || len(body.Permissions) != len(AllCodes()) || body.Permissions[0].Name == "" {
		t.Fatalf("%d %d", rec.Code, len(body.Permissions))
	}
}

func TestParseListDay(t *testing.T) {
	start, end, ok := parseListDay("2026-11-17")
	if !ok || start.UTC().Hour() != 21 || end.Sub(start).Hours() != 24 {
		t.Fatalf("%v %v %v", start, end, ok)
	}
	if _, _, ok := parseListDay("17/11/2026"); ok {
		t.Fatal("a bad day must be ignored")
	}
}
