package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter provides Redis-backed rate limiting using INCR + EXPIRE.
type Limiter struct {
	rdb *redis.Client
}

// New creates a new rate limiter backed by the given Redis client.
func New(rdb *redis.Client) *Limiter {
	return &Limiter{rdb: rdb}
}

// Allow returns true if the key is within the allowed count during window.
// First call on a key sets TTL = window.
func (l *Limiter) Allow(ctx context.Context, key string, maxCount int, window time.Duration) (bool, error) {
	pipe := l.rdb.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)

	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("ratelimit: %w", err)
	}

	return incr.Val() <= int64(maxCount), nil
}

// IPKey returns a namespaced rate-limit key for an IP address.
func IPKey(tenantSlug, ip string) string {
	return fmt.Sprintf("rl:ip:%s:%s", tenantSlug, ip)
}

// EmailKey returns a namespaced rate-limit key for an email address.
func EmailKey(tenantSlug, email string) string {
	return fmt.Sprintf("rl:email:%s:%s", tenantSlug, email)
}
