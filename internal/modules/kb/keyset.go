package kb

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	pagination "github.com/Bengo-Hub/pagination"
	"github.com/google/uuid"
)

// ErrInvalidCursor is returned for a cursor the list did not hand out.
var ErrInvalidCursor = errors.New("kb: invalid cursor")

// Cursor is a keyset position: the (created_at, id) of the last row of the previous page.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Page size bounds. A list fetches Limit+1 rows to learn whether another page exists.
const (
	defaultPageLimit = 20
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

// cursorOf is the position after an article, rounded to the microsecond Postgres stores.
func cursorOf(a *Article) string {
	return pagination.EncodeCursor(a.ID, a.CreatedAt)
}

// parsePage reads ?cursor= and ?limit= (default 20, at most 100).
func parsePage(r *http.Request) (*Cursor, int, error) {
	q := r.URL.Query()
	after, err := ParseCursor(q.Get("cursor"))
	if err != nil {
		return nil, 0, err
	}
	return after, clampLimit(q.Get("limit"), defaultPageLimit, maxPageLimit), nil
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

// normalLimit is the page size a service call uses for a filter limit.
func normalLimit(n int) int {
	if n <= 0 {
		return defaultPageLimit
	}
	if n > maxPageLimit {
		return maxPageLimit
	}
	return n
}

// pageOf trims a Limit+1 fetch to Limit rows and sets the next cursor from the last row kept.
func pageOf(rows []*Article, limit int) *ArticleList {
	if rows == nil {
		rows = []*Article{}
	}
	list := &ArticleList{Articles: rows}
	if len(rows) > limit {
		list.Articles = rows[:limit]
		list.HasMore = true
		list.NextCursor = cursorOf(list.Articles[limit-1])
	}
	return list
}
