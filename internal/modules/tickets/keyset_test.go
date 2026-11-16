package tickets

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 11, 16, 10, 30, 15, 123456789, time.UTC)
	id := uuid.New()
	c, err := ParseCursor(CursorOf(at, id))
	if err != nil {
		t.Fatal(err)
	}
	// Postgres keeps microseconds, so the cursor does too.
	if c.ID != id || !c.CreatedAt.Equal(at.Truncate(time.Microsecond)) {
		t.Fatalf("round trip: got %v %v, want %v %v", c.ID, c.CreatedAt, id, at.Truncate(time.Microsecond))
	}
	if c, err := ParseCursor(""); c != nil || err != nil {
		t.Fatalf("empty cursor is the first page: %v %v", c, err)
	}
	for _, bad := range []string{"not-base64!", "Zm9v", "MDAwMDAwMDAtMDAwMC0wMDAwLTAwMDAtMDAwMDAwMDAwMDAwfDE="} {
		if _, err := ParseCursor(bad); err != ErrInvalidCursor {
			t.Errorf("%q: got %v, want ErrInvalidCursor", bad, err)
		}
	}
}

func TestPageFromSetsNextCursor(t *testing.T) {
	base := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	rows := make([]*Ticket, 4)
	for i := range rows {
		rows[i] = &Ticket{ID: uuid.New(), CreatedAt: base.Add(-time.Duration(i) * time.Minute)}
	}
	p := pageFrom(rows, 3, ticketKey)
	if !p.HasMore || len(p.Data) != 3 {
		t.Fatalf("limit+1 rows must give a full page with more: %+v", p)
	}
	c, err := ParseCursor(p.NextCursor)
	if err != nil || c.ID != rows[2].ID || !c.CreatedAt.Equal(rows[2].CreatedAt) {
		t.Fatalf("next cursor must point at the last row kept: %v %v", c, err)
	}
	last := pageFrom(rows[:2], 3, ticketKey)
	if last.HasMore || last.NextCursor != "" || len(last.Data) != 2 {
		t.Fatalf("a short page is the last: %+v", last)
	}
	if empty := pageFrom[*Ticket](nil, 3, ticketKey); empty.Data == nil {
		t.Fatal("an empty page encodes data as [], not null")
	}
}

func TestParsePageParamsBoundsTheLimit(t *testing.T) {
	cases := map[string]int{"": 25, "limit=0": 25, "limit=-3": 25, "limit=10": 10, "limit=1000": 100, "limit=x": 25}
	for q, want := range cases {
		p, err := ParsePageParams(httptest.NewRequest("GET", "/tickets?"+q, nil))
		if err != nil || p.Limit != want {
			t.Errorf("%q: limit %d err %v, want %d", q, p.Limit, err, want)
		}
	}
	if _, err := ParsePageParams(httptest.NewRequest("GET", "/tickets?cursor=garbage!", nil)); err != ErrInvalidCursor {
		t.Errorf("bad cursor: %v", err)
	}
}
