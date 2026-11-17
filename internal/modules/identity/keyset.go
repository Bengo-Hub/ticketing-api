package identity

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	pagination "github.com/Bengo-Hub/pagination"
	"github.com/google/uuid"
)

// The agent and team lists page by keyset, newest first: (created_at, id) descending, served by
// the (tenant_id, created_at, id) index of each table. There is no total.

// ErrInvalidCursor is returned for a cursor the list did not hand out.
var ErrInvalidCursor = errors.New("identity: invalid cursor")

// Cursor is the (created_at, id) of the last row of the previous page.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Page is one keyset page. Ask for the next page with ?cursor=<next_cursor> while has_more is true.
type Page[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// Page size bounds. Lists fetch Limit+1 rows to learn whether another page exists.
const (
	defaultPageLimit = 25
	maxPageLimit     = 100
)

// ParseCursor decodes a cursor from the query string. An empty cursor is the first page (nil).
func ParseCursor(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	id, at, err := pagination.DecodeCursor(raw)
	if err != nil || id == uuid.Nil {
		return nil, ErrInvalidCursor
	}
	return &Cursor{CreatedAt: at, ID: id}, nil
}

// parsePage reads ?cursor= and ?limit= (default 25, at most 100).
func parsePage(r *http.Request) (*Cursor, int, error) {
	q := r.URL.Query()
	after, err := ParseCursor(q.Get("cursor"))
	if err != nil {
		return nil, 0, err
	}
	limit := defaultPageLimit
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = min(n, maxPageLimit)
	}
	return after, limit, nil
}

// pageFrom trims a Limit+1 fetch to Limit rows and sets the next cursor from the last row kept.
func pageFrom[T any](rows []T, limit int, key func(T) (time.Time, uuid.UUID)) Page[T] {
	if rows == nil {
		rows = []T{}
	}
	p := Page[T]{Data: rows}
	if limit > 0 && len(rows) > limit {
		p.Data = rows[:limit]
		p.HasMore = true
		at, id := key(p.Data[limit-1])
		p.NextCursor = pagination.EncodeCursor(id, at)
	}
	return p
}

// beforeRow is the keyset condition (created_at, id) < cursor as one row comparison, so the
// (tenant_id, created_at, id) index serves it.
func beforeRow(s *entsql.Selector, c *Cursor) *entsql.Predicate {
	return entsql.P(func(b *entsql.Builder) {
		b.WriteString("(").WriteString(s.C("created_at")).WriteString(", ").WriteString(s.C("id")).
			WriteString(") < (").Arg(c.CreatedAt).WriteString(", ").Arg(c.ID).WriteString(")")
	})
}

func agentKey(a *Agent) (time.Time, uuid.UUID) { return a.CreatedAt, a.ID }
func teamKey(t *Team) (time.Time, uuid.UUID)   { return t.CreatedAt, t.ID }
