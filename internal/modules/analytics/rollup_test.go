package analytics

import (
	"testing"
	"time"

	"github.com/google/uuid"
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
