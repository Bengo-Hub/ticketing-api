package tenantcache

import (
	"context"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
)

// Lookup resolves a tenant slug to auth-api's tenant; *Resolver implements it.
type Lookup interface {
	GetTenantDetails(ctx context.Context, slug string) (sharedcache.TenantDetails, error)
}

// PublicTenantID resolves the tenant segment of a public route: a UUID is used as-is, a slug is
// looked up (Redis, then auth-api). An unknown slug is false, so the caller answers 404 rather
// than storing data under a tenant no one can see. Tenant ids are auth-api's random UUIDs, never
// derived from the slug.
func PublicTenantID(ctx context.Context, l Lookup, raw string) (uuid.UUID, bool) {
	if id, err := uuid.Parse(raw); err == nil {
		return id, id != uuid.Nil
	}
	if raw == "" || l == nil {
		return uuid.Nil, false
	}
	d, err := l.GetTenantDetails(ctx, raw)
	if err != nil {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(d.ID)
	return id, err == nil && id != uuid.Nil
}
