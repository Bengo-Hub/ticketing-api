package tickets

import (
	"context"

	"github.com/Bengo-Hub/httpware/richtext"
	"github.com/google/uuid"
)

// The steps below run inside Repository.InTx. Single changes, bulk updates and macros share them,
// so every path applies the same rules and records the same events.

// statusTx moves a ticket to status, recording ticket.status_changed, or ticket.resolved and the
// late-resolution breach. It returns the status the ticket had.
func (s *Service) statusTx(ctx context.Context, repo Repository, out *Outbox, tenantID, ticketID uuid.UUID, status TicketStatus) (TicketStatus, error) {
	ticket, err := repo.GetTicket(ctx, tenantID, ticketID)
	if err != nil {
		return "", err
	}
	from := ticket.Status
	if !isValidStatusTransition(from, status) {
		return from, ErrInvalidTransition
	}
	if err := repo.UpdateTicketStatus(ctx, tenantID, ticketID, status); err != nil {
		return from, err
	}
	now := s.now()
	// The SLA clock stops on a pending status and restarts, with moved deadlines, when it ends.
	if err := s.slaStatusTx(ctx, repo, ticket, status, now); err != nil {
		return from, err
	}
	if status != StatusResolved {
		// Every other change tells the requester where their ticket stands.
		if status != from {
			out.Ticket(EventTicketStatusChanged, TicketStatusChangedPayload(ticket, from, status))
		}
		return from, nil
	}
	if ticket.SLAResolutionDueAt != nil && !ticket.SLAResolutionBreached && now.After(*ticket.SLAResolutionDueAt) {
		if _, err := repo.MarkSLABreached(ctx, tenantID, ticketID, false, true); err != nil {
			return from, err
		}
	}
	out.Ticket(EventTicketResolved, TicketResolvedPayload(ticket, from, now))
	if ticket.CRMContactID != nil {
		out.CRMActivity(crmActivity(ticket, "ticket_resolved"))
	}
	return from, nil
}

// checkAssignees refuses an agent or team that does not belong to the tenant.
func checkAssignees(ctx context.Context, repo Repository, tenantID uuid.UUID, agentID, teamID *uuid.UUID) error {
	if agentID != nil {
		emails, err := repo.AgentEmails(ctx, tenantID, []uuid.UUID{*agentID})
		if err != nil {
			return err
		}
		if _, ok := emails[*agentID]; !ok {
			return ErrAssigneeNotFound
		}
	}
	if teamID != nil {
		ok, err := repo.TeamExists(ctx, tenantID, *teamID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAssigneeNotFound
		}
	}
	return nil
}

// assignTx sets the ticket's agent and team (nil clears), opens a new ticket given an agent and
// records ticket.assigned with the agent's email.
func (s *Service) assignTx(ctx context.Context, repo Repository, out *Outbox, tenantID, ticketID uuid.UUID, agentID, teamID *uuid.UUID) error {
	if err := checkAssignees(ctx, repo, tenantID, agentID, teamID); err != nil {
		return err
	}
	if err := repo.AssignTicket(ctx, tenantID, ticketID, agentID, teamID); err != nil {
		return err
	}
	ticket, err := repo.GetTicket(ctx, tenantID, ticketID)
	if err != nil {
		return err
	}
	if ticket.Status == StatusNew && agentID != nil {
		if err := repo.UpdateTicketStatus(ctx, tenantID, ticketID, StatusOpen); err != nil {
			return err
		}
		ticket.Status = StatusOpen
	}
	agentEmail := ""
	if agentID != nil {
		// The assignee's email lets notifications-api email them directly.
		if agentEmail, err = repo.GetAgentEmail(ctx, tenantID, *agentID); err != nil {
			return err
		}
	}
	out.Ticket(EventTicketAssigned, TicketAssignedPayload(ticket, agentID, agentEmail))
	return nil
}

// commentTx stores a comment with its agent mentions and records ticket.comment_added for a
// public agent reply and ticket.mentioned for each mentioned agent.
func (s *Service) commentTx(ctx context.Context, repo Repository, out *Outbox, req *AddCommentRequest) (*TicketComment, error) {
	if req.CommentType == "" {
		req.CommentType = CommentPublic
	}
	req.Content = richtext.Sanitize(req.Content)
	comment := &TicketComment{
		ID:          uuid.New(),
		TenantID:    req.TenantID,
		TicketID:    req.TicketID,
		AuthorID:    req.AuthorID,
		AuthorType:  req.AuthorType,
		CommentType: req.CommentType,
		Content:     req.Content,
		Metadata:    req.Metadata,
	}
	withChannelMessage(comment)
	// Only staff mention agents; a customer typing the token mentions nobody.
	var mentionEmails map[uuid.UUID]string
	if req.AuthorType == AuthorAgent {
		if ids := ParseMentions(req.Content); len(ids) > 0 {
			emails, err := repo.AgentEmails(ctx, req.TenantID, ids)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				if _, ok := emails[id]; ok {
					comment.Mentions = append(comment.Mentions, id)
				}
			}
			mentionEmails = emails
		}
	}
	if err := repo.CreateComment(ctx, req.TenantID, comment); err != nil {
		return nil, err
	}
	publicReply := req.AuthorType == AuthorAgent && req.CommentType == CommentPublic
	if !publicReply && len(comment.Mentions) == 0 {
		return comment, nil
	}
	ticket, err := repo.GetTicket(ctx, req.TenantID, req.TicketID)
	if err != nil {
		return nil, err
	}
	for _, id := range comment.Mentions {
		out.Ticket(EventTicketMentioned, TicketMentionedPayload(ticket, comment, id, mentionEmails[id]))
	}
	if !publicReply {
		return comment, nil
	}
	late, err := s.slaFirstResponseTx(ctx, repo, ticket, s.now())
	if err != nil {
		return nil, err
	}
	if err := repo.MarkFirstResponse(ctx, req.TenantID, req.TicketID); err != nil {
		return nil, err
	}
	if late {
		if _, err := repo.MarkSLABreached(ctx, req.TenantID, req.TicketID, true, false); err != nil {
			return nil, err
		}
	}
	agentEmail, err := repo.GetAgentEmailByUser(ctx, req.TenantID, req.AuthorID)
	if err != nil {
		return nil, err
	}
	out.Ticket(EventTicketCommentAdded, TicketCommentAddedPayload(ticket, comment, agentEmail, s.now()))
	return comment, nil
}

// TicketChanges are the field changes a bulk update or a macro makes to one ticket. A nil field
// leaves the ticket's value alone.
type TicketChanges struct {
	Status     *TicketStatus
	Priority   *TicketPriority
	CategoryID *uuid.UUID
	// Assign is true when the agent or team changes; AgentID and TeamID are then the final values.
	Assign  bool
	AgentID *uuid.UUID
	TeamID  *uuid.UUID
	// Tags is the ticket's full new tag list (nil: unchanged).
	Tags []string
}

// Check reports whether the changes can apply to t. An assignment that opens a new ticket is
// taken into account before the status transition is checked.
func (c TicketChanges) Check(t *Ticket) error {
	if c.Status == nil || *c.Status == t.Status {
		return nil
	}
	from := t.Status
	if c.Assign && c.AgentID != nil && from == StatusNew {
		from = StatusOpen
	}
	if *c.Status != from && !isValidStatusTransition(from, *c.Status) {
		return ErrInvalidTransition
	}
	return nil
}

// applyChangesTx applies the changes in order: fields, then the assignment, then the status.
func (s *Service) applyChangesTx(ctx context.Context, repo Repository, out *Outbox, t *Ticket, c TicketChanges) error {
	if c.Priority != nil || c.CategoryID != nil || c.Tags != nil {
		upd := &UpdateTicketRequest{Priority: c.Priority, CategoryID: c.CategoryID, Tags: c.Tags}
		if err := repo.UpdateTicket(ctx, t.TenantID, t.ID, upd); err != nil {
			return err
		}
	}
	if c.Assign {
		if err := s.assignTx(ctx, repo, out, t.TenantID, t.ID, c.AgentID, c.TeamID); err != nil {
			return err
		}
	}
	if c.Status != nil {
		current, err := repo.GetTicket(ctx, t.TenantID, t.ID)
		if err != nil {
			return err
		}
		if current.Status != *c.Status {
			if _, err := s.statusTx(ctx, repo, out, t.TenantID, t.ID, *c.Status); err != nil {
				return err
			}
		}
	}
	return nil
}
