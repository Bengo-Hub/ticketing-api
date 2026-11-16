package tickets

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

// Event types the SLA worker records (subjects ticketing.sla.breach_warning and
// ticketing.ticket.escalated).
const (
	EventSLABreachWarning = "sla.breach_warning"
	EventTicketEscalated  = "ticket.escalated"
)

// priorityLadder is the order escalation raises a priority along.
var priorityLadder = []TicketPriority{PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent, PriorityCritical}

// nextPriority is the priority one level up, or nil at the top (or for an unknown priority).
func nextPriority(p TicketPriority) *TicketPriority {
	for i, step := range priorityLadder {
		if step == p && i+1 < len(priorityLadder) {
			next := priorityLadder[i+1]
			return &next
		}
	}
	return nil
}

// escalationPlan is what a rule changes on a ticket; nil fields stay as they are.
type escalationPlan struct {
	priority *TicketPriority
	team     *uuid.UUID
}

// planEscalation applies a rule to a ticket: one priority level up when the rule raises it and
// the ticket is below critical, and the rule's team when the ticket is not already with it.
func planEscalation(t *Ticket, r clock.EscalationRule) escalationPlan {
	var p escalationPlan
	if r.RaisePriority {
		p.priority = nextPriority(t.Priority)
	}
	if team := r.Team(); team != nil && (t.AssignedTeamID == nil || *t.AssignedTeamID != *team) {
		p.team = team
	}
	return p
}

// escalationNote is the internal note an escalation leaves on the ticket.
func escalationNote(level int, r clock.EscalationRule, from *Ticket, p escalationPlan) string {
	target := "resolution"
	if r.Target == clock.TargetFirstResponse {
		target = "first response"
	}
	var b strings.Builder
	if r.Minutes == 0 {
		fmt.Fprintf(&b, "Escalated to level %d: the %s deadline passed.", level, target)
	} else if r.When == clock.WhenBeforeBreach {
		fmt.Fprintf(&b, "Escalated to level %d: the %s deadline is %d minutes away.", level, target, r.Minutes)
	} else {
		fmt.Fprintf(&b, "Escalated to level %d: the %s deadline passed %d minutes ago.", level, target, r.Minutes)
	}
	if p.priority != nil {
		fmt.Fprintf(&b, " Priority raised from %s to %s.", from.Priority, *p.priority)
	}
	if p.team != nil {
		b.WriteString(" Reassigned to the escalation team.")
	}
	return b.String()
}

// RecordBreachWarning records one pre-breach warning and its event in one transaction. It reports
// false when the target was already warned, so each target is warned once.
func (s *Service) RecordBreachWarning(ctx context.Context, w BreachWarning, now time.Time) (bool, error) {
	changed := false
	err := s.repo.InTx(ctx, w.TenantID, func(repo Repository, out *Outbox) error {
		var err error
		if changed, err = repo.MarkSLAWarned(ctx, w.TenantID, w.TicketID, w.Target, now); err != nil || !changed {
			return err
		}
		t, err := repo.GetTicket(ctx, w.TenantID, w.TicketID)
		if err != nil {
			return err
		}
		agentEmail, teamEmails, err := alertRecipients(ctx, repo, w.TenantID, t)
		if err != nil {
			return err
		}
		out.Ticket(EventSLABreachWarning, SLABreachWarningPayload(t, agentEmail, teamEmails, w.Target, w.DueAt, now))
		return nil
	})
	return changed, err
}

// alertRecipients is who an internal SLA alert reaches: the assigned agent, or, for a ticket with
// only a team, the team's supervisors and admins (all its active agents when it has none). An
// escalation to a team clears the agent, so the team is told.
func alertRecipients(ctx context.Context, repo Repository, tenantID uuid.UUID, t *Ticket) (string, []string, error) {
	if t.AssignedAgentID != nil {
		email, err := repo.GetAgentEmail(ctx, tenantID, *t.AssignedAgentID)
		if err != nil || email != "" {
			return email, nil, err
		}
	}
	if t.AssignedTeamID == nil {
		return "", nil, nil
	}
	emails, err := repo.TeamAlertEmails(ctx, tenantID, *t.AssignedTeamID)
	return "", emails, err
}

// ApplyEscalation applies a due escalation rule: the next level, the raised priority and the
// team, an internal note, and ticketing.ticket.escalated, in one transaction. It reports false
// when the ticket already moved past that level.
func (s *Service) ApplyEscalation(ctx context.Context, e EscalationDue, now time.Time) (bool, error) {
	changed := false
	err := s.repo.InTx(ctx, e.TenantID, func(repo Repository, out *Outbox) error {
		before, err := repo.GetTicket(ctx, e.TenantID, e.TicketID)
		if err != nil || before.EscalationLevel != e.Level {
			return err
		}
		plan := planEscalation(before, e.Rule)
		if plan.team != nil {
			// A team deleted since the rule was saved is skipped; the rest of the rule still applies.
			ok, err := repo.TeamExists(ctx, e.TenantID, *plan.team)
			if err != nil {
				return err
			}
			if !ok {
				plan.team = nil
			}
		}
		if changed, err = repo.Escalate(ctx, e.TenantID, e.TicketID, e.Level, plan.priority, plan.team, now); err != nil || !changed {
			return err
		}
		level := e.Level + 1
		if err := repo.CreateComment(ctx, e.TenantID, &TicketComment{
			ID: uuid.New(), TenantID: e.TenantID, TicketID: e.TicketID,
			AuthorType: AuthorSystem, CommentType: CommentInternal,
			Content:  escalationNote(level, e.Rule, before, plan),
			Metadata: map[string]any{"escalation_level": level, "target": e.Rule.Target},
		}); err != nil {
			return err
		}
		after, err := repo.GetTicket(ctx, e.TenantID, e.TicketID)
		if err != nil {
			return err
		}
		agentEmail, teamEmails, err := alertRecipients(ctx, repo, e.TenantID, after)
		if err != nil {
			return err
		}
		out.Ticket(EventTicketEscalated, TicketEscalatedPayload(before, after, agentEmail, teamEmails, e.Rule))
		return nil
	})
	return changed, err
}

// SLABreachWarningPayload is ticketing.sla.breach_warning: a target is about to breach. Like the
// breach events it goes to the agent (or the tenant's support contact) and carries no requester
// contact.
func SLABreachWarningPayload(t *Ticket, agentEmail string, teamEmails []string, target string, dueAt, now time.Time) map[string]any {
	p := slaEventBase(t)
	p["agent_email"] = agentEmail
	p["team_emails"] = nonNilStrings(teamEmails)
	p["target"] = target
	p["due_at"] = dueAt.UTC().Format(time.RFC3339)
	p["minutes_left"] = int(dueAt.Sub(now) / time.Minute)
	p["event_type"] = EventSLABreachWarning
	return p
}

// TicketEscalatedPayload is ticketing.ticket.escalated, with the priority and team before and
// after. It carries no requester contact.
func TicketEscalatedPayload(before, after *Ticket, agentEmail string, teamEmails []string, r clock.EscalationRule) map[string]any {
	p := slaEventBase(after)
	p["agent_email"] = agentEmail
	p["team_emails"] = nonNilStrings(teamEmails)
	p["escalation_level"] = after.EscalationLevel
	p["escalated_at"] = ""
	if after.EscalatedAt != nil {
		p["escalated_at"] = after.EscalatedAt.UTC().Format(time.RFC3339)
	}
	p["old_priority"] = string(before.Priority)
	p["old_team_id"] = uuidText(before.AssignedTeamID)
	p["target"] = r.Target
	p["when"] = r.When
	p["minutes"] = r.Minutes
	p["event_type"] = EventTicketEscalated
	return p
}

// nonNilStrings keeps an empty list as [] in the event JSON rather than null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// slaEventBase is the ticket part of the internal SLA events: no requester contact.
func slaEventBase(t *Ticket) map[string]any {
	return map[string]any{
		"ticket_id":     t.ID.String(),
		"ticket_number": t.TicketNumber,
		"subject":       t.Subject,
		"priority":      string(t.Priority),
		"status":        string(t.Status),
		"agent_id":      uuidText(t.AssignedAgentID),
		"team_id":       uuidText(t.AssignedTeamID),
	}
}

func uuidText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
