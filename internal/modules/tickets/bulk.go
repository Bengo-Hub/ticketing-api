package tickets

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// maxBulkTickets is the most tickets one bulk update may change.
const maxBulkTickets = 100

// BulkUpdateRequest is the body of PUT /tickets/bulk. Every field but ids is optional; at least
// one change is required. agent_id and team_id set the assignee and leave the other one as it is.
type BulkUpdateRequest struct {
	IDs      []uuid.UUID     `json:"ids"`
	Status   *TicketStatus   `json:"status,omitempty"`
	Priority *TicketPriority `json:"priority,omitempty"`
	AgentID  *uuid.UUID      `json:"agent_id,omitempty"`
	TeamID   *uuid.UUID      `json:"team_id,omitempty"`
	AddTags  []string        `json:"add_tags,omitempty"`
}

// BulkResult is the outcome for one ticket.
type BulkResult struct {
	ID    uuid.UUID `json:"id"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
}

// Validate checks the request and drops repeated ids, keeping the first order.
func (r *BulkUpdateRequest) Validate() error {
	seen := make(map[uuid.UUID]bool, len(r.IDs))
	ids := r.IDs[:0]
	for _, id := range r.IDs {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	r.IDs = ids
	switch {
	case len(r.IDs) == 0:
		return invalid("ids must name at least one ticket")
	case len(r.IDs) > maxBulkTickets:
		return invalid("at most 100 tickets can be updated at once")
	case r.Status == nil && r.Priority == nil && r.AgentID == nil && r.TeamID == nil && len(NormalizeTags(r.AddTags)) == 0:
		return invalid("nothing to change")
	case r.Status != nil && !validStatus(*r.Status):
		return invalid("unknown status")
	case r.Priority != nil && !validPriority(*r.Priority):
		return invalid("unknown priority")
	}
	return nil
}

// changesFor is what the request changes on t.
func (r *BulkUpdateRequest) changesFor(t *Ticket) TicketChanges {
	c := TicketChanges{Status: r.Status}
	if r.Priority != nil && *r.Priority != t.Priority {
		c.Priority = r.Priority
	}
	if r.AgentID != nil || r.TeamID != nil {
		c.Assign, c.AgentID, c.TeamID = true, t.AssignedAgentID, t.AssignedTeamID
		if r.AgentID != nil {
			c.AgentID = r.AgentID
		}
		if r.TeamID != nil {
			c.TeamID = r.TeamID
		}
	}
	if add := NormalizeTags(r.AddTags); len(add) > 0 {
		c.Tags = MergeTags(t.Tags, add)
	}
	return c
}

// BulkUpdate applies the request to every ticket in one transaction, with the same rules and
// events as single changes. A ticket that is missing or cannot take the change is reported and
// skipped; any other error rolls the whole update back.
func (s *Service) BulkUpdate(ctx context.Context, tenantID uuid.UUID, req *BulkUpdateRequest) ([]BulkResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var results []BulkResult
	err := s.repo.InTx(ctx, tenantID, func(repo Repository, out *Outbox) error {
		results = make([]BulkResult, 0, len(req.IDs))
		if err := checkAssignees(ctx, repo, tenantID, req.AgentID, req.TeamID); err != nil {
			return err
		}
		for _, id := range req.IDs {
			t, err := repo.GetTicket(ctx, tenantID, id)
			if errors.Is(err, ErrTicketNotFound) {
				results = append(results, BulkResult{ID: id, Error: ErrTicketNotFound.Error()})
				continue
			}
			if err != nil {
				return err
			}
			c := req.changesFor(t)
			if err := c.Check(t); err != nil {
				results = append(results, BulkResult{ID: id, Error: err.Error()})
				continue
			}
			if err := s.applyChangesTx(ctx, repo, out, t, c); err != nil {
				return err
			}
			results = append(results, BulkResult{ID: id, OK: true})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		if r.OK {
			s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": r.ID})
		}
	}
	return results, nil
}

func validStatus(s TicketStatus) bool {
	_, ok := validTransitions[s]
	return ok
}

func validPriority(p TicketPriority) bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent, PriorityCritical:
		return true
	}
	return false
}
