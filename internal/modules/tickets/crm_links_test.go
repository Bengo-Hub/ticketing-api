package tickets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type slugLookup map[string]uuid.UUID

func (l slugLookup) GetTenantDetails(_ context.Context, slug string) (sharedcache.TenantDetails, error) {
	if id, ok := l[slug]; ok {
		return sharedcache.TenantDetails{ID: id.String(), Slug: slug}, nil
	}
	return sharedcache.TenantDetails{}, errors.New("not found")
}

// marketflow asks, before erasing a CRM contact, whether ticketing still links it. A ticket carrying
// it (in any tenant) answers yes whichever tenant asks; no ticket answers no. A slug resolves through
// auth-api (unknown: 404); the route needs the service key.
func TestCRMLinksAnswersPerTenant(t *testing.T) {
	tenantA, tenantB, linked := uuid.New(), uuid.New(), uuid.New()
	h := &CRMLinksHandler{
		referenced: func(_ context.Context, contactID uuid.UUID) (bool, error) {
			return contactID == linked, nil // a ticket under tenant A
		},
		tenants: slugLookup{"tenant-a": tenantA},
		key:     "svc-key",
		log:     zap.NewNop(),
	}
	r := chi.NewRouter()
	r.Get("/api/v1/s2s/{tenant}/crm-links/{contactID}", h.Referenced)

	ask := func(tenant, contact string, withKey bool) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/s2s/"+tenant+"/crm-links/"+contact, nil)
		if withKey {
			req.Header.Set("X-API-Key", "svc-key")
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}

	if code, body := ask(tenantA.String(), linked.String(), true); code != http.StatusOK || body["referenced"] != true || len(body) != 1 {
		t.Fatalf("linked by tenant id: %d %v", code, body)
	}
	if code, body := ask("tenant-a", linked.String(), true); code != http.StatusOK || body["referenced"] != true {
		t.Fatalf("linked by slug: %d %v", code, body)
	}
	if code, body := ask(tenantB.String(), linked.String(), true); code != http.StatusOK || body["referenced"] != true {
		t.Fatalf("a link under another tenant must count: %d %v", code, body)
	}
	if code, _ := ask("no-such-tenant", linked.String(), true); code != http.StatusNotFound {
		t.Fatalf("unknown slug: %d", code)
	}
	if code, _ := ask(tenantA.String(), "not-a-uuid", true); code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", code)
	}
	if code, _ := ask(tenantA.String(), linked.String(), false); code != http.StatusUnauthorized {
		t.Fatalf("no key: %d", code)
	}
	h.referenced = func(context.Context, uuid.UUID) (bool, error) { return false, errors.New("db down") }
	if code, _ := ask(tenantA.String(), linked.String(), true); code != http.StatusInternalServerError {
		t.Fatalf("a failed read must not answer false: %d", code)
	}
}
