package sla

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

// memRepo keeps one tenant's policies and calendars in memory; other methods panic (nil embed).
type memRepo struct {
	Repository
	policies  map[uuid.UUID]*Policy
	calendars map[uuid.UUID]*Calendar
	teams     map[uuid.UUID]bool
}

func newMemRepo() *memRepo {
	return &memRepo{policies: map[uuid.UUID]*Policy{}, calendars: map[uuid.UUID]*Calendar{}, teams: map[uuid.UUID]bool{}}
}

func (m *memRepo) Create(_ context.Context, p *Policy) error {
	m.policies[p.ID] = p
	return nil
}

func (m *memRepo) Get(_ context.Context, tenantID, id uuid.UUID) (*Policy, error) {
	if p, ok := m.policies[id]; ok && p.TenantID == tenantID {
		return p, nil
	}
	return nil, ErrPolicyNotFound
}

func (m *memRepo) FindMatchingPolicy(_ context.Context, tenantID uuid.UUID, priority Priority, _ *uuid.UUID) (*Policy, error) {
	for _, p := range m.policies {
		if p.TenantID == tenantID && p.Priority == priority {
			return p, nil
		}
	}
	return nil, errors.New("no matching sla policy")
}

func (m *memRepo) TeamExists(_ context.Context, _ uuid.UUID, id uuid.UUID) (bool, error) {
	return m.teams[id], nil
}

func (m *memRepo) CreateCalendar(_ context.Context, c *Calendar) error {
	m.calendars[c.ID] = c
	return nil
}

func (m *memRepo) GetCalendar(_ context.Context, tenantID, id uuid.UUID) (*Calendar, error) {
	if c, ok := m.calendars[id]; ok && c.TenantID == tenantID {
		cp := *c
		return &cp, nil
	}
	return nil, ErrCalendarNotFound
}

func (m *memRepo) CountCalendars(_ context.Context, tenantID uuid.UUID) (int, error) {
	n := 0
	for _, c := range m.calendars {
		if c.TenantID == tenantID {
			n++
		}
	}
	return n, nil
}

func (m *memRepo) SaveCalendar(_ context.Context, c *Calendar) error {
	m.calendars[c.ID] = c
	return nil
}

func (m *memRepo) ClearDefaultCalendar(_ context.Context, tenantID, keep uuid.UUID) error {
	for id, c := range m.calendars {
		if c.TenantID == tenantID && id != keep {
			c.IsDefault = false
		}
	}
	return nil
}

func (m *memRepo) CalendarInUse(_ context.Context, tenantID, id uuid.UUID) (bool, error) {
	for _, p := range m.policies {
		if p.TenantID == tenantID && p.CalendarID != nil && *p.CalendarID == id {
			return true, nil
		}
	}
	return false, nil
}

func newTestService(now time.Time) (*Service, *memRepo) {
	repo := newMemRepo()
	s := NewService(repo, zap.NewNop())
	s.now = func() time.Time { return now }
	return s, repo
}

func strp(s string) *string { return &s }

func TestFirstCalendarSeedsKenyanHolidays(t *testing.T) {
	s, repo := newTestService(time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC))
	tenant := uuid.New()
	own := []clock.Holiday{{Date: "2026-03-21", Name: "Idd ul-Fitr (confirmed)"}, {Date: "2026-12-31", Name: "Year-end closure"}}
	c, err := s.CreateCalendar(context.Background(), tenant, CalendarInput{Name: strp("Main"), Holidays: &own})
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsDefault || c.Timezone != "Africa/Nairobi" || len(c.WeeklyHours["mon"]) != 1 || len(c.WeeklyHours["sat"]) != 0 {
		t.Fatalf("first calendar = %+v", c)
	}
	dates := map[string]string{}
	for _, h := range c.Holidays {
		dates[h.Date] = h.Name
	}
	for _, d := range []string{"2026-12-25", "2026-12-31", "2026-03-21", "2027-04-01", "2027-03-29", "2027-12-27"} {
		if d == "2027-04-01" {
			if _, ok := dates[d]; ok {
				t.Errorf("%s is no holiday", d)
			}
			continue
		}
		if _, ok := dates[d]; !ok {
			t.Errorf("missing holiday %s", d)
		}
	}

	second, err := s.CreateCalendar(context.Background(), tenant, CalendarInput{Name: strp("Weekend desk"), IsDefault: boolp(true),
		WeeklyHours: clock.Week{"sat": {{Start: "09:00", End: "13:00"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Holidays) != 0 || !second.IsDefault || repo.calendars[c.ID].IsDefault {
		t.Errorf("only the first calendar is seeded, and a new default unsets the old one: %+v", second)
	}
}

func boolp(b bool) *bool { return &b }

func TestCalendarValidationAndDelete(t *testing.T) {
	s, repo := newTestService(time.Now())
	tenant := uuid.New()
	if _, err := s.CreateCalendar(context.Background(), tenant, CalendarInput{Name: strp("x"), Timezone: strp("Nowhere/City")}); !errors.Is(err, ErrInvalidCalendar) {
		t.Errorf("bad timezone: %v", err)
	}
	if _, err := s.CreateCalendar(context.Background(), tenant, CalendarInput{}); !errors.Is(err, ErrInvalidCalendar) {
		t.Errorf("missing name: %v", err)
	}
	c, err := s.CreateCalendar(context.Background(), tenant, CalendarInput{Name: strp("Main")})
	if err != nil {
		t.Fatal(err)
	}
	repo.policies[uuid.New()] = &Policy{TenantID: tenant, CalendarID: &c.ID}
	if err := s.DeleteCalendar(context.Background(), tenant, c.ID); !errors.Is(err, ErrCalendarInUse) {
		t.Errorf("delete in use: %v", err)
	}
}

// The sprint 10 acceptance case through the policy: a high ticket opened Friday 17:00 with a
// 2-hour target in a weekday 08:00 to 17:00 calendar is due Monday 10:00.
func TestComputeDeadlinesUsesCalendar(t *testing.T) {
	eat, _ := time.LoadLocation("Africa/Nairobi")
	s, _ := newTestService(time.Date(2026, 11, 13, 12, 0, 0, 0, eat))
	tenant := uuid.New()
	cal, err := s.CreateCalendar(context.Background(), tenant, CalendarInput{Name: strp("Main")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePolicy(context.Background(), &CreatePolicyRequest{
		TenantID: tenant, Name: "High", Priority: PriorityHigh, CalendarID: &cal.ID,
		FirstResponseMinutes: 120, ResolutionMinutes: 9 * 60,
	}); err != nil {
		t.Fatal(err)
	}
	res, err := s.ComputeDeadlines(context.Background(), tenant, PriorityHigh, nil, time.Date(2026, 11, 13, 17, 0, 0, 0, eat))
	if err != nil || res == nil {
		t.Fatalf("deadlines: %v %v", res, err)
	}
	if want := time.Date(2026, 11, 16, 10, 0, 0, 0, eat); !res.ResponseDueAt.Equal(want) {
		t.Errorf("response due = %s, want %s", res.ResponseDueAt, want)
	}
	if want := time.Date(2026, 11, 16, 17, 0, 0, 0, eat); !res.ResolutionDueAt.Equal(want) {
		t.Errorf("resolution due = %s, want %s", res.ResolutionDueAt, want)
	}
	if none, _ := s.ComputeDeadlines(context.Background(), tenant, PriorityLow, nil, time.Now()); none != nil {
		t.Error("no matching policy means no SLA")
	}
}

func TestPolicyRefsAreChecked(t *testing.T) {
	s, repo := newTestService(time.Now())
	tenant := uuid.New()
	base := CreatePolicyRequest{TenantID: tenant, Name: "P", Priority: PriorityNormal, FirstResponseMinutes: 60, ResolutionMinutes: 240}

	other := uuid.New()
	req := base
	req.CalendarID = &other
	if _, err := s.CreatePolicy(context.Background(), &req); !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("foreign calendar: %v", err)
	}
	req = base
	req.Metadata = map[string]any{"escalation_rules": []any{map[string]any{"target": "resolution", "when": "after_breach", "team_id": other.String()}}}
	if _, err := s.CreatePolicy(context.Background(), &req); !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("foreign team: %v", err)
	}
	repo.teams[other] = true
	p, err := s.CreatePolicy(context.Background(), &req)
	if err != nil {
		t.Fatalf("valid rules refused: %v", err)
	}
	clk, err := s.PolicyClock(context.Background(), tenant, &p.ID)
	if err != nil || clk.Calendar != nil || clk.PauseOnPendingInternal {
		t.Errorf("a policy without a calendar counts 24x7: %+v %v", clk, err)
	}
	missing := uuid.New()
	if clk, err := s.PolicyClock(context.Background(), tenant, &missing); err != nil || clk.Calendar != nil {
		t.Errorf("a deleted policy counts 24x7: %v", err)
	}
}
