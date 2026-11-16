package tickets

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

func nairobi(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Africa/Nairobi")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func officeClock(t *testing.T) SLAClock {
	t.Helper()
	cal, err := clock.New("Africa/Nairobi", clock.DefaultWeek(), clock.KenyaHolidays(2026))
	if err != nil {
		t.Fatal(err)
	}
	return SLAClock{Calendar: cal}
}

func ptime(t time.Time) *time.Time { return &t }

// slaTicket was created Monday 16 November 2026 09:00 EAT, response due 11:00, resolution due 17:00.
func slaTicket(t *testing.T) *Ticket {
	eat := nairobi(t)
	return &Ticket{
		ID: uuid.New(), TenantID: uuid.New(), Status: StatusOpen, Priority: PriorityHigh,
		CreatedAt:          time.Date(2026, 11, 16, 9, 0, 0, 0, eat),
		SLAResponseDueAt:   ptime(time.Date(2026, 11, 16, 11, 0, 0, 0, eat)),
		SLAResolutionDueAt: ptime(time.Date(2026, 11, 16, 17, 0, 0, 0, eat)),
	}
}

func TestPauseAndResumeMoveDeadlines(t *testing.T) {
	eat := nairobi(t)
	c := officeClock(t)
	tk := slaTicket(t)

	pausedAt := time.Date(2026, 11, 16, 10, 0, 0, 0, eat)
	u := pauseTransition(tk, StatusPendingCustomer, c, pausedAt)
	if u.PausedAt == nil || !u.PausedAt.Equal(pausedAt) || tk.SLAPausedAt == nil || u.ResponseDueAt != nil {
		t.Fatalf("pause: %+v", u)
	}

	// Three hours in pending_customer give three more business hours on each open deadline.
	resumed := time.Date(2026, 11, 16, 13, 0, 0, 0, eat)
	u = pauseTransition(tk, StatusOpen, c, resumed)
	if !u.Resume || tk.SLAPausedAt != nil || u.PausedMinutes == nil || *u.PausedMinutes != 180 {
		t.Fatalf("resume: %+v", u)
	}
	if want := time.Date(2026, 11, 16, 14, 0, 0, 0, eat); !tk.SLAResponseDueAt.Equal(want) || !u.ResponseDueAt.Equal(want) {
		t.Errorf("response due = %s, want %s", tk.SLAResponseDueAt, want)
	}
	// 17:00 plus three business hours runs into Tuesday morning.
	if want := time.Date(2026, 11, 17, 11, 0, 0, 0, eat); !tk.SLAResolutionDueAt.Equal(want) {
		t.Errorf("resolution due = %s, want %s", tk.SLAResolutionDueAt, want)
	}
	if got := netMinutes(c, tk, time.Date(2026, 11, 16, 15, 0, 0, 0, eat)); got != 180 {
		t.Errorf("net minutes = %d, want 180 (6 hours less the 3-hour pause)", got)
	}
}

func TestPauseOverWeekendCountsBusinessMinutes(t *testing.T) {
	eat := nairobi(t)
	c := officeClock(t)
	tk := slaTicket(t)
	tk.SLAPausedAt = ptime(time.Date(2026, 11, 20, 16, 0, 0, 0, eat))
	u := pauseTransition(tk, StatusInProgress, c, time.Date(2026, 11, 23, 9, 0, 0, 0, eat))
	if u.PausedMinutes == nil || *u.PausedMinutes != 120 {
		t.Fatalf("paused minutes = %v, want 120", u.PausedMinutes)
	}
}

func TestPendingInternalFollowsPolicy(t *testing.T) {
	eat := nairobi(t)
	at := time.Date(2026, 11, 16, 10, 0, 0, 0, eat)
	plain := officeClock(t)
	if u := pauseTransition(slaTicket(t), StatusPendingInternal, plain, at); !u.empty() {
		t.Errorf("pending_internal stopped a clock whose policy does not pause it: %+v", u)
	}
	internal := plain
	internal.PauseOnPendingInternal = true
	if u := pauseTransition(slaTicket(t), StatusPendingInternal, internal, at); u.PausedAt == nil {
		t.Error("pending_internal should stop the clock when the policy says so")
	}
	// Moving from pending_customer to a pending_internal that does not pause restarts the clock.
	tk := slaTicket(t)
	tk.SLAPausedAt = ptime(at)
	if u := pauseTransition(tk, StatusPendingInternal, plain, at.Add(time.Hour)); !u.Resume {
		t.Error("leaving pending_customer for a running status restarts the clock")
	}
}

func TestResolveFromPendingRestartsFirst(t *testing.T) {
	eat := nairobi(t)
	c := officeClock(t)
	tk := slaTicket(t)
	now := time.Now()
	tk.FirstResponseAt = &now
	tk.SLAPausedAt = ptime(time.Date(2026, 11, 16, 10, 0, 0, 0, eat))
	u := pauseTransition(tk, StatusResolved, c, time.Date(2026, 11, 16, 12, 0, 0, 0, eat))
	if !u.Resume || u.ResponseDueAt != nil {
		t.Fatalf("resolve: %+v (a met response deadline does not move)", u)
	}
	if want := time.Date(2026, 11, 17, 10, 0, 0, 0, eat); !tk.SLAResolutionDueAt.Equal(want) {
		t.Errorf("resolution due = %s, want %s", tk.SLAResolutionDueAt, want)
	}
	if u := pauseTransition(&Ticket{Status: StatusOpen}, StatusPendingCustomer, c, now); !u.empty() {
		t.Error("a ticket without an SLA never pauses")
	}
}

func TestEffectiveDue(t *testing.T) {
	eat := nairobi(t)
	c := officeClock(t)
	tk := slaTicket(t)
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, eat)
	if got := effectiveDue(c, tk, *tk.SLAResponseDueAt, at); !got.Equal(*tk.SLAResponseDueAt) {
		t.Errorf("running clock: %s", got)
	}
	tk.SLAPausedAt = ptime(time.Date(2026, 11, 16, 10, 30, 0, 0, eat))
	if got, want := effectiveDue(c, tk, *tk.SLAResponseDueAt, at), time.Date(2026, 11, 16, 12, 30, 0, 0, eat); !got.Equal(want) {
		t.Errorf("open pause: %s, want %s", got, want)
	}
	if got := netMinutes(c, tk, at); got != 90 {
		t.Errorf("net minutes during a pause = %d, want 90", got)
	}
}
