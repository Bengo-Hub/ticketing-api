package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// answerTTL is how long a request key's answer is kept, so a retried request gets the same answer
// without a second model call or a second charge.
const answerTTL = 24 * time.Hour

// AnswerCache keeps the answer of each paid request key.
type AnswerCache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration)
}

// redisAnswers is the Redis AnswerCache. A Redis failure reads as a miss and a skipped write: the
// wallet's own key still keeps the charge to one.
type redisAnswers struct{ rdb redis.UniversalClient }

// NewRedisAnswerCache returns the Redis cache, or nil without Redis.
func NewRedisAnswerCache(rdb redis.UniversalClient) AnswerCache {
	if rdb == nil {
		return nil
	}
	return &redisAnswers{rdb: rdb}
}

func (c *redisAnswers) Get(ctx context.Context, key string) ([]byte, bool) {
	b, err := c.rdb.Get(ctx, "ticketing:ai:answer:"+key).Bytes()
	return b, err == nil
}

func (c *redisAnswers) Set(ctx context.Context, key string, val []byte, ttl time.Duration) {
	_ = c.rdb.Set(ctx, "ticketing:ai:answer:"+key, val, ttl).Err()
}

var requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,100}$`)

// RequestKey is the caller's Idempotency-Key when it is well formed, otherwise a new one, so a
// request without a key is simply never a repeat.
func RequestKey(header string) string {
	if requestKeyPattern.MatchString(header) {
		return header
	}
	return uuid.NewString()
}

// creditKey is the wallet key of one request: feature, the record it is about and the request key.
// subscriptions-api keeps keys per tenant wallet, so tenants never collide.
func creditKey(f Feature, ref, requestKey string) string {
	return fmt.Sprintf("ticketing:ai:%s:%s:%s", f, ref, requestKey)
}

// paidCall is one metered AI call.
type paidCall struct {
	TenantID    uuid.UUID
	Feature     Feature
	Key         string
	Description string
}

// cacheKey scopes the kept answer to the tenant, so one tenant's request key can never read
// another tenant's answer.
func (c paidCall) cacheKey() string { return c.TenantID.String() + ":" + c.Key }

// metered runs fn, the model call, at most once per key and pays for it once:
//
//  1. a key already answered returns the kept answer, with no model call and no charge;
//  2. the wallet is checked for one credit; a refusal is a CreditError and the model never runs,
//     and an unreadable wallet is ErrCreditsUnknown (fail closed);
//  3. fn runs; when it fails nothing is charged;
//  4. one credit is spent under the key (an exempt tenant spends nothing). A replayed key is not
//     charged again; a spend refused in a race since the check withholds the answer.
//
// out must be a pointer; fn fills it, and on a repeat the kept answer is decoded into it.
func (s *Service) metered(ctx context.Context, c paidCall, out any, fn func(ctx context.Context) error) (Credits, error) {
	if s.answers != nil {
		if b, ok := s.answers.Get(ctx, c.cacheKey()); ok && json.Unmarshal(b, out) == nil {
			return Credits{Replayed: true}, nil
		}
	}
	check, err := s.wallet.Check(ctx, c.TenantID, creditsPerCall)
	if err != nil {
		s.log.Warn("ai credit check failed", zap.String("feature", string(c.Feature)), zap.Error(err))
		return Credits{}, fmt.Errorf("%w: %v", ErrCreditsUnknown, err)
	}
	if !check.Allowed {
		return Credits{}, &CreditError{Reason: reasonOr(check.Reason)}
	}
	if err := fn(ctx); err != nil {
		return Credits{}, err
	}
	credits := Credits{Exempt: check.Exempt}
	if !check.Exempt {
		spent, err := s.wallet.Consume(ctx, c.TenantID, creditsPerCall, c.Key, c.Description)
		if err != nil {
			s.log.Warn("ai credit spend failed, answer withheld", zap.String("feature", string(c.Feature)), zap.Error(err))
			return Credits{}, fmt.Errorf("%w: %v", ErrCreditsUnknown, err)
		}
		if !spent.Allowed {
			return Credits{}, &CreditError{Reason: reasonOr(spent.Reason)}
		}
		credits.Exempt, credits.Replayed = spent.Exempt, spent.Replayed
		if !spent.Exempt && !spent.Replayed {
			credits.Charged = creditsPerCall
		}
	}
	if s.answers != nil {
		if b, err := json.Marshal(out); err == nil {
			s.answers.Set(context.WithoutCancel(ctx), c.cacheKey(), b, answerTTL)
		}
	}
	return credits, nil
}

func reasonOr(r string) string {
	if r == "" {
		return "insufficient_credits"
	}
	return r
}
