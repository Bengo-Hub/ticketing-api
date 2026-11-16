package tickets

import (
	"context"
	"time"

	"github.com/Bengo-Hub/httpware/richtext"
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
	files   FileStore
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
	// Every create path (signed in, public form, internal S2S) passes here, so the description is
	// sanitised once, before routing reads it and before it is stored.
	req.Description = richtext.Sanitize(req.Description)
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
		Tags:              NormalizeTags(req.Tags),
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

// ListTickets returns one keyset page of the tenant's tickets.
func (s *Service) ListTickets(ctx context.Context, tenantID uuid.UUID, filter TicketFilter) (Page[*Ticket], error) {
	filter.TenantID = tenantID
	if !ValidSort(filter.Sort) {
		filter.Sort = SortCreatedDesc
	}
	if filter.Page.Limit <= 0 {
		filter.Page.Limit = defaultPageLimit
	}
	rows, err := s.repo.ListTickets(ctx, tenantID, filter)
	if err != nil {
		return Page[*Ticket]{}, err
	}
	return pageFrom(rows, filter.Page.Limit, ticketKey), nil
}

func ticketKey(t *Ticket) (time.Time, uuid.UUID) { return t.CreatedAt, t.ID }

// ListTags returns the tenant's tags in use, most used first.
func (s *Service) ListTags(ctx context.Context, tenantID uuid.UUID, prefix string, limit int) ([]TagCount, error) {
	return s.repo.ListTags(ctx, tenantID, NormalizeTag(prefix), limit)
}

// UpdateTicket updates a ticket's fields. An agent or team named here must belong to the tenant.
func (s *Service) UpdateTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, req *UpdateTicketRequest) error {
	req.Tags = NormalizeTags(req.Tags)
	err := s.repo.InTx(ctx, tenantID, func(repo Repository, _ *Outbox) error {
		if err := checkAssignees(ctx, repo, tenantID, req.AssignedAgentID, req.AssignedTeamID); err != nil {
			return err
		}
		return repo.UpdateTicket(ctx, tenantID, ticketID, req)
	})
	if err != nil {
		return err
	}
	s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": ticketID})
	return nil
}

// UpdateTicketStatus moves a ticket to a new status and records the events for it.
func (s *Service) UpdateTicketStatus(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, status TicketStatus) error {
	err := s.repo.InTx(ctx, tenantID, func(repo Repository, out *Outbox) error {
		_, err := s.statusTx(ctx, repo, out, tenantID, ticketID, status)
		return err
	})
	if err != nil {
		return err
	}
	s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": ticketID, "status": string(status)})
	return nil
}

// AssignTicket assigns a ticket to an agent and/or team; a new ticket given an agent opens.
func (s *Service) AssignTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, req *AssignTicketRequest) error {
	err := s.repo.InTx(ctx, tenantID, func(repo Repository, out *Outbox) error {
		return s.assignTx(ctx, repo, out, tenantID, ticketID, req.AgentID, req.TeamID)
	})
	if err != nil {
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

// AddComment adds a comment. An agent's first public reply records the first response, a public
// agent reply goes to the requester (internal notes and customer comments never do), and each
// agent the comment @mentions is told.
func (s *Service) AddComment(ctx context.Context, req *AddCommentRequest) (*TicketComment, error) {
	var comment *TicketComment
	err := s.repo.InTx(ctx, req.TenantID, func(repo Repository, out *Outbox) error {
		var err error
		comment, err = s.commentTx(ctx, repo, out, req)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.broadcast(req.TenantID, "comment.created", map[string]any{"ticket_id": req.TicketID, "comment_id": comment.ID})
	return comment, nil
}

// GetComment retrieves a comment by ID.
func (s *Service) GetComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) (*TicketComment, error) {
	return s.repo.GetComment(ctx, tenantID, commentID)
}

// maxThreadComments bounds the whole-thread reads of the S2S and public ticket views.
const maxThreadComments = 500

// ListComments returns the ticket's thread without internal notes, oldest first, up to
// maxThreadComments, for the S2S and public views.
func (s *Service) ListComments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketComment, error) {
	rows, err := s.repo.ListComments(ctx, tenantID, ticketID, CommentQuery{
		Page: PageParams{Limit: maxThreadComments}, PublicOnly: true,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) > maxThreadComments {
		rows = rows[:maxThreadComments]
	}
	return rows, nil
}

// PageComments returns one keyset page of a ticket's comments, oldest first.
func (s *Service) PageComments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, q CommentQuery) (Page[*TicketComment], error) {
	rows, err := s.repo.ListComments(ctx, tenantID, ticketID, q)
	if err != nil {
		return Page[*TicketComment]{}, err
	}
	return pageFrom(rows, q.Page.Limit, func(c *TicketComment) (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }), nil
}

// UpdateComment updates a comment's content.
func (s *Service) UpdateComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID, content string) error {
	return s.repo.UpdateComment(ctx, tenantID, commentID, richtext.Sanitize(content))
}

// DeleteComment soft-deletes a comment.
func (s *Service) DeleteComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) error {
	return s.repo.DeleteComment(ctx, tenantID, commentID)
}
