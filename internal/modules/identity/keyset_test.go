package identity

import (
	"context"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// listRepo keeps one tenant's agents and teams and answers the list queries the way the SQL
// does: newest first by (created_at, id), rows strictly before the cursor, Limit+1 rows.
type listRepo struct {
	Repository
	agents []*Agent
	teams  []*Team
	calls  int
}

// before reports whether (at, id) sorts after the cursor in the descending order.
func before(at time.Time, id uuid.UUID, c *Cursor) bool {
	if c == nil {
		return true
	}
	if !at.Equal(c.CreatedAt) {
		return at.Before(c.CreatedAt)
	}
	return id.String() < c.ID.String()
}

func newestFirst(ai, aj time.Time, ii, ij uuid.UUID) bool {
	if !ai.Equal(aj) {
		return ai.After(aj)
	}
	return ii.String() > ij.String()
}

func (r *listRepo) ListAgents(_ context.Context, _ uuid.UUID, f AgentFilter) ([]*Agent, error) {
	r.calls++
	var out []*Agent
	for _, a := range r.agents {
		if before(a.CreatedAt, a.ID, f.After) && (f.IsActive == nil || a.IsActive == *f.IsActive) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return newestFirst(out[i].CreatedAt, out[j].CreatedAt, out[i].ID, out[j].ID) })
	if len(out) > f.Limit+1 {
		out = out[:f.Limit+1]
	}
	return out, nil
}

func (r *listRepo) ListTeams(_ context.Context, _ uuid.UUID, f TeamFilter) ([]*Team, error) {
	r.calls++
	var out []*Team
	for _, t := range r.teams {
		if before(t.CreatedAt, t.ID, f.After) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return newestFirst(out[i].CreatedAt, out[j].CreatedAt, out[i].ID, out[j].ID) })
	if len(out) > f.Limit+1 {
		out = out[:f.Limit+1]
	}
	return out, nil
}

// Walking the agent list page by page returns every agent once, newest first, including agents
// that share a created_at (the id breaks the tie).
func TestAgentKeysetWalksEveryAgentOnce(t *testing.T) {
	base := time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)
	repo := &listRepo{}
	for i := 0; i < 7; i++ {
		// Pairs share a timestamp.
		repo.agents = append(repo.agents, &Agent{ID: uuid.New(), DisplayName: "A", IsActive: true,
			CreatedAt: base.Add(time.Duration(i/2) * time.Minute)})
	}
	svc := NewService(repo, zap.NewNop())

	var got []*Agent
	var after *Cursor
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("the walk does not end")
		}
		page, err := svc.ListAgents(context.Background(), uuid.Nil, AgentFilter{After: after, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, page.Data...)
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Fatal("the last page carries no cursor")
			}
			break
		}
		if after, err = ParseCursor(page.NextCursor); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 7 {
		t.Fatalf("walked %d agents, want 7", len(got))
	}
	seen := map[uuid.UUID]bool{}
	for i, a := range got {
		if seen[a.ID] {
			t.Fatalf("agent %s returned twice", a.ID)
		}
		seen[a.ID] = true
		if i > 0 && !newestFirst(got[i-1].CreatedAt, a.CreatedAt, got[i-1].ID, a.ID) {
			t.Fatalf("row %d is out of order", i)
		}
	}
	if repo.calls != 3 {
		t.Fatalf("7 agents at 3 a page take 3 queries, took %d", repo.calls)
	}
}

func TestTeamPageSetsNextCursorFromLastRowKept(t *testing.T) {
	base := time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)
	repo := &listRepo{}
	for i := 0; i < 3; i++ {
		repo.teams = append(repo.teams, &Team{ID: uuid.New(), Name: "T", CreatedAt: base.Add(time.Duration(i) * time.Hour)})
	}
	page, err := NewService(repo, zap.NewNop()).ListTeams(context.Background(), uuid.Nil, TeamFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || len(page.Data) != 2 {
		t.Fatalf("want a full page with more: %+v", page)
	}
	c, err := ParseCursor(page.NextCursor)
	if err != nil || c.ID != page.Data[1].ID || !c.CreatedAt.Equal(page.Data[1].CreatedAt) {
		t.Fatalf("cursor must point at the last row kept: %v %v", c, err)
	}
	if page.Data[0].ID != repo.teams[2].ID {
		t.Fatal("the newest team comes first")
	}
	empty, _ := NewService(&listRepo{}, zap.NewNop()).ListTeams(context.Background(), uuid.Nil, TeamFilter{})
	if empty.Data == nil || empty.HasMore {
		t.Fatalf("an empty list encodes data as [] with no more: %+v", empty)
	}
}

func TestParsePageBoundsAndCursor(t *testing.T) {
	cases := map[string]int{"": 25, "limit=0": 25, "limit=-1": 25, "limit=40": 40, "limit=500": 100, "limit=x": 25}
	for q, want := range cases {
		after, limit, err := parsePage(httptest.NewRequest("GET", "/agents?"+q, nil))
		if err != nil || after != nil || limit != want {
			t.Errorf("%q: limit %d after %v err %v, want %d", q, limit, after, err, want)
		}
	}
	if _, _, err := parsePage(httptest.NewRequest("GET", "/agents?cursor=nope!", nil)); err != ErrInvalidCursor {
		t.Errorf("bad cursor: %v", err)
	}
}
