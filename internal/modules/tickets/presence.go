package tickets

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// PresenceState is what an agent is doing on a ticket.
type PresenceState string

const (
	PresenceViewing PresenceState = "viewing"
	PresenceTyping  PresenceState = "typing"
	// PresenceLeft is only broadcast, when an agent leaves; it is never stored.
	PresenceLeft PresenceState = "left"
)

// presenceTTL is how long a presence lasts without a refresh. The UI refreshes about every 10 s
// while the ticket is open and on each typing burst.
const presenceTTL = 30 * time.Second

// PresenceEntry is one agent on a ticket.
type PresenceEntry struct {
	UserID    uuid.UUID     `json:"user_id"`
	AgentID   *uuid.UUID    `json:"agent_id,omitempty"`
	Name      string        `json:"name"`
	State     PresenceState `json:"state"`
	ExpiresAt time.Time     `json:"expires_at"`
}

// PresenceStore keeps short-lived presence per ticket.
type PresenceStore interface {
	// Touch stores e until now+ttl and reports whether the state is new for this user (a new
	// arrival, an expired one coming back, or viewing switched to typing).
	Touch(ctx context.Context, tenantID, ticketID uuid.UUID, e PresenceEntry, ttl time.Duration) (changed bool, err error)
	// Remove forgets the user; removed reports whether they were present.
	Remove(ctx context.Context, tenantID, ticketID, userID uuid.UUID) (removed bool, err error)
	// List returns who is on the ticket now.
	List(ctx context.Context, tenantID, ticketID uuid.UUID) ([]PresenceEntry, error)
}

// Presence records collision presence and broadcasts presence.changed on the ticket SSE stream,
// which only staff can open.
type Presence struct {
	store PresenceStore
	sse   SSEPublisher
	now   func() time.Time
}

// NewPresence creates the presence service. store may be nil (no Redis): every call then fails
// with ErrPresenceUnavailable.
func NewPresence(store PresenceStore, sse SSEPublisher) *Presence {
	return &Presence{store: store, sse: sse, now: time.Now}
}

// Set records the user on the ticket and broadcasts when their state changed.
func (p *Presence) Set(ctx context.Context, tenantID, ticketID uuid.UUID, e PresenceEntry) error {
	if p == nil || p.store == nil {
		return ErrPresenceUnavailable
	}
	if e.State != PresenceViewing && e.State != PresenceTyping {
		return invalid("state must be viewing or typing")
	}
	e.ExpiresAt = p.now().Add(presenceTTL).UTC()
	changed, err := p.store.Touch(ctx, tenantID, ticketID, e, presenceTTL)
	if err != nil {
		return err
	}
	if changed {
		p.broadcast(tenantID, ticketID, e)
	}
	return nil
}

// Leave forgets the user on the ticket and broadcasts it.
func (p *Presence) Leave(ctx context.Context, tenantID, ticketID uuid.UUID, e PresenceEntry) error {
	if p == nil || p.store == nil {
		return ErrPresenceUnavailable
	}
	removed, err := p.store.Remove(ctx, tenantID, ticketID, e.UserID)
	if err != nil {
		return err
	}
	if removed {
		e.State = PresenceLeft
		p.broadcast(tenantID, ticketID, e)
	}
	return nil
}

// List returns who is viewing or typing on the ticket.
func (p *Presence) List(ctx context.Context, tenantID, ticketID uuid.UUID) ([]PresenceEntry, error) {
	if p == nil || p.store == nil {
		return nil, ErrPresenceUnavailable
	}
	return p.store.List(ctx, tenantID, ticketID)
}

func (p *Presence) broadcast(tenantID, ticketID uuid.UUID, e PresenceEntry) {
	if p.sse == nil {
		return
	}
	p.sse.Publish(tenantID, "presence.changed", map[string]any{
		"ticket_id": ticketID, "user_id": e.UserID, "agent_id": e.AgentID, "name": e.Name, "state": e.State,
		"expires_at": e.ExpiresAt,
	})
}

// RedisPresenceStore keeps presence in Redis: a sorted set of user ids scored by expiry time and
// a hash of their entries, both per ticket, both expiring shortly after the last refresh.
type RedisPresenceStore struct {
	rdb redis.UniversalClient
}

// NewRedisPresenceStore returns nil for a nil client, so presence reports itself unavailable.
func NewRedisPresenceStore(rdb redis.UniversalClient) PresenceStore {
	if rdb == nil {
		return nil
	}
	return &RedisPresenceStore{rdb: rdb}
}

func presenceKeys(tenantID, ticketID uuid.UUID) (zset, hash string) {
	base := "ticketing:presence:" + tenantID.String() + ":" + ticketID.String()
	return base, base + ":who"
}

func msString(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

// Touch implements PresenceStore.
func (s *RedisPresenceStore) Touch(ctx context.Context, tenantID, ticketID uuid.UUID, e PresenceEntry, ttl time.Duration) (bool, error) {
	zset, hash := presenceKeys(tenantID, ticketID)
	user := e.UserID.String()
	now := time.Now()
	changed := true
	score, err := s.rdb.ZScore(ctx, zset, user).Result()
	switch {
	case errors.Is(err, redis.Nil):
	case err != nil:
		return false, err
	case int64(score) > now.UnixMilli():
		prev, err := s.rdb.HGet(ctx, hash, user).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return false, err
		}
		var old PresenceEntry
		if json.Unmarshal([]byte(prev), &old) == nil && old.State == e.State {
			changed = false
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	pipe := s.rdb.TxPipeline()
	pipe.ZRemRangeByScore(ctx, zset, "-inf", msString(now))
	pipe.ZAdd(ctx, zset, redis.Z{Score: float64(now.Add(ttl).UnixMilli()), Member: user})
	pipe.HSet(ctx, hash, user, data)
	pipe.Expire(ctx, zset, 2*ttl)
	pipe.Expire(ctx, hash, 2*ttl)
	_, err = pipe.Exec(ctx)
	return changed, err
}

// Remove implements PresenceStore.
func (s *RedisPresenceStore) Remove(ctx context.Context, tenantID, ticketID, userID uuid.UUID) (bool, error) {
	zset, hash := presenceKeys(tenantID, ticketID)
	n, err := s.rdb.ZRem(ctx, zset, userID.String()).Result()
	if err != nil {
		return false, err
	}
	if err := s.rdb.HDel(ctx, hash, userID.String()).Err(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// List implements PresenceStore.
func (s *RedisPresenceStore) List(ctx context.Context, tenantID, ticketID uuid.UUID) ([]PresenceEntry, error) {
	zset, hash := presenceKeys(tenantID, ticketID)
	now := msString(time.Now())
	users, err := s.rdb.ZRangeByScore(ctx, zset, &redis.ZRangeBy{Min: "(" + now, Max: "+inf"}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]PresenceEntry, 0, len(users))
	if len(users) == 0 {
		return out, nil
	}
	vals, err := s.rdb.HMGet(ctx, hash, users...).Result()
	if err != nil {
		return nil, err
	}
	for _, v := range vals {
		raw, ok := v.(string)
		if !ok {
			continue
		}
		var e PresenceEntry
		if json.Unmarshal([]byte(raw), &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}
