//go:build integration

package tickets

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/sla"
	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

func itEqualTime(t *testing.T, label string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil {
		t.Errorf("%s is null, want %s", label, want.Format(time.RFC3339))
		return
	}
	if !got.Equal(want) {
		t.Errorf("%s = %s, want %s", label, got.In(want.Location()).Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// Deadlines count in the policy's calendar as stored in Postgres (weekly hours and holidays read
// back from jsonb), and a pending_customer status stops the clock and moves both deadlines by the
// business minutes it lasted.
//
// The calendar is Monday to Friday, 08:00 to 17:00 in Africa/Nairobi (UTC+3), with Monday
// 14 December 2026 a holiday. The ticket opens on Friday 11 December at 16:00:
//
//	first response 120 min: Friday 16:00 to 17:00 (60), Monday is a holiday, Tuesday 08:00 to
//	09:00 (60) -> Tuesday 15 December 09:00
//	resolution 660 min: Friday 60, Tuesday 540, Wednesday 60 -> Wednesday 16 December 09:00
//
// It is pending_customer from Friday 16:30 to Tuesday 10:00, which is 30 + 0 + 120 = 150 business
// minutes, so the deadlines move to Tuesday 11:30 and Wednesday 11:30.
func TestIntegrationSLACalendarDeadlinesAndPause(t *testing.T) {
	env := newITEnv(t)
	ctx := context.Background()
	tenant := uuid.New()
	nairobi, err := time.LoadLocation("Africa/Nairobi")
	if err != nil {
		t.Fatal(err)
	}
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 12, day, hour, minute, 0, 0, nairobi) }

	slaRepo := sla.NewEntRepository(env.db.Ent)
	slaSvc := sla.NewService(slaRepo, zap.NewNop())
	cal := &sla.Calendar{ID: uuid.New(), TenantID: tenant, Name: "Nairobi office", Timezone: "Africa/Nairobi",
		WeeklyHours: clock.DefaultWeek(), Holidays: []clock.Holiday{{Date: "2026-12-14", Name: "Test holiday"}}, IsDefault: true}
	if err := slaRepo.CreateCalendar(ctx, cal); err != nil {
		t.Fatal(err)
	}
	policy, err := slaSvc.CreatePolicy(ctx, &sla.CreatePolicyRequest{
		TenantID: tenant, Name: "Normal in business hours", Priority: sla.PriorityNormal, CalendarID: &cal.ID,
		FirstResponseMinutes: 120, ResolutionMinutes: 660,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A 24x7 policy for high priority: deadlines are plain minutes.
	if _, err := slaSvc.CreatePolicy(ctx, &sla.CreatePolicyRequest{
		TenantID: tenant, Name: "High around the clock", Priority: sla.PriorityHigh,
		FirstResponseMinutes: 30, ResolutionMinutes: 240,
	}); err != nil {
		t.Fatal(err)
	}
	env.svc.SetSLAService(&itSLA{svc: slaSvc})
	now := at(11, 16, 0)
	env.svc.now = func() time.Time { return now }

	created, err := env.svc.CreateTicket(ctx, &CreateTicketRequest{TenantID: tenant, CustomerID: uuid.New(),
		Subject: "Card machine offline", Description: "Since this afternoon"})
	if err != nil {
		t.Fatal(err)
	}
	tk := env.get(t, tenant, created.ID)
	if tk.SLAPolicyID == nil || *tk.SLAPolicyID != policy.ID {
		t.Fatalf("policy = %v, want %s", tk.SLAPolicyID, policy.ID)
	}
	itEqualTime(t, "response due", tk.SLAResponseDueAt, at(15, 9, 0))
	itEqualTime(t, "resolution due", tk.SLAResolutionDueAt, at(16, 9, 0))

	high, err := env.svc.CreateTicket(ctx, &CreateTicketRequest{TenantID: tenant, CustomerID: uuid.New(),
		Subject: "Site down", Description: "All tills", Priority: PriorityHigh})
	if err != nil {
		t.Fatal(err)
	}
	h := env.get(t, tenant, high.ID)
	itEqualTime(t, "24x7 response due", h.SLAResponseDueAt, now.Add(30*time.Minute))
	itEqualTime(t, "24x7 resolution due", h.SLAResolutionDueAt, now.Add(240*time.Minute))

	move := func(when time.Time, to TicketStatus) *Ticket {
		t.Helper()
		now = when
		if err := env.svc.UpdateTicketStatus(ctx, tenant, created.ID, to); err != nil {
			t.Fatalf("move to %s: %v", to, err)
		}
		return env.get(t, tenant, created.ID)
	}

	tk = move(at(11, 16, 10), StatusOpen)
	if tk.SLAPausedAt != nil {
		t.Fatalf("open ticket is paused at %s", tk.SLAPausedAt)
	}
	tk = move(at(11, 16, 30), StatusPendingCustomer)
	itEqualTime(t, "paused at", tk.SLAPausedAt, at(11, 16, 30))
	itEqualTime(t, "response due while paused", tk.SLAResponseDueAt, at(15, 9, 0))

	// A paused clock never breaches and is never warned about, however late it is.
	late := at(18, 12, 0)
	pending, err := env.repo.ListTicketsPendingBreach(ctx, late, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pending {
		if p.ID == created.ID {
			t.Error("the breach scan picked a paused ticket")
		}
	}
	warnings, err := env.repo.ListBreachWarnings(ctx, at(15, 8, 45), 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warnings {
		if w.TicketID == created.ID {
			t.Error("the warning scan picked a paused ticket")
		}
	}

	tk = move(at(15, 10, 0), StatusOpen)
	if tk.SLAPausedAt != nil {
		t.Errorf("still paused after resume: %s", tk.SLAPausedAt)
	}
	if tk.SLAPausedMinutes != 150 {
		t.Errorf("paused minutes = %d, want 150", tk.SLAPausedMinutes)
	}
	itEqualTime(t, "response due after resume", tk.SLAResponseDueAt, at(15, 11, 30))
	itEqualTime(t, "resolution due after resume", tk.SLAResolutionDueAt, at(16, 11, 30))

	// pending_internal stops the clock only when the policy says so; this one does not.
	tk = move(at(15, 10, 30), StatusPendingInternal)
	if tk.SLAPausedAt != nil {
		t.Errorf("pending_internal paused a policy without pause_on_pending_internal")
	}
	tk = move(at(15, 11, 0), StatusOpen)
	if tk.SLAPausedMinutes != 150 {
		t.Errorf("paused minutes = %d after pending_internal, want 150", tk.SLAPausedMinutes)
	}

	// Resolving records the elapsed resolution minutes net of the pause.
	tk = move(at(15, 12, 0), StatusResolved)
	if tk.SLAResolutionMinutes == nil {
		t.Fatal("resolution minutes not recorded")
	}
	if tk.SLAResolutionBreached {
		t.Error("resolved before the moved deadline but marked breached")
	}
}

// The breach scan returns every live, running, unanswered or unresolved ticket past a deadline in
// any tenant, and nothing else. Recording a breach flips its flag once, so the next scan and a
// second worker leave it alone.
func TestIntegrationSLABreachScan(t *testing.T) {
	env := newITEnv(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	env.svc.now = func() time.Time { return now }
	before, after := func(d time.Duration) *time.Time { return itPtr(now.Add(-d)) }, func(d time.Duration) *time.Time { return itPtr(now.Add(d)) }

	responseLate := env.insert(t, tenant, func(tk *Ticket) {
		tk.SLAResponseDueAt, tk.SLAResolutionDueAt = before(10*time.Minute), after(5*time.Hour)
	})
	resolutionLate := env.insert(t, tenant, func(tk *Ticket) {
		tk.SLAResponseDueAt, tk.SLAResolutionDueAt = before(2*time.Hour), before(time.Minute)
	})
	env.db.Exec(t, `UPDATE tickets SET first_response_at = $2 WHERE id = $1`, resolutionLate.ID, now.Add(-3*time.Hour))
	otherLate := env.insert(t, other, func(tk *Ticket) { tk.SLAResponseDueAt = before(time.Minute) })

	paused := env.insert(t, tenant, func(tk *Ticket) { tk.SLAResponseDueAt = before(time.Hour) })
	env.db.Exec(t, `UPDATE tickets SET sla_paused_at = $2 WHERE id = $1`, paused.ID, now.Add(-2*time.Hour))
	resolved := env.insert(t, tenant, func(tk *Ticket) { tk.Status, tk.SLAResponseDueAt = StatusResolved, before(time.Hour) })
	answered := env.insert(t, tenant, func(tk *Ticket) {
		tk.SLAResponseDueAt, tk.SLAResolutionDueAt = before(time.Hour), after(time.Hour)
	})
	env.db.Exec(t, `UPDATE tickets SET first_response_at = $2 WHERE id = $1`, answered.ID, now.Add(-30*time.Minute))
	flagged := env.insert(t, tenant, func(tk *Ticket) { tk.SLAResponseDueAt = before(time.Hour) })
	env.db.Exec(t, `UPDATE tickets SET sla_response_breached = true WHERE id = $1`, flagged.ID)
	deleted := env.insert(t, tenant, func(tk *Ticket) { tk.SLAResponseDueAt = before(time.Hour) })
	if err := env.repo.DeleteTicket(ctx, tenant, deleted.ID); err != nil {
		t.Fatal(err)
	}
	notYet := env.insert(t, tenant, func(tk *Ticket) { tk.SLAResponseDueAt = after(10 * time.Minute) })
	noSLA := env.insert(t, tenant, nil)

	mine := map[uuid.UUID]string{
		responseLate.ID: "response late", resolutionLate.ID: "resolution late", otherLate.ID: "other tenant late",
		paused.ID: "paused", resolved.ID: "resolved", answered.ID: "answered", flagged.ID: "already flagged",
		deleted.ID: "deleted", notYet.ID: "not yet due", noSLA.ID: "no SLA",
	}
	scan := func(at time.Time) map[uuid.UUID]*Ticket {
		t.Helper()
		rows, err := env.repo.ListTicketsPendingBreach(ctx, at, 1000)
		if err != nil {
			t.Fatal(err)
		}
		out := map[uuid.UUID]*Ticket{}
		for _, r := range rows {
			if _, ok := mine[r.ID]; ok {
				out[r.ID] = r
			}
		}
		return out
	}
	expect := func(label string, got map[uuid.UUID]*Ticket, want ...*Ticket) {
		t.Helper()
		wantSet := map[uuid.UUID]bool{}
		for _, w := range want {
			wantSet[w.ID] = true
			if got[w.ID] == nil {
				t.Errorf("%s: %s not selected", label, mine[w.ID])
			}
		}
		for id := range got {
			if !wantSet[id] {
				t.Errorf("%s: %s selected", label, mine[id])
			}
		}
	}

	first := scan(now)
	expect("first scan", first, responseLate, resolutionLate, otherLate)
	for _, tk := range first {
		changed, err := env.svc.RecordSLABreach(ctx, tk, now)
		if err != nil || !changed {
			t.Errorf("record %s: changed=%v err=%v", mine[tk.ID], changed, err)
		}
		// A second worker holding the same snapshot changes nothing.
		if changed, err := env.svc.RecordSLABreach(ctx, tk, now); err != nil || changed {
			t.Errorf("second record of %s: changed=%v err=%v", mine[tk.ID], changed, err)
		}
	}
	if n := env.db.Int64(t, `SELECT count(*) FROM outbox_events WHERE tenant_id IN ($1, $2)`, tenant, other); n != 3 {
		t.Errorf("%d breach events, want 3 (one per breach)", n)
	}
	expect("second scan", scan(now))
	if tk := env.get(t, tenant, responseLate.ID); !tk.SLAResponseBreached || tk.SLAResolutionBreached {
		t.Errorf("response late: flags %v/%v, want response only", tk.SLAResponseBreached, tk.SLAResolutionBreached)
	}

	// Six hours on, the resolution deadlines of the response-late and answered tickets have
	// passed and the not-yet-due response is late; each of those targets is picked up once.
	later := now.Add(6 * time.Hour)
	third := scan(later)
	expect("scan after the resolution deadline", third, responseLate, answered, notYet)
	if tk := third[responseLate.ID]; tk != nil {
		if changed, err := env.svc.RecordSLABreach(ctx, tk, later); err != nil || !changed {
			t.Errorf("record resolution breach: changed=%v err=%v", changed, err)
		}
		if after := env.get(t, tenant, responseLate.ID); !after.SLAResolutionBreached {
			t.Error("resolution breach not recorded")
		}
	}
}

// The pre-breach scan returns each target due within its policy's notify_before_breach_min that
// is still running and not yet warned, and a recorded warning is never repeated.
func TestIntegrationSLABreachWarningScan(t *testing.T) {
	env := newITEnv(t)
	ctx := context.Background()
	tenant := uuid.New()
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	in := func(d time.Duration) *time.Time { return itPtr(now.Add(d)) }

	slaRepo := sla.NewEntRepository(env.db.Ent)
	policy := &sla.Policy{ID: uuid.New(), TenantID: tenant, Name: "Warn 30 minutes ahead", Priority: sla.PriorityNormal,
		FirstResponseMinutes: 60, ResolutionMinutes: 480, IsActive: true,
		Metadata: map[string]any{clock.MetaNotifyBeforeBreachMin: 30}}
	if err := slaRepo.Create(ctx, policy); err != nil {
		t.Fatal(err)
	}
	withPolicy := func(mut func(*Ticket)) func(*Ticket) {
		return func(tk *Ticket) {
			tk.SLAPolicyID = &policy.ID
			mut(tk)
		}
	}
	resolutionSoon := env.insert(t, tenant, withPolicy(func(tk *Ticket) { tk.SLAResolutionDueAt = in(20 * time.Minute) }))
	responseSoon := env.insert(t, tenant, withPolicy(func(tk *Ticket) {
		tk.SLAResponseDueAt, tk.SLAResolutionDueAt = in(10*time.Minute), in(5*time.Hour)
	}))
	answered := env.insert(t, tenant, withPolicy(func(tk *Ticket) { tk.SLAResponseDueAt = in(10 * time.Minute) }))
	env.db.Exec(t, `UPDATE tickets SET first_response_at = $2 WHERE id = $1`, answered.ID, now.Add(-time.Minute))
	tooFar := env.insert(t, tenant, withPolicy(func(tk *Ticket) { tk.SLAResolutionDueAt = in(40 * time.Minute) }))
	paused := env.insert(t, tenant, withPolicy(func(tk *Ticket) { tk.SLAResolutionDueAt = in(10 * time.Minute) }))
	env.db.Exec(t, `UPDATE tickets SET sla_paused_at = $2 WHERE id = $1`, paused.ID, now.Add(-time.Hour))
	warned := env.insert(t, tenant, withPolicy(func(tk *Ticket) {
		tk.SLAResolutionDueAt = in(10 * time.Minute)
		tk.Metadata = map[string]any{MetaSLAWarnings: map[string]any{"resolution": now.Add(-time.Minute).Format(time.RFC3339)}}
	}))
	overdue := env.insert(t, tenant, withPolicy(func(tk *Ticket) { tk.SLAResolutionDueAt = in(-5 * time.Minute) }))
	noPolicy := env.insert(t, tenant, func(tk *Ticket) { tk.SLAResolutionDueAt = in(10 * time.Minute) })

	mine := map[uuid.UUID]string{resolutionSoon.ID: "resolution soon", responseSoon.ID: "response soon",
		answered.ID: "answered", tooFar.ID: "too far", paused.ID: "paused", warned.ID: "already warned",
		overdue.ID: "overdue", noPolicy.ID: "no policy"}
	scan := func() []BreachWarning {
		t.Helper()
		rows, err := env.repo.ListBreachWarnings(ctx, now, 1000)
		if err != nil {
			t.Fatal(err)
		}
		var out []BreachWarning
		for _, w := range rows {
			if _, ok := mine[w.TicketID]; ok {
				out = append(out, w)
			}
		}
		return out
	}

	got := scan()
	want := map[[2]string]bool{
		{resolutionSoon.ID.String(), clock.TargetResolution}:  true,
		{responseSoon.ID.String(), clock.TargetFirstResponse}: true,
	}
	if len(got) != len(want) {
		t.Errorf("%d warnings, want %d: %+v", len(got), len(want), got)
	}
	for _, w := range got {
		if !want[[2]string{w.TicketID.String(), w.Target}] {
			t.Errorf("unexpected warning: %s %s", mine[w.TicketID], w.Target)
		}
	}
	for _, w := range got {
		if changed, err := env.svc.RecordBreachWarning(ctx, w, now); err != nil || !changed {
			t.Errorf("record %s %s: changed=%v err=%v", mine[w.TicketID], w.Target, changed, err)
		}
		if changed, err := env.svc.RecordBreachWarning(ctx, w, now); err != nil || changed {
			t.Errorf("second record of %s %s: changed=%v err=%v", mine[w.TicketID], w.Target, changed, err)
		}
	}
	if again := scan(); len(again) != 0 {
		t.Errorf("warned targets listed again: %+v", again)
	}
}
