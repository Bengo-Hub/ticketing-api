package tickets

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SLAComputer computes SLA deadlines for new tickets.
type SLAComputer interface {
	ComputeDeadlines(ctx context.Context, tenantID uuid.UUID, priorityStr string, categoryID *uuid.UUID, createdAt time.Time) (*time.Time, *time.Time, error)
}

// RulesEvaluator evaluates auto-routing rules against a new ticket.
type RulesEvaluator interface {
	EvaluateRules(ctx context.Context, tenantID uuid.UUID, priority, channel, subject, description string, categoryID *uuid.UUID) *RoutingAction
}

// RoutingAction is the action to apply from a matched routing rule.
type RoutingAction struct {
	Type  string // "assign_team", "assign_agent", "set_priority"
	Value string // team/agent UUID string or priority string
}

// SSEPublisher broadcasts real-time events to connected SSE clients.
type SSEPublisher interface {
	Publish(tenantID uuid.UUID, eventType string, payload any)
}

// Service provides ticket business logic. Every change and the events it produces are written in
// one transaction (Repository.InTx), so an event exists exactly when its change committed.
type Service struct {
	repo    Repository
	logger  *zap.Logger
	sla     SLAComputer
	sse     SSEPublisher
	routing RulesEvaluator
	now     func() time.Time
}

// NewService creates a new ticket service.
func NewService(repo Repository, logger *zap.Logger) *Service {
	return &Service{repo: repo, logger: logger.Named("tickets"), now: time.Now}
}

// SetSLAService wires in the SLA service for deadline computation.
func (s *Service) SetSLAService(svc SLAComputer) { s.sla = svc }

// SetSSEBroker wires in the SSE broker for real-time client streaming.
func (s *Service) SetSSEBroker(b SSEPublisher) { s.sse = b }

// SetRoutingService wires in the routing rules evaluator.
func (s *Service) SetRoutingService(re RulesEvaluator) { s.routing = re }

func (s *Service) broadcast(tenantID uuid.UUID, eventType string, payload map[string]any) {
	if s.sse != nil {
		s.sse.Publish(tenantID, eventType, payload)
	}
}

// CreateTicket creates a ticket, numbered from the tenant's counter in the same transaction.
func (s *Service) CreateTicket(ctx context.Context, req *CreateTicketRequest) (*Ticket, error) {
	if req.Priority == "" {
		req.Priority = PriorityNormal
	}
	if req.Channel == "" {
		req.Channel = ChannelWeb
	}
	s.applyRouting(ctx, req)

	ticket := &Ticket{
		ID:                uuid.New(),
		TenantID:          req.TenantID,
		CustomerID:        req.CustomerID,
		CategoryID:        req.CategoryID,
		AssignedAgentID:   req.AssignedAgentID,
		AssignedTeamID:    req.AssignedTeamID,
		Subject:           req.Subject,
		Description:       req.Description,
		Status:            StatusNew,
		Priority:          req.Priority,
		Channel:           req.Channel,
		OrderID:           req.OrderID,
		ExternalReference: req.ExternalReference,
		CRMContactID:      req.CRMContactID,
		CRMLeadID:         req.CRMLeadID,
		Tags:              req.Tags,
		// The requester's contact is stored the same way on every create path (requester_*).
		Metadata: withRequester(req.Metadata, RequesterContact{
			Name: req.RequesterName, Email: req.RequesterEmail, Phone: req.RequesterPhone,
		}),
	}
	if s.sla != nil {
		responseDue, resolutionDue, err := s.sla.ComputeDeadlines(ctx, req.TenantID, string(ticket.Priority), ticket.CategoryID, s.now())
		if err != nil {
			s.logger.Warn("sla deadlines not computed", zap.Error(err))
		} else {
			ticket.SLAResponseDueAt, ticket.SLAResolutionDueAt = responseDue, resolutionDue
		}
	}

	err := s.repo.InTx(ctx, req.TenantID, func(repo Repository, out *Outbox) error {
		number, err := repo.NextTicketNumber(ctx, req.TenantID)
		if err != nil {
			return err
		}
		ticket.TicketNumber = number
		if err := repo.CreateTicket(ctx, req.TenantID, ticket); err != nil {
			return err
		}
		if ticket.CRMContactID != nil {
			out.CRMActivity(crmActivity(ticket, "ticket_opened"))
		}
		// The requester learns their ticket number; the tenant's helpdesk learns of a new ticket.
		out.Ticket(EventTicketCreated, TicketCreatedPayload(ticket))
		return nil
	})
	if err != nil {
		if err != ErrDuplicateSourceRef {
			s.logger.Error("failed to create ticket", zap.Error(err), zap.String("subject", req.Subject))
		}
		return nil, err
	}

	s.logger.Info("ticket created",
		zap.String("ticket_id", ticket.ID.String()), zap.String("ticket_number", ticket.TicketNumber))
	s.broadcast(ticket.TenantID, "ticket.created", map[string]any{"ticket_id": ticket.ID, "ticket_number": ticket.TicketNumber})
	return ticket, nil
}

// applyRouting applies the first matching routing rule to a new ticket.
func (s *Service) applyRouting(ctx context.Context, req *CreateTicketRequest) {
	if s.routing == nil {
		return
	}
	action := s.routing.EvaluateRules(ctx, req.TenantID, string(req.Priority), string(req.Channel), req.Subject, req.Description, req.CategoryID)
	if action == nil {
		return
	}
	switch action.Type {
	case "set_priority":
		req.Priority = TicketPriority(action.Value)
	case "assign_team":
		if id, err := uuid.Parse(action.Value); err == nil {
			req.AssignedTeamID = &id
		}
	case "assign_agent":
		if id, err := uuid.Parse(action.Value); err == nil {
			req.AssignedAgentID = &id
		}
	}
}

// GetTicket retrieves a ticket by ID.
func (s *Service) GetTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) (*Ticket, error) {
	return s.repo.GetTicket(ctx, tenantID, ticketID)
}

// GetTicketByNumber retrieves a ticket by its number.
func (s *Service) GetTicketByNumber(ctx context.Context, tenantID uuid.UUID, ticketNumber string) (*Ticket, error) {
	return s.repo.GetTicketByNumber(ctx, tenantID, ticketNumber)
}

// ListTickets lists tickets with filtering.
func (s *Service) ListTickets(ctx context.Context, tenantID uuid.UUID, filter TicketFilter) ([]*Ticket, int, error) {
	filter.TenantID = tenantID
	return s.repo.ListTickets(ctx, tenantID, filter)
}

// UpdateTicket updates a ticket.
func (s *Service) UpdateTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, req *UpdateTicketRequest) error {
	if err := s.repo.UpdateTicket(ctx, tenantID, ticketID, req); err != nil {
		s.logger.Error("failed to update ticket", zap.Error(err), zap.String("ticket_id", ticketID.String()))
		return err
	}
	return nil
}

// UpdateTicketStatus moves a ticket to a new status and records the events for it.
func (s *Service) UpdateTicketStatus(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, status TicketStatus) error {
	var from TicketStatus
	err := s.repo.InTx(ctx, tenantID, func(repo Repository, out *Outbox) error {
		ticket, err := repo.GetTicket(ctx, tenantID, ticketID)
		if err != nil {
			return err
		}
		from = ticket.Status
		if !isValidStatusTransition(from, status) {
			return ErrInvalidTransition
		}
		if err := repo.UpdateTicketStatus(ctx, tenantID, ticketID, status); err != nil {
			return err
		}
		if status != StatusResolved {
			// Every other change tells the requester where their ticket stands.
			if status != from {
				out.Ticket(EventTicketStatusChanged, TicketStatusChangedPayload(ticket, from, status))
			}
			return nil
		}
		now := s.now()
		if ticket.SLAResolutionDueAt != nil && !ticket.SLAResolutionBreached && now.After(*ticket.SLAResolutionDueAt) {
			if _, err := repo.MarkSLABreached(ctx, tenantID, ticketID, false, true); err != nil {
				return err
			}
		}
		out.Ticket(EventTicketResolved, TicketResolvedPayload(ticket, from, now))
		if ticket.CRMContactID != nil {
			out.CRMActivity(crmActivity(ticket, "ticket_resolved"))
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.logger.Info("ticket status updated", zap.String("ticket_id", ticketID.String()),
		zap.String("old_status", string(from)), zap.String("new_status", string(status)))
	s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": ticketID, "status": string(status)})
	return nil
}

// AssignTicket assigns a ticket to an agent and/or team; a new ticket given an agent opens.
func (s *Service) AssignTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, req *AssignTicketRequest) error {
	err := s.repo.InTx(ctx, tenantID, func(repo Repository, out *Outbox) error {
		if err := repo.AssignTicket(ctx, tenantID, ticketID, req.AgentID, req.TeamID); err != nil {
			return err
		}
		ticket, err := repo.GetTicket(ctx, tenantID, ticketID)
		if err != nil {
			return err
		}
		if ticket.Status == StatusNew && req.AgentID != nil {
			if err := repo.UpdateTicketStatus(ctx, tenantID, ticketID, StatusOpen); err != nil {
				return err
			}
			ticket.Status = StatusOpen
		}
		agentEmail := ""
		if req.AgentID != nil {
			// The assignee's email lets notifications-api email them directly.
			if agentEmail, err = repo.GetAgentEmail(ctx, tenantID, *req.AgentID); err != nil {
				return err
			}
		}
		out.Ticket(EventTicketAssigned, TicketAssignedPayload(ticket, req.AgentID, agentEmail))
		return nil
	})
	if err != nil {
		s.logger.Error("failed to assign ticket", zap.Error(err), zap.String("ticket_id", ticketID.String()))
		return err
	}
	s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": ticketID})
	return nil
}

// DeleteTicket soft-deletes a ticket.
func (s *Service) DeleteTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) error {
	if err := s.repo.DeleteTicket(ctx, tenantID, ticketID); err != nil {
		s.logger.Error("failed to delete ticket", zap.Error(err), zap.String("ticket_id", ticketID.String()))
		return err
	}
	return nil
}

// AddComment adds a comment. An agent's first public reply records the first response, and a
// public agent reply goes to the requester (internal notes and customer comments never do).
func (s *Service) AddComment(ctx context.Context, req *AddCommentRequest) (*TicketComment, error) {
	if req.CommentType == "" {
		req.CommentType = CommentPublic
	}
	comment := &TicketComment{
		ID:          uuid.New(),
		TenantID:    req.TenantID,
		TicketID:    req.TicketID,
		AuthorID:    req.AuthorID,
		AuthorType:  req.AuthorType,
		CommentType: req.CommentType,
		Content:     req.Content,
	}
	err := s.repo.InTx(ctx, req.TenantID, func(repo Repository, out *Outbox) error {
		if err := repo.CreateComment(ctx, req.TenantID, comment); err != nil {
			return err
		}
		if req.AuthorType != AuthorAgent || req.CommentType != CommentPublic {
			return nil
		}
		ticket, err := repo.GetTicket(ctx, req.TenantID, req.TicketID)
		if err != nil {
			return err
		}
		if err := repo.MarkFirstResponse(ctx, req.TenantID, req.TicketID); err != nil {
			return err
		}
		if ticket.FirstResponseAt == nil && ticket.SLAResponseDueAt != nil && !ticket.SLAResponseBreached && s.now().After(*ticket.SLAResponseDueAt) {
			if _, err := repo.MarkSLABreached(ctx, req.TenantID, req.TicketID, true, false); err != nil {
				return err
			}
		}
		agentEmail, err := repo.GetAgentEmailByUser(ctx, req.TenantID, req.AuthorID)
		if err != nil {
			return err
		}
		out.Ticket(EventTicketCommentAdded, TicketCommentAddedPayload(ticket, comment, agentEmail))
		return nil
	})
	if err != nil {
		s.logger.Error("failed to add comment", zap.Error(err), zap.String("ticket_id", req.TicketID.String()))
		return nil, err
	}
	s.broadcast(req.TenantID, "comment.created", map[string]any{"ticket_id": req.TicketID, "comment_id": comment.ID})
	return comment, nil
}

// GetComment retrieves a comment by ID.
func (s *Service) GetComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) (*TicketComment, error) {
	return s.repo.GetComment(ctx, tenantID, commentID)
}

// ListComments lists comments for a ticket.
func (s *Service) ListComments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketComment, error) {
	return s.repo.ListComments(ctx, tenantID, ticketID)
}

// UpdateComment updates a comment's content.
func (s *Service) UpdateComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID, content string) error {
	return s.repo.UpdateComment(ctx, tenantID, commentID, content)
}

// DeleteComment soft-deletes a comment.
func (s *Service) DeleteComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) error {
	return s.repo.DeleteComment(ctx, tenantID, commentID)
}

// ListAttachments lists attachments for a ticket.
func (s *Service) ListAttachments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketAttachment, error) {
	return s.repo.ListAttachments(ctx, tenantID, ticketID)
}

// GetAttachment retrieves an attachment by ID.
func (s *Service) GetAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) (*TicketAttachment, error) {
	return s.repo.GetAttachment(ctx, tenantID, attachmentID)
}
