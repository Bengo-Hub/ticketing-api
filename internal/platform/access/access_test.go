package access

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestSatisfies(t *testing.T) {
	cases := []struct {
		held, need string
		want       bool
	}{
		{"ticketing.tickets.view", "ticketing.tickets.view", true},
		{"ticketing.tickets.manage", "ticketing.tickets.delete", true},
		{"ticketing.tickets.manage", "ticketing.tickets.view_own", true},
		{"ticketing.tickets.view", "ticketing.tickets.view_own", true},
		{"ticketing.tickets.manage_own", "ticketing.tickets.change_own", true},
		{"ticketing.tickets.manage_own", "ticketing.tickets.change", false},
		{"ticketing.tickets.view_own", "ticketing.tickets.view", false},
		{"ticketing.tickets.manage", "ticketing.kb.view", false},
		{"ticketing.comments.add", "ticketing.tickets.add", false},
		{"garbage", "ticketing.tickets.view", false},
	}
	for _, c := range cases {
		if got := Satisfies(c.held, c.need); got != c.want {
			t.Errorf("Satisfies(%q, %q) = %v, want %v", c.held, c.need, got, c.want)
		}
	}
}

func gate(a *Access, codes ...string) int {
	h := RequireAnyPermission(codes...)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if a != nil {
		req = req.WithContext(With(req.Context(), a))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestRequirePermission(t *testing.T) {
	customer := &Access{Perms: []string{"ticketing.tickets.add", "ticketing.tickets.view_own"}}
	if got := gate(customer, "ticketing.tickets.view", "ticketing.tickets.view_own"); got != http.StatusNoContent {
		t.Errorf("customer with view_own on a view-or-own route: %d", got)
	}
	if got := gate(customer, "ticketing.tickets.delete"); got != http.StatusForbidden {
		t.Errorf("customer deleting: %d", got)
	}
	if got := gate(&Access{Bypass: true}, "ticketing.rbac.manage"); got != http.StatusNoContent {
		t.Errorf("bypass: %d", got)
	}
	if got := gate(nil, "ticketing.tickets.view"); got != http.StatusUnauthorized {
		t.Errorf("no access resolved: %d", got)
	}
	supervisor := &Access{Perms: []string{"ticketing.tickets.manage"}}
	if got := gate(supervisor, "ticketing.tickets.delete"); got != http.StatusNoContent {
		t.Errorf("manage implies delete: %d", got)
	}
}

type tenants map[string]string

func (f tenants) GetTenantDetails(_ context.Context, slug string) (sharedcache.TenantDetails, error) {
	if id, ok := f[slug]; ok {
		return sharedcache.TenantDetails{ID: id}, nil
	}
	return sharedcache.TenantDetails{}, errors.New("not found")
}

func TestResolveTenant(t *testing.T) {
	acme, other := uuid.New(), uuid.New()
	lookup := tenants{"acme": acme.String(), "other": other.String()}
	user := &authclient.Claims{TenantID: acme.String(), TenantSlug: "acme"}
	owner := &authclient.Claims{TenantID: uuid.NewString(), IsPlatformOwner: true}
	ctx := context.Background()

	cases := []struct {
		name     string
		claims   *authclient.Claims
		segment  string
		override string
		want     uuid.UUID
		status   int
	}{
		{"own slug", user, "acme", "", acme, 0},
		{"own uuid", user, acme.String(), "", acme, 0},
		{"another tenant's slug", user, "other", "", uuid.Nil, http.StatusForbidden},
		{"another tenant's uuid", user, other.String(), "", uuid.Nil, http.StatusForbidden},
		{"tenantId ignored for a user", user, "acme", other.String(), acme, 0},
		{"unknown slug", user, "nobody", "", uuid.Nil, http.StatusForbidden},
		{"no tenant claim", &authclient.Claims{}, "acme", "", uuid.Nil, http.StatusForbidden},
		{"owner by slug", owner, "other", "", other, 0},
		{"owner by tenantId", owner, "acme", other.String(), other, 0},
		{"owner bad tenantId", owner, "acme", "nope", uuid.Nil, http.StatusBadRequest},
		{"owner unknown slug", owner, "nobody", "", uuid.Nil, http.StatusNotFound},
	}
	for _, c := range cases {
		got, status, _ := ResolveTenant(ctx, lookup, c.claims, c.segment, c.override)
		if got != c.want || status != c.status {
			t.Errorf("%s: got (%v, %d), want (%v, %d)", c.name, got, status, c.want, c.status)
		}
	}
	// The cache down: the token's own slug still matches its tenant.
	if got, status, _ := ResolveTenant(ctx, tenants{}, user, "ACME", ""); got != acme || status != 0 {
		t.Errorf("own slug without the cache: (%v, %d)", got, status)
	}
}

func chiTenant(r *http.Request) string { return httpware.GetTenantID(r.Context()) }

func TestTenantGuardSetsTheTenant(t *testing.T) {
	acme := uuid.New()
	var seen string
	r := chi.NewRouter()
	r.With(TenantGuard(tenants{"acme": acme.String()}, nil)).Get("/{tenant}/x", func(w http.ResponseWriter, r *http.Request) {
		seen = chiTenant(r)
		w.WriteHeader(http.StatusNoContent)
	})
	call := func(path string, c *authclient.Claims) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if c != nil {
			req = req.WithContext(authclient.ContextWithClaims(req.Context(), c))
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := call("/acme/x", &authclient.Claims{TenantID: acme.String()}); code != http.StatusNoContent || seen != acme.String() {
		t.Fatalf("own tenant: %d %q", code, seen)
	}
	if code := call("/acme/x", &authclient.Claims{TenantID: uuid.NewString()}); code != http.StatusForbidden {
		t.Fatalf("other tenant: %d", code)
	}
	if code := call("/acme/x", nil); code != http.StatusUnauthorized {
		t.Fatalf("no claims: %d", code)
	}
}
