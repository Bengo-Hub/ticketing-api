package tickets

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	pagination "github.com/Bengo-Hub/pagination"
	"github.com/google/uuid"
)

// ErrInvalidCursor is returned for a cursor the list did not hand out.
var ErrInvalidCursor = errors.New("tickets: invalid cursor")

// Cursor is a keyset position: the (created_at, id) of the last row of the previous page. Under
// sort=sla_due it is the (rank, due key, id) of that row instead: Ranked is set, Rank is the SLA
// rank and CreatedAt holds the SLA due key (sla_sort.go).
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
	Rank      int
	Ranked    bool
}

// CursorOf is the position after row (created_at, id), rounded to the microsecond Postgres stores.
func CursorOf(createdAt time.Time, id uuid.UUID) string {
	return pagination.EncodeCursor(id, createdAt)
}

// rankedCursorSep separates the rank from the shared cursor. The shared cursor is URL-safe base64,
// which never contains a dot, so a dot marks a ranked cursor unambiguously.
const rankedCursorSep = "."

// RankedCursorOf is the position after a row of a ranked order (rank, key, id): the rank, a dot,
// then the shared (id, time) cursor of the key.
func RankedCursorOf(rank int, key time.Time, id uuid.UUID) string {
	return strconv.Itoa(rank) + rankedCursorSep + pagination.EncodeCursor(id, key)
}

// ParseCursor decodes a cursor from the query string. An empty cursor is the first page (nil).
func ParseCursor(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	c := &Cursor{}
	if rank, rest, ok := strings.Cut(raw, rankedCursorSep); ok {
		n, err := strconv.Atoi(rank)
		if err != nil || n < 0 || n > maxSLARank {
			return nil, ErrInvalidCursor
		}
		c.Rank, c.Ranked, raw = n, true, rest
	}
	id, at, err := pagination.DecodeCursor(raw)
	if err != nil || id == uuid.Nil {
		return nil, ErrInvalidCursor
	}
	c.CreatedAt, c.ID = at, id
	return c, nil
}

// Page is one keyset page. The UI asks for the next page with ?cursor=<next_cursor> while
// has_more is true; there is no total.
type Page[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// PageParams are the cursor and the page size of a keyset list.
type PageParams struct {
	After *Cursor
	Limit int
}

// Page size bounds. Lists fetch Limit+1 rows to learn whether another page exists.
const (
	defaultPageLimit = 25
	maxPageLimit     = 100
)

// ParsePageParams reads ?cursor= and ?limit= (default 25, at most 100).
func ParsePageParams(r *http.Request) (PageParams, error) {
	q := r.URL.Query()
	after, err := ParseCursor(q.Get("cursor"))
	if err != nil {
		return PageParams{}, err
	}
	return PageParams{After: after, Limit: clampLimit(q.Get("limit"), defaultPageLimit, maxPageLimit)}, nil
}

// clampLimit parses a page size, falling back to def and capping at max.
func clampLimit(raw string, def, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// pageFrom trims a Limit+1 fetch to Limit rows and sets the next cursor from the last row kept.
func pageFrom[T any](rows []T, limit int, key func(T) (time.Time, uuid.UUID)) Page[T] {
	return pageWithCursor(rows, limit, func(row T) string {
		at, id := key(row)
		return CursorOf(at, id)
	})
}

// pageWithCursor trims a Limit+1 fetch to Limit rows and sets the next cursor to cursor(last row
// kept), for orders whose position is more than (time, id).
func pageWithCursor[T any](rows []T, limit int, cursor func(T) string) Page[T] {
	if rows == nil {
		rows = []T{}
	}
	p := Page[T]{Data: rows}
	if limit > 0 && len(rows) > limit {
		p.Data = rows[:limit]
		p.HasMore = true
		p.NextCursor = cursor(p.Data[limit-1])
	}
	return p
}
