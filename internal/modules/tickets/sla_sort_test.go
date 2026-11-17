package tickets

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// listRepo answers ListTickets the way the SQL does for sort=sla_due: rows strictly after the
// (rank, key, id) cursor, ordered by the same key, Limit+1 rows. It records the SLA label and
// author name lookups, so a test can see one lookup per page.
type listRepo struct {
	Repository
	tickets     []*Ticket
	labelCalls  int
	labelIDs    [][]uuid.UUID
	labels      map[uuid.UUID]SLALabel
	comments    []*TicketComment
	nameCalls   int
	nameIDs     [][]uuid.UUID
	authorNames map[uuid.UUID]string
}

// afterSLACursor reports whether t sorts strictly after the cursor in the SLA due order.
func afterSLACursor(t *Ticket, c *Cursor) bool {
	rank, key := slaSortKey(t)
	switch {
	case rank != c.Rank:
		return rank > c.Rank
	case !key.Equal(c.CreatedAt):
		return key.After(c.CreatedAt)
	default:
		return t.ID.String() > c.ID.String()
	}
}

func (r *listRepo) ListTickets(_ context.Context, _ uuid.UUID, f TicketFilter) ([]*Ticket, error) {
	if f.Sort != SortSLADue {
		panic("listRepo serves only sort=sla_due")
	}
	var out []*Ticket
	for _, t := range r.tickets {
		if f.Page.After == nil || afterSLACursor(t, f.Page.After) {
			cp := *t
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return slaLess(out[i], out[j]) })
	if len(out) > f.Page.Limit+1 {
		out = out[:f.Page.Limit+1]
	}
	return out, nil
}

func (r *listRepo) SLALabels(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]SLALabel, error) {
	r.labelCalls++
	r.labelIDs = append(r.labelIDs, ids)
	out := map[uuid.UUID]SLALabel{}
	for _, id := range ids {
		if l, ok := r.labels[id]; ok {
			out[id] = l
		}
	}
	return out, nil
}

func (r *listRepo) ListComments(_ context.Context, _, _ uuid.UUID, q CommentQuery) ([]*TicketComment, error) {
	out := r.comments
	if len(out) > q.Page.Limit+1 {
		out = out[:q.Page.Limit+1]
	}
	return out, nil
}

func (r *listRepo) AuthorNames(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	r.nameCalls++
	r.nameIDs = append(r.nameIDs, ids)
	out := map[uuid.UUID]string{}
	for _, id := range ids {
		if n, ok := r.authorNames[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

// minutesIn is 09:00 UTC on 2026-11-17 plus n minutes.
func minutesIn(n int) *time.Time {
	t := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Minute)
	return &t
}

func TestSLASortKeyRanksRunningBeforePausedAndNoTargetLast(t *testing.T) {
	cases := []struct {
		name     string
		t        Ticket
		wantRank int
		wantKey  *time.Time
	}{
		{"running, response nearer", Ticket{SLAResponseDueAt: minutesIn(10), SLAResolutionDueAt: minutesIn(60)}, 0, minutesIn(10)},
		{"running, resolution nearer", Ticket{SLAResponseDueAt: minutesIn(90), SLAResolutionDueAt: minutesIn(60)}, 0, minutesIn(60)},
		{"response met counts only resolution", Ticket{FirstResponseAt: minutesIn(1), SLAResponseDueAt: minutesIn(10), SLAResolutionDueAt: minutesIn(60)}, 0, minutesIn(60)},
		{"response met, no resolution target", Ticket{FirstResponseAt: minutesIn(1), SLAResponseDueAt: minutesIn(10)}, 1, nil},
		{"running, no policy", Ticket{}, 1, nil},
		{"paused with a target", Ticket{SLAPausedAt: minutesIn(0), SLAResolutionDueAt: minutesIn(5)}, 2, minutesIn(5)},
		{"paused, no target", Ticket{SLAPausedAt: minutesIn(0)}, 3, nil},
	}
	for _, c := range cases {
		rank, key := slaSortKey(&c.t)
		want := slaEpoch
		if c.wantKey != nil {
			want = *c.wantKey
		}
		if rank != c.wantRank || !key.Equal(want) {
			t.Errorf("%s: got (%d, %v), want (%d, %v)", c.name, rank, key, c.wantRank, want)
		}
	}
}

func TestRankedCursorRoundTripAndRefusals(t *testing.T) {
	id := uuid.New()
	key := time.Date(2026, 11, 17, 9, 30, 0, 123000, time.UTC)
	c, err := ParseCursor(RankedCursorOf(2, key, id))
	if err != nil || !c.Ranked || c.Rank != 2 || c.ID != id || !c.CreatedAt.Equal(key) {
		t.Fatalf("round trip: %+v %v", c, err)
	}
	plain, err := ParseCursor(CursorOf(key, id))
	if err != nil || plain.Ranked {
		t.Fatalf("a created cursor is not ranked: %+v %v", plain, err)
	}
	for _, bad := range []string{"9." + CursorOf(key, id), "x." + CursorOf(key, id), "-1." + CursorOf(key, id), "1.notbase64!"} {
		if _, err := ParseCursor(bad); err != ErrInvalidCursor {
			t.Errorf("%q: got %v, want ErrInvalidCursor", bad, err)
		}
	}
	svc := NewService(&listRepo{}, zap.NewNop())
	if _, err := svc.ListTickets(context.Background(), uuid.Nil, TicketFilter{Sort: SortSLADue, Page: PageParams{After: plain, Limit: 5}}); err != ErrInvalidCursor {
		t.Errorf("a created cursor under sla_due: %v", err)
	}
	if _, err := svc.ListTickets(context.Background(), uuid.Nil, TicketFilter{Sort: SortCreatedDesc, Page: PageParams{After: c, Limit: 5}}); err != ErrInvalidCursor {
		t.Errorf("an sla cursor under created_desc: %v", err)
	}
}

// Walking sort=sla_due a page at a time returns every ticket once in SLA order (running by due,
// running without a target, paused by due, paused without a target), and each page looks up the
// SLA names once with the page's distinct policy ids.
func TestSLADueKeysetWalkAndOneLabelLookupPerPage(t *testing.T) {
	policy, other := uuid.New(), uuid.New()
	repo := &listRepo{labels: map[uuid.UUID]SLALabel{
		policy: {PolicyName: "Standard", CalendarName: "Nairobi office"},
		other:  {PolicyName: "Urgent"},
	}}
	add := func(name string, tk Ticket, policyID *uuid.UUID) *Ticket {
		tk.ID, tk.Subject, tk.SLAPolicyID = uuid.New(), name, policyID
		repo.tickets = append(repo.tickets, &tk)
		return &tk
	}
	pausedNone := add("paused none", Ticket{SLAPausedAt: minutesIn(0)}, nil)
	pausedSoon := add("paused 5", Ticket{SLAPausedAt: minutesIn(0), SLAResolutionDueAt: minutesIn(5)}, &policy)
	none := add("none", Ticket{}, nil)
	late := add("running 120", Ticket{SLAResolutionDueAt: minutesIn(120)}, &policy)
	soon := add("running 10", Ticket{SLAResponseDueAt: minutesIn(10), SLAResolutionDueAt: minutesIn(100)}, &policy)
	tieA := add("running 30 a", Ticket{SLAResolutionDueAt: minutesIn(30)}, &policy)
	tieB := add("running 30 b", Ticket{SLAResolutionDueAt: minutesIn(30)}, &other)
	met := add("met, 50", Ticket{FirstResponseAt: minutesIn(1), SLAResponseDueAt: minutesIn(2), SLAResolutionDueAt: minutesIn(50)}, &policy)

	ties := []*Ticket{tieA, tieB}
	if tieB.ID.String() < tieA.ID.String() {
		ties = []*Ticket{tieB, tieA}
	}
	want := []*Ticket{soon, ties[0], ties[1], met, late, none, pausedSoon, pausedNone}

	svc := NewService(repo, zap.NewNop())
	var got []*Ticket
	var after *Cursor
	pages := 0
	for {
		pages++
		if pages > 10 {
			t.Fatal("the walk does not end")
		}
		page, err := svc.ListTickets(context.Background(), uuid.Nil, TicketFilter{Sort: SortSLADue, Page: PageParams{After: after, Limit: 3}})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, page.Data...)
		if !page.HasMore {
			break
		}
		if after, err = ParseCursor(page.NextCursor); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("walked %d tickets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID {
			t.Fatalf("position %d: got %q, want %q", i, got[i].Subject, want[i].Subject)
		}
	}
	if pages != 3 || repo.labelCalls != 3 {
		t.Fatalf("8 tickets at 3 a page: %d pages and %d label lookups, want 3 and 3", pages, repo.labelCalls)
	}
	// The first page holds two tickets on policy and one on other: two distinct ids, one lookup.
	if n := len(repo.labelIDs[0]); n != 2 {
		t.Fatalf("first page looked up %d policy ids, want the 2 distinct ones", n)
	}
	for _, tk := range got {
		switch tk.ID {
		case soon.ID:
			if tk.SLAPolicyName != "Standard" || tk.SLACalendarName != "Nairobi office" {
				t.Fatalf("names not set: %+v", tk)
			}
		case tieB.ID:
			if tk.SLAPolicyName != "Urgent" || tk.SLACalendarName != "" {
				t.Fatalf("a 24x7 policy has no calendar name: %+v", tk)
			}
		case none.ID:
			if tk.SLAPolicyName != "" {
				t.Fatal("a ticket without a policy has no policy name")
			}
		}
	}
}

// A page whose tickets have no policy makes no label lookup at all.
func TestNoLabelLookupWithoutPolicies(t *testing.T) {
	repo := &listRepo{tickets: []*Ticket{{ID: uuid.New()}, {ID: uuid.New()}}}
	if _, err := NewService(repo, zap.NewNop()).ListTickets(context.Background(), uuid.Nil,
		TicketFilter{Sort: SortSLADue, Page: PageParams{Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	if repo.labelCalls != 0 {
		t.Fatalf("%d label lookups, want 0", repo.labelCalls)
	}
}
