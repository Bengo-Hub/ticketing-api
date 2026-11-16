package tickets

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

// slaRepo is an in-memory repository for the SLA paths; other methods panic (nil embed).
type slaRepo struct {
	Repository
	tickets  map[uuid.UUID]*Ticket
	teams    map[uuid.UUID]bool
	warned   map[string]bool
	comments []*TicketComment
	updates  []SLAUpdate
	events   []OutboxEvent
}

func newSLARepo() *slaRepo {
	return &slaRepo{tickets: map[uuid.UUID]*Ticket{}, teams: map[uuid.UUID]bool{}, warned: map[string]bool{}}
}

func (r *slaRepo) InTx(_ context.Context, _ uuid.UUID, fn func(Repository, *Outbox) error) error {
	out := &Outbox{}
	if err := fn(r, out); err != nil {
		return err
	}
	r.events = append(r.events, out.Events...)
	return nil
}

func (r *slaRepo) GetTicket(_ context.Context, _ uuid.UUID, id uuid.UUID) (*Ticket, error) {
	t, ok := r.tickets[id]
	if !ok {
		return nil, ErrTicketNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *slaRepo) UpdateTicketStatus(_ context.Context, _ uuid.UUID, id uuid.UUID, s TicketStatus) error {
	r.tickets[id].Status = s
	return nil
}

func (r *slaRepo) UpdateSLA(_ context.Context, _ uuid.UUID, id uuid.UUID, u SLAUpdate) error {
	r.updates = append(r.updates, u)
	t := r.tickets[id]
	if u.ResponseDueAt != nil {
		t.SLAResponseDueAt = u.ResponseDueAt
	}
	if u.ResolutionDueAt != nil {
		t.SLAResolutionDueAt = u.ResolutionDueAt
	}
	if u.PausedAt != nil {
		t.SLAPausedAt = u.PausedAt
	}
	if u.Resume {
		t.SLAPausedAt = nil
	}
	if u.PausedMinutes != nil {
		t.SLAPausedMinutes = *u.PausedMinutes
	}
	if u.ResolutionMinutes != nil {
		t.SLAResolutionMinutes = u.ResolutionMinutes
	}
	return nil
}

func (r *slaRepo) MarkSLABreached(_ context.Context, _ uuid.UUID, id uuid.UUID, response, resolution bool) (bool, error) {
	t := r.tickets[id]
	changed := (response && !t.SLAResponseBreached) || (resolution && !t.SLAResolutionBreached)
	t.SLAResponseBreached = t.SLAResponseBreached || response
	t.SLAResolutionBreached = t.SLAResolutionBreached || resolution
	return changed, nil
}

func (r *slaRepo) GetAgentEmail(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return "agent@shop.co.ke", nil
}

func (r *slaRepo) TeamAlertEmails(context.Context, uuid.UUID, uuid.UUID) ([]string, error) {
	return []string{"lead@shop.co.ke"}, nil
}

func (r *slaRepo) MarkSLAWarned(_ context.Context, _ uuid.UUID, id uuid.UUID, target string, _ time.Time) (bool, error) {
	key := id.String() + "/" + target
	if r.warned[key] {
		return false, nil
	}
	r.warned[key] = true
	return true, nil
}

func (r *slaRepo) TeamExists(_ context.Context, _ uuid.UUID, id uuid.UUID) (bool, error) {
	return r.teams[id], nil
}

func (r *slaRepo) Escalate(_ context.Context, _ uuid.UUID, id uuid.UUID, from int, p *TicketPriority, team *uuid.UUID, at time.Time) (bool, error) {
	t := r.tickets[id]
	if t.EscalationLevel != from {
		return false, nil
	}
	t.EscalationLevel, t.EscalatedAt = from+1, &at
	if p != nil {
		t.Priority = *p
	}
	if team != nil {
		t.AssignedTeamID, t.AssignedAgentID = team, nil
	}
	return true, nil
}

func (r *slaRepo) CreateComment(_ context.Context, _ uuid.UUID, c *TicketComment) error {
	r.comments = append(r.comments, c)
	return nil
}

func (r *slaRepo) count(eventType string) int {
	n := 0
	for _, e := range r.events {
		if e.Type == eventType {
			n++
		}
	}
	return n
}

// fixedSLA hands every ticket the same clock.
type fixedSLA struct{ c SLAClock }

func (f fixedSLA) ComputeDeadlines(context.Context, uuid.UUID, string, *uuid.UUID, time.Time) (*SLAPlan, error) {
	return nil, nil
}

func (f fixedSLA) PolicyClock(context.Context, uuid.UUID, *uuid.UUID) (SLAClock, error) {
	return f.c, nil
}

// A ticket held in pending_customer for 3 hours resolves 3 business hours later than planned
// without a breach, and records its net resolution minutes.
func TestStatusFlowPausesAndResumes(t *testing.T) {
	eat := nairobi(t)
	repo := newSLARepo()
	tk := slaTicket(t)
	repo.tickets[tk.ID] = tk
	s := NewService(repo, zap.NewNop())
	s.SetSLAService(fixedSLA{officeClock(t)})
	ctx := context.Background()

	step := func(at time.Time, to TicketStatus) {
		t.Helper()
		s.now = func() time.Time { return at }
		if err := s.UpdateTicketStatus(ctx, tk.TenantID, tk.ID, to); err != nil {
			t.Fatalf("to %s: %v", to, err)
		}
	}
	step(time.Date(2026, 11, 16, 10, 0, 0, 0, eat), StatusPendingCustomer)
	if repo.tickets[tk.ID].SLAPausedAt == nil {
		t.Fatal("pending_customer did not stop the clock")
	}
	step(time.Date(2026, 11, 16, 13, 0, 0, 0, eat), StatusOpen)
	step(time.Date(2026, 11, 17, 10, 0, 0, 0, eat), StatusResolved)

	got := repo.tickets[tk.ID]
	if want := time.Date(2026, 11, 17, 11, 0, 0, 0, eat); !got.SLAResolutionDueAt.Equal(want) {
		t.Errorf("resolution due = %s, want %s", got.SLAResolutionDueAt, want)
	}
	if got.SLAResolutionBreached {
		t.Error("resolved inside the moved deadline, yet marked breached")
	}
	// Monday 09:00 to Tuesday 10:00 is 10 business hours; less the 3-hour pause leaves 7.
	if got.SLAResolutionMinutes == nil || *got.SLAResolutionMinutes != 7*60 || got.SLAPausedMinutes != 180 {
		t.Errorf("resolution minutes = %v, paused = %d", got.SLAResolutionMinutes, got.SLAPausedMinutes)
	}
	if repo.count(EventTicketResolved) != 1 {
		t.Errorf("events = %v", repo.events)
	}
}

func TestBreachWarningOnce(t *testing.T) {
	repo := newSLARepo()
	tk := slaTicket(t)
	agent := uuid.New()
	tk.AssignedAgentID = &agent
	repo.tickets[tk.ID] = tk
	s := NewService(repo, zap.NewNop())
	w := BreachWarning{TenantID: tk.TenantID, TicketID: tk.ID, Target: clock.TargetResolution, DueAt: *tk.SLAResolutionDueAt}
	now := tk.SLAResolutionDueAt.Add(-30 * time.Minute)
	for i := 0; i < 3; i++ {
		changed, err := s.RecordBreachWarning(context.Background(), w, now)
		if err != nil || changed != (i == 0) {
			t.Fatalf("run %d: changed=%v err=%v", i, changed, err)
		}
	}
	if repo.count(EventSLABreachWarning) != 1 {
		t.Fatalf("breach_warning published %d times", repo.count(EventSLABreachWarning))
	}
	p := repo.events[0].Data
	if p["target"] != "resolution" || p["minutes_left"] != 30 || p["agent_email"] != "agent@shop.co.ke" || p["requester_email"] != nil {
		t.Errorf("payload = %v", p)
	}
	// The first-response target of the same ticket is a separate warning.
	w.Target = clock.TargetFirstResponse
	if changed, _ := s.RecordBreachWarning(context.Background(), w, now); !changed {
		t.Error("each target is warned once, independently")
	}
}

// A ticket held by a team with no agent warns the team's leads, never nobody.
func TestBreachWarningUnassignedGoesToTeam(t *testing.T) {
	repo := newSLARepo()
	tk := slaTicket(t)
	team := uuid.New()
	tk.AssignedAgentID, tk.AssignedTeamID = nil, &team
	repo.tickets[tk.ID] = tk
	s := NewService(repo, zap.NewNop())
	w := BreachWarning{TenantID: tk.TenantID, TicketID: tk.ID, Target: clock.TargetResolution, DueAt: *tk.SLAResolutionDueAt}
	if _, err := s.RecordBreachWarning(context.Background(), w, tk.SLAResolutionDueAt.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	p := repo.events[0].Data
	emails, _ := p["team_emails"].([]string)
	if p["agent_email"] != "" || len(emails) != 1 || emails[0] != "lead@shop.co.ke" {
		t.Errorf("payload = %v", p)
	}
}

func TestApplyEscalation(t *testing.T) {
	repo := newSLARepo()
	tk := slaTicket(t)
	agent, team := uuid.New(), uuid.New()
	tk.AssignedAgentID = &agent
	repo.tickets[tk.ID] = tk
	repo.teams[team] = true
	s := NewService(repo, zap.NewNop())
	rule := clock.EscalationRule{Target: clock.TargetResolution, When: clock.WhenAfterBreach, RaisePriority: true, TeamID: team.String()}
	due := EscalationDue{TenantID: tk.TenantID, TicketID: tk.ID, Level: 0, Rule: rule}
	now := tk.SLAResolutionDueAt.Add(time.Minute)

	for i := 0; i < 2; i++ {
		changed, err := s.ApplyEscalation(context.Background(), due, now)
		if err != nil || changed != (i == 0) {
			t.Fatalf("run %d: changed=%v err=%v", i, changed, err)
		}
	}
	got := repo.tickets[tk.ID]
	if got.EscalationLevel != 1 || got.EscalatedAt == nil || got.Priority != PriorityUrgent || got.AssignedTeamID == nil || *got.AssignedTeamID != team || got.AssignedAgentID != nil {
		t.Fatalf("ticket after escalation = %+v", got)
	}
	if repo.count(EventTicketEscalated) != 1 || len(repo.comments) != 1 {
		t.Fatalf("events %d, comments %d", repo.count(EventTicketEscalated), len(repo.comments))
	}
	c := repo.comments[0]
	if c.CommentType != CommentInternal || c.AuthorType != AuthorSystem || !strings.Contains(c.Content, "level 1") || !strings.Contains(c.Content, "high to urgent") {
		t.Errorf("note = %+v", c)
	}
	p := repo.events[0].Data
	if p["escalation_level"] != 1 || p["old_priority"] != "high" || p["priority"] != "urgent" || p["team_id"] != team.String() || p["old_team_id"] != "" {
		t.Errorf("payload = %v", p)
	}
}

func TestPlanEscalation(t *testing.T) {
	team := uuid.New()
	critical := &Ticket{Priority: PriorityCritical, AssignedTeamID: &team}
	if p := planEscalation(critical, clock.EscalationRule{RaisePriority: true, TeamID: team.String()}); p.priority != nil || p.team != nil {
		t.Errorf("critical with the same team changes nothing: %+v", p)
	}
	low := &Ticket{Priority: PriorityLow}
	if p := planEscalation(low, clock.EscalationRule{RaisePriority: true}); p.priority == nil || *p.priority != PriorityNormal || p.team != nil {
		t.Errorf("low raises to normal: %+v", p)
	}
	if nextPriority("unknown") != nil {
		t.Error("an unknown priority has no next level")
	}
}

func TestSLAScanSQL(t *testing.T) {
	for name, q := range map[string]string{"warnings": breachWarningsSQL, "escalations": escalationsDueSQL} {
		for _, want := range []string{"p.tenant_id = t.tenant_id", "t.sla_paused_at IS NULL", "t.deleted_at IS NULL", "LIMIT $3", "~ '^[0-9]{1,5}$'"} {
			if !strings.Contains(q, want) {
				t.Errorf("%s query lacks %q", name, want)
			}
		}
	}
	if !strings.Contains(escalationsDueSQL, "t.sla_resolution_due_at > $4") {
		t.Error("the escalation scan is bounded below by the lookback")
	}
	if !strings.Contains(markWarnedSQL, "->> $3::text) IS NULL") {
		t.Error("the warning mark must only match an unwarned target")
	}
}
