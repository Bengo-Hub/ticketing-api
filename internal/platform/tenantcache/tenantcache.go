package tenantcache

import (
	"context"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Resolver resolves tenant details using Redis cache → auth-api fallback.
// It wraps the shared cache library to provide tenant branding and contact
// info without storing tenant data locally in the ticketing service.
type Resolver struct {
	aside      *sharedcache.Aside
	authURL    string
	defaultTTL time.Duration
}

// New creates a Resolver. rdb may be nil (cache disabled, always hits auth-api).
func New(rdb *redis.Client, authURL string, ttl time.Duration, logger *zap.Logger) *Resolver {
	var aside *sharedcache.Aside
	if rdb != nil {
		aside = sharedcache.New(rdb, logger)
	}
	if ttl == 0 {
		ttl = sharedcache.DefaultTenantTTL
	}
	return &Resolver{aside: aside, authURL: authURL, defaultTTL: ttl}
}

// GetBranding returns branding details for the given tenant slug.
func (r *Resolver) GetBranding(ctx context.Context, slug string) (sharedcache.TenantBranding, error) {
	details, err := sharedcache.GetTenantDetails(ctx, r.aside, r.authURL, slug, r.defaultTTL)
	if err != nil {
		return sharedcache.TenantBranding{}, err
	}
	return sharedcache.GetTenantBranding(details), nil
}

// GetTenantDetails returns full tenant details for the given slug.
func (r *Resolver) GetTenantDetails(ctx context.Context, slug string) (sharedcache.TenantDetails, error) {
	return sharedcache.GetTenantDetails(ctx, r.aside, r.authURL, slug, r.defaultTTL)
}
