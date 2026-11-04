package tenantcache

import (
	"context"
	"errors"
	"testing"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
)

type fakeLookup map[string]string

func (f fakeLookup) GetTenantDetails(_ context.Context, slug string) (sharedcache.TenantDetails, error) {
	if id, ok := f[slug]; ok {
		return sharedcache.TenantDetails{ID: id, Slug: slug}, nil
	}
	return sharedcache.TenantDetails{}, errors.New("not found")
}

// A slug resolves to auth-api's random id, never uuid5(DNS, slug); an unknown slug and a nil
// lookup are refused; a UUID segment is used as-is.
func TestPublicTenantID(t *testing.T) {
	ctx := context.Background()
	real := uuid.New()
	l := fakeLookup{"hadia": real.String()}
	if id, ok := PublicTenantID(ctx, l, "hadia"); !ok || id != real {
		t.Fatalf("slug: %v %v", id, ok)
	}
	if id, _ := PublicTenantID(ctx, l, "hadia"); id == uuid.NewSHA1(uuid.NameSpaceDNS, []byte("hadia")) {
		t.Fatal("the slug hash must never be used")
	}
	if _, ok := PublicTenantID(ctx, l, "nobody"); ok {
		t.Fatal("an unknown slug must be refused")
	}
	if _, ok := PublicTenantID(ctx, nil, "hadia"); ok {
		t.Fatal("no lookup configured must refuse, not guess")
	}
	if id, ok := PublicTenantID(ctx, l, real.String()); !ok || id != real {
		t.Fatal("a UUID segment is used as-is")
	}
	if _, ok := PublicTenantID(ctx, l, uuid.Nil.String()); ok {
		t.Fatal("the nil UUID is refused")
	}
}
