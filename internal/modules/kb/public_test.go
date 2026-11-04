package kb

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestPubliclyVisible(t *testing.T) {
	for _, c := range []struct {
		a    *Article
		want bool
	}{
		{&Article{IsPublic: true, Status: StatusPublished}, true},
		{&Article{IsPublic: true, Status: StatusDraft}, false},
		{&Article{IsPublic: true, Status: StatusArchived}, false},
		{&Article{IsPublic: false, Status: StatusPublished}, false},
		{nil, false},
	} {
		if got := publiclyVisible(c.a); got != c.want {
			t.Errorf("publiclyVisible(%+v) = %v, want %v", c.a, got, c.want)
		}
	}
}

type fakeTenants map[string]string

func (f fakeTenants) GetTenantDetails(_ context.Context, slug string) (sharedcache.TenantDetails, error) {
	if id, ok := f[slug]; ok {
		return sharedcache.TenantDetails{ID: id, Slug: slug}, nil
	}
	return sharedcache.TenantDetails{}, errors.New("not found")
}

// A slug resolves to auth-api's own tenant id, never a hash of the slug.
func TestPublicTenant(t *testing.T) {
	real := uuid.New()
	h := NewHandler(nil, zap.NewNop())
	h.SetTenantLookup(fakeTenants{"hadia": real.String()})
	resolve := func(segment string) (uuid.UUID, bool) {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant", segment)
		r := httptest.NewRequest("GET", "/", nil)
		return h.publicTenant(r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx)))
	}
	if id, ok := resolve("hadia"); !ok || id != real {
		t.Errorf("hadia = %s %v, want %s", id, ok, real)
	}
	if id, ok := resolve(real.String()); !ok || id != real {
		t.Errorf("uuid segment = %s %v", id, ok)
	}
	if _, ok := resolve("unknown"); ok {
		t.Error("an unknown slug must not resolve")
	}
}
