package analytics

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func at(h, m int) time.Time { return time.Date(2026, 11, 17, h, m, 0, 0, time.UTC) }

func TestTicketHoursCoverEveryMoment(t *testing.T) {
	resolved, first, due := at(14, 5), at(9, 59), at(12, 30)
	got := ticketHours(changedTicket{
		CreatedAt: at(9, 10), FirstResponseAt: &first, ResolvedAt: &resolved, ResolutionDueAt: &due,
	}, []time.Time{at(15, 45)})
	want := []time.Time{at(9, 0), at(12, 0), at(14, 0), at(15, 0)}
	if len(got) != len(want) {
		t.Fatalf("hours = %v, want %v", got, want)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("hours = %v, want %v", got, want)
		}
	}
}

func TestDeletedTicketCountsNowhere(t *testing.T) {
	if h := ticketHours(changedTicket{CreatedAt: at(9, 0), Deleted: true}, nil); len(h) != 0 {
		t.Fatalf("deleted ticket hours = %v", h)
	}
}

// A ticket resolved again after a reopen leaves its old resolution hour; the plan recomputes it
// from the mark so the old cell loses the ticket.
func TestPlanRecomputesOldAndNewHours(t *testing.T) {
	tenantA, tenantB := uuid.New(), uuid.New()
	moved := uuid.New()
	newResolved := at(16, 20)
	changed := []changedTicket{
		{ID: moved, TenantID: tenantA, CreatedAt: at(8, 0), ResolvedAt: &newResolved},
		{ID: uuid.New(), TenantID: tenantB, CreatedAt: at(10, 30), Deleted: true},
	}
	old := map[uuid.UUID][]time.Time{
		moved:         {at(8, 0), at(11, 0)},
		changed[1].ID: {at(10, 0)},
	}
	plan := planBatch(changed, nil, old)
	a := plan.Hours[tenantA]
	if len(a) != 3 || !a[0].Equal(at(8, 0)) || !a[1].Equal(at(11, 0)) || !a[2].Equal(at(16, 0)) {
		t.Fatalf("tenant A hours = %v", a)
	}
	if b := plan.Hours[tenantB]; len(b) != 1 || !b[0].Equal(at(10, 0)) {
		t.Fatalf("tenant B hours = %v (the deleted ticket's old hour)", b)
	}
	if len(plan.Marks) != 2 || len(plan.Marks[0].Hours) != 2 || len(plan.Marks[1].Hours) != 0 {
		t.Fatalf("marks = %+v", plan.Marks)
	}
}

// markAll is what a step leaves behind: each planned ticket's mark holds the updated_at it
// counted, which the next overlap re-read compares with the candidate's.
func markAll(cands []overlapCandidate, plan batchPlan) {
	seen := map[uuid.UUID]time.Time{}
	for _, m := range plan.Marks {
		seen[m.ID] = m.UpdatedAt
	}
	for i := range cands {
		if v, ok := seen[cands[i].ID]; ok {
			cands[i].MarkedAt = &v
		}
	}
}

func pickedTickets(ids []uuid.UUID, all map[uuid.UUID]changedTicket) []changedTicket {
	out := make([]changedTicket, len(ids))
	for i, id := range ids {
		out[i] = all[id]
	}
	return out
}

// A slow transaction commits B with an updated_at the watermark (A, at 10:00) has already
// passed, and C's change at 9:50 after a step counted C at 9:40. The forward read starts past the
// watermark and never sees them; the next run's overlap window does, and rebuilds both. Once
// their marks hold what was counted, the window has nothing left to rebuild.
func TestOverlapPicksChangeCommittedBehindWatermark(t *testing.T) {
	r := NewRollup(nil, zap.NewNop())
	if r.Overlap != 15*time.Minute || r.OverlapLimit != 2000 || r.OverlapScan < r.OverlapLimit {
		t.Fatalf("overlap defaults = %s, scan %d, limit %d", r.Overlap, r.OverlapScan, r.OverlapLimit)
	}
	tenant := uuid.New()
	a := changedTicket{ID: uuid.New(), TenantID: tenant, CreatedAt: at(10, 0), UpdatedAt: at(10, 0)}
	b := changedTicket{ID: uuid.New(), TenantID: tenant, CreatedAt: at(9, 55), UpdatedAt: at(9, 55)}
	resolved := at(9, 50)
	c := changedTicket{ID: uuid.New(), TenantID: tenant, CreatedAt: at(9, 0), ResolvedAt: &resolved, UpdatedAt: at(9, 50)}
	wmAt := a.UpdatedAt
	for _, late := range []changedTicket{b, c} {
		if !late.UpdatedAt.Before(wmAt) {
			t.Fatalf("ticket at %s is not behind the watermark %s", late.UpdatedAt, wmAt)
		}
		if late.UpdatedAt.Before(wmAt.Add(-r.Overlap)) {
			t.Fatalf("ticket at %s is outside the overlap window from %s", late.UpdatedAt, wmAt.Add(-r.Overlap))
		}
	}

	countedA, countedC := a.UpdatedAt, at(9, 40)
	cands := []overlapCandidate{ // as overlapCandidatesSQL returns them: oldest first
		{ID: c.ID, UpdatedAt: c.UpdatedAt, MarkedAt: &countedC},
		{ID: b.ID, UpdatedAt: b.UpdatedAt},
		{ID: a.ID, UpdatedAt: a.UpdatedAt, MarkedAt: &countedA},
	}
	picked := pickOverlap(cands, r.OverlapLimit)
	if len(picked) != 2 || picked[0] != c.ID || picked[1] != b.ID {
		t.Fatalf("picked %v, want C then B (A is counted at its updated_at)", picked)
	}

	all := map[uuid.UUID]changedTicket{a.ID: a, b.ID: b, c.ID: c}
	old := map[uuid.UUID][]time.Time{c.ID: {at(9, 0)}}
	plan := planBatch(pickedTickets(picked, all), nil, old)
	hours := plan.Hours[tenant]
	if len(hours) != 1 || !hours[0].Equal(at(9, 0)) {
		t.Fatalf("rebuilt hours = %v, want the 09:00 hour of B's creation and C's resolution", hours)
	}
	for _, m := range plan.Marks {
		if !m.UpdatedAt.Equal(all[m.ID].UpdatedAt) {
			t.Fatalf("mark of %s holds %s, want the ticket's updated_at %s", m.ID, m.UpdatedAt, all[m.ID].UpdatedAt)
		}
	}
	// saveMarksSQL reads the mark's updated_at from this key.
	raw, err := jsonParam(plan.Marks)
	if err != nil || !strings.Contains(raw, `"updated_at":"`) {
		t.Fatalf("marks JSON = %s (%v), want an updated_at per mark", raw, err)
	}

	markAll(cands, plan)
	if again := pickOverlap(cands, r.OverlapLimit); len(again) != 0 {
		t.Fatalf("second overlap picked %v, want nothing once the marks match", again)
	}
	// C changes again behind the watermark: its mark no longer matches, so it is picked again.
	cands[0].UpdatedAt = at(9, 58)
	if again := pickOverlap(cands, r.OverlapLimit); len(again) != 1 || again[0] != c.ID {
		t.Fatalf("after C's second late change picked %v, want C", again)
	}
}

// The overlap rebuilds at most OverlapLimit tickets per run, oldest first, and the next runs take
// the rest because the rebuilt ones then match their marks.
func TestOverlapIsCapped(t *testing.T) {
	tenant := uuid.New()
	all := map[uuid.UUID]changedTicket{}
	var cands []overlapCandidate
	for i := 0; i < 9; i++ {
		tk := changedTicket{ID: uuid.New(), TenantID: tenant, CreatedAt: at(9, i), UpdatedAt: at(9, i)}
		all[tk.ID] = tk
		c := overlapCandidate{ID: tk.ID, UpdatedAt: tk.UpdatedAt}
		if i%3 == 0 { // already counted at this version
			counted := tk.UpdatedAt
			c.MarkedAt = &counted
		}
		cands = append(cands, c)
	}
	const limit = 2
	var runs [][]uuid.UUID
	for run := 0; run < 10; run++ {
		picked := pickOverlap(cands, limit)
		if len(picked) == 0 {
			break
		}
		if len(picked) > limit {
			t.Fatalf("run %d picked %d tickets, cap %d", run, len(picked), limit)
		}
		runs = append(runs, picked)
		markAll(cands, planBatch(pickedTickets(picked, all), nil, nil))
	}
	// Six late tickets (1, 2, 4, 5, 7, 8) in runs of two, oldest first.
	want := []int{1, 2, 4, 5, 7, 8}
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(runs))
	}
	k := 0
	for _, picked := range runs {
		for _, id := range picked {
			if id != cands[want[k]].ID {
				t.Fatalf("pick %d = ticket %d, want ticket %d", k, indexOf(cands, id), want[k])
			}
			k++
		}
	}
	if got := pickOverlap(cands, 0); len(got) != 0 {
		t.Fatalf("a zero cap picked %v", got)
	}
}

func indexOf(cands []overlapCandidate, id uuid.UUID) int {
	for i, c := range cands {
		if c.ID == id {
			return i
		}
	}
	return -1
}

func TestDaysAndChunks(t *testing.T) {
	hours := []time.Time{at(0, 0), at(23, 0), at(23, 0).Add(2 * time.Hour)}
	d := daysOf(hours)
	if len(d) != 2 || !d[0].Equal(time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("days = %v", d)
	}
	if c := chunks(hours, 2); len(c) != 2 || len(c[0]) != 2 || len(c[1]) != 1 {
		t.Fatalf("chunks = %v", c)
	}
	if c := chunks(nil, 2); len(c) != 0 {
		t.Fatalf("empty chunks = %v", c)
	}
}
