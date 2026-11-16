package tickets

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	pagination "github.com/Bengo-Hub/pagination"
	"github.com/google/uuid"
)

// ErrInvalidCursor is returned for a cursor the list did not hand out.
var ErrInvalidCursor = errors.New("tickets: invalid cursor")

// Cursor is a keyset position: the (created_at, id) of the last row of the previous page.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// CursorOf is the position after row (created_at, id), rounded to the microsecond Postgres stores.
func CursorOf(createdAt time.Time, id uuid.UUID) string {
	return pagination.EncodeCursor(id, createdAt)
}

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
	if rows == nil {
		rows = []T{}
	}
	p := Page[T]{Data: rows}
	if limit > 0 && len(rows) > limit {
		p.Data = rows[:limit]
		p.HasMore = true
		at, id := key(p.Data[limit-1])
		p.NextCursor = CursorOf(at, id)
	}
	return p
}
