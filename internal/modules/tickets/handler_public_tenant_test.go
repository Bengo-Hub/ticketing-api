package tickets

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
)

type fakeTenants map[string]string

func (f fakeTenants) GetTenantDetails(_ context.Context, slug string) (sharedcache.TenantDetails, error) {
	if id, ok := f[slug]; ok {
		return sharedcache.TenantDetails{ID: id, Slug: slug}, nil
	}
	return sharedcache.TenantDetails{}, errors.New("not found")
}

// A public ticket lands on auth-api's real tenant id; an unknown slug is a 404, so no ticket is
// ever stored under an id no agent can see.
func TestPublicTicketTenantResolution(t *testing.T) {
	real := uuid.New()
	h := &PublicHandler{}
	h.SetTenantLookup(fakeTenants{"hadia": real.String()})
	r := httptest.NewRequest(http.MethodPost, "/", nil)

	id, err := h.resolveTenantID(r, "hadia")
	if err != nil || id != real || id == uuid.NewSHA1(uuid.NameSpaceDNS, []byte("hadia")) {
		t.Fatalf("real tenant: %v %v", id, err)
	}
	_, err = h.resolveTenantID(r, "nobody")
	rec := httptest.NewRecorder()
	h.tenantError(rec, err)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown slug must be a 404, got %d", rec.Code)
	}
	_, err = h.resolveTenantID(r, "")
	rec = httptest.NewRecorder()
	h.tenantError(rec, err)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty slug is a bad request, got %d", rec.Code)
	}
}
