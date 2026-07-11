package tickets

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/events"
)

// SLAComputer computes SLA deadlines for new tickets.
// Takes a string priority and optional category ID, returns the response and resolution due times.
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

// Notifier sends lifecycle notifications fire-and-forget.
type Notifier interface {
	SendTicketCreated(ctx context.Context, tenantSlug string, customerEmail string, data map[string]interface{})
	SendTicketAssigned(ctx context.Context, tenantSlug string, agentEmail string, data map[string]interface{})
	SendTicketResolved(ctx context.Context, tenantSlug string, customerEmail string, data map[string]interface{})
	SendAgentComment(ctx context.Context, tenantSlug string, customerEmail string, data map[string]interface{})
}

// SSEPublisher broadcasts real-time events to connected SSE clients.
type SSEPublisher interface {
	Publish(tenantID uuid.UUID, eventType string, payload any)
}

// Service provides ticket business logic.
type Service struct {
	repo    Repository
	logger  *zap.Logger
	publisher *events.Publisher
	sla     SLAComputer
	notif   Notifier
	sse     SSEPublisher
	routing RulesEvaluator
}

// NewService creates a new ticket service.
func NewService(repo Repository, logger *zap.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: logger.Named("tickets"),
	}
}

// SetPublisher sets the event publisher for ticket lifecycle events.
func (s *Service) SetPublisher(p *events.Publisher) {
	s.publisher = p
}

// SetSLAService wires in the SLA service for deadline computation.
func (s *Service) SetSLAService(svc SLAComputer) {
	s.sla = svc
}

// SetNotifier wires in the notifications client.
func (s *Service) SetNotifier(n Notifier) {
	s.notif = n
}

// SetSSEBroker wires in the SSE broker for real-time client streaming.
func (s *Service) SetSSEBroker(b SSEPublisher) {
	s.sse = b
}

// SetRoutingService wires in the routing rules evaluator.
func (s *Service) SetRoutingService(re RulesEvaluator) {
	s.routing = re
}

// Ticket operations

// CreateTicket creates a new ticket.
func (s *Service) CreateTicket(ctx context.Context, req *CreateTicketRequest) (*Ticket, error) {
	// Generate ticket number
	ticketNumber, err := s.repo.GenerateTicketNumber(ctx, req.TenantID)
	if err != nil {
		s.logger.Error("failed to generate ticket number", zap.Error(err))
		return nil, err
	}

	// Set defaults
	if req.Priority == "" {
		req.Priority = PriorityNormal
	}
	if req.Channel == "" {
		req.Channel = ChannelWeb
	}

	// Evaluate routing rules and apply the first matching action.
	if s.routing != nil {
		if action := s.routing.EvaluateRules(ctx, req.TenantID, string(req.Priority), string(req.Channel), req.Subject, req.Description, req.CategoryID); action != nil {
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
	}

	ticket := &Ticket{
		ID:                uuid.New(),
		TenantID:          req.TenantID,
		TicketNumber:      ticketNumber,
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
		Metadata:          req.Metadata,
	}

	if err := s.repo.CreateTicket(ctx, req.TenantID, ticket); err != nil {
		s.logger.Error("failed to create ticket",
			zap.Error(err),
			zap.String("subject", req.Subject),
		)
		return nil, err
	}

	// Compute SLA deadlines if a policy matches this ticket's priority and category.
	if s.sla != nil {
		responseDue, resolutionDue, err := s.sla.ComputeDeadlines(ctx, req.TenantID, string(ticket.Priority), ticket.CategoryID, ticket.CreatedAt)
		if err == nil && (responseDue != nil || resolutionDue != nil) {
			if setErr := s.repo.SetSLADeadlines(ctx, req.TenantID, ticket.ID, responseDue, resolutionDue); setErr != nil {
				s.logger.Warn("failed to set sla deadlines", zap.Error(setErr))
			} else {
				ticket.SLAResponseDueAt = responseDue
				ticket.SLAResolutionDueAt = resolutionDue
			}
		}
	}

	// Publish CRM activity event if a CRM contact is linked.
	if s.publisher != nil && ticket.CRMContactID != nil {
		_ = s.publisher.PublishCRMActivityRequest(ctx, ticket.TenantID, map[string]any{
			"entity_type":   "contact",
			"entity_id":     ticket.CRMContactID.String(),
			"activity_type": "ticket_opened",
			"external_ref":  ticket.ID.String(),
			"subject":       ticket.Subject,
			"source":        "ticketing-api",
		})
	}

	s.logger.Info("ticket created",
		zap.String("ticket_id", ticket.ID.String()),
		zap.String("ticket_number", ticket.TicketNumber),
	)

	if s.sse != nil {
		s.sse.Publish(ticket.TenantID, "ticket.created", map[string]any{"ticket_id": ticket.ID, "ticket_number": ticket.TicketNumber})
	}

	return ticket, nil
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
		s.logger.Error("failed to update ticket",
			zap.Error(err),
			zap.String("ticket_id", ticketID.String()),
		)
		return err
	}
	return nil
}

// UpdateTicketStatus updates a ticket's status.
func (s *Service) UpdateTicketStatus(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, status TicketStatus) error {
	// Get current ticket to validate transition
	ticket, err := s.repo.GetTicket(ctx, tenantID, ticketID)
	if err != nil {
		return err
	}

	// Validate status transition
	if !s.isValidStatusTransition(ticket.Status, status) {
		return ErrInvalidTransition
	}

	if err := s.repo.UpdateTicketStatus(ctx, tenantID, ticketID, status); err != nil {
		s.logger.Error("failed to update ticket status",
			zap.Error(err),
			zap.String("ticket_id", ticketID.String()),
			zap.String("status", string(status)),
		)
		return err
	}

	s.logger.Info("ticket status updated",
		zap.String("ticket_id", ticketID.String()),
		zap.String("old_status", string(ticket.Status)),
		zap.String("new_status", string(status)),
	)

	if s.sse != nil {
		s.sse.Publish(tenantID, "ticket.updated", map[string]any{"ticket_id": ticketID, "status": string(status)})
	}

	if status == StatusResolved {
		// Check if resolution SLA was breached
		if ticket.SLAResolutionDueAt != nil && !ticket.SLAResolutionBreached {
			if time.Now().After(*ticket.SLAResolutionDueAt) {
				if err := s.repo.MarkSLABreached(ctx, tenantID, ticketID, false, true); err != nil {
					s.logger.Warn("failed to mark resolution sla breach", zap.Error(err))
				}
			}
		}

		// Publish resolved event
		if s.publisher != nil {
			_ = s.publisher.PublishTicketResolved(ctx, tenantID, map[string]any{
				"ticket_id":     ticketID.String(),
				"ticket_number": ticket.TicketNumber,
				"subject":       ticket.Subject,
			})

			// Publish CRM activity event if a CRM contact is linked.
			if ticket.CRMContactID != nil {
				_ = s.publisher.PublishCRMActivityRequest(ctx, tenantID, map[string]any{
					"entity_type":   "contact",
					"entity_id":     ticket.CRMContactID.String(),
					"activity_type": "ticket_resolved",
					"external_ref":  ticketID.String(),
					"subject":       ticket.Subject,
					"source":        "ticketing-api",
				})
			}
		}
	}

	return nil
}

// AssignTicket assigns a ticket to an agent and/or team.
func (s *Service) AssignTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, req *AssignTicketRequest) error {
	if err := s.repo.AssignTicket(ctx, tenantID, ticketID, req.AgentID, req.TeamID); err != nil {
		s.logger.Error("failed to assign ticket",
			zap.Error(err),
			zap.String("ticket_id", ticketID.String()),
		)
		return err
	}

	// Update status to open if it's new
	ticket, err := s.repo.GetTicket(ctx, tenantID, ticketID)
	if err == nil && ticket.Status == StatusNew && req.AgentID != nil {
		_ = s.repo.UpdateTicketStatus(ctx, tenantID, ticketID, StatusOpen)
	}

	s.logger.Info("ticket assigned",
		zap.String("ticket_id", ticketID.String()),
		zap.Any("agent_id", req.AgentID),
		zap.Any("team_id", req.TeamID),
	)

	// Publish assigned event
	if s.publisher != nil && ticket != nil {
		agentIDStr := ""
		agentEmail := ""
		if req.AgentID != nil {
			agentIDStr = req.AgentID.String()
			// Include the agent's email so notifications-api can email the assignee
			// directly; without it the notification silently fell back to the tenant
			// mailbox and the agent never learned they were assigned.
			if email, err := s.repo.GetAgentEmail(ctx, tenantID, *req.AgentID); err == nil {
				agentEmail = email
			}
		}
		_ = s.publisher.PublishTicketAssigned(ctx, tenantID, map[string]any{
			"ticket_id":     ticketID.String(),
			"ticket_number": ticket.TicketNumber,
			"subject":       ticket.Subject,
			"agent_id":      agentIDStr,
			"agent_email":   agentEmail,
			"priority":      string(ticket.Priority),
		})
	}

	return nil
}

// DeleteTicket soft-deletes a ticket.
func (s *Service) DeleteTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) error {
	if err := s.repo.DeleteTicket(ctx, tenantID, ticketID); err != nil {
		s.logger.Error("failed to delete ticket",
			zap.Error(err),
			zap.String("ticket_id", ticketID.String()),
		)
		return err
	}

	s.logger.Info("ticket deleted",
		zap.String("ticket_id", ticketID.String()),
	)

	return nil
}

// Comment operations

// AddComment adds a comment to a ticket.
func (s *Service) AddComment(ctx context.Context, req *AddCommentRequest) (*TicketComment, error) {
	// Set default comment type
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

	if err := s.repo.CreateComment(ctx, req.TenantID, comment); err != nil {
		s.logger.Error("failed to add comment",
			zap.Error(err),
			zap.String("ticket_id", req.TicketID.String()),
		)
		return nil, err
	}

	// Mark first response if this is an agent's first public response
	if req.AuthorType == AuthorAgent && req.CommentType == CommentPublic {
		if err := s.repo.MarkFirstResponse(ctx, req.TenantID, req.TicketID); err != nil {
			s.logger.Warn("failed to mark first response", zap.Error(err))
		} else {
			// Check if first-response SLA was breached
			ticket, tErr := s.repo.GetTicket(ctx, req.TenantID, req.TicketID)
			if tErr == nil && ticket.SLAResponseDueAt != nil && !ticket.SLAResponseBreached {
				if time.Now().After(*ticket.SLAResponseDueAt) {
					if bErr := s.repo.MarkSLABreached(ctx, req.TenantID, req.TicketID, true, false); bErr != nil {
						s.logger.Warn("failed to mark response sla breach", zap.Error(bErr))
					}
				}
			}
		}
	}

	s.logger.Info("comment added",
		zap.String("ticket_id", req.TicketID.String()),
		zap.String("comment_id", comment.ID.String()),
	)

	if s.sse != nil {
		s.sse.Publish(req.TenantID, "comment.created", map[string]any{"ticket_id": req.TicketID, "comment_id": comment.ID})
	}

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

// Attachment operations

// ListAttachments lists attachments for a ticket.
func (s *Service) ListAttachments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketAttachment, error) {
	return s.repo.ListAttachments(ctx, tenantID, ticketID)
}

// GetAttachment retrieves an attachment by ID.
func (s *Service) GetAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) (*TicketAttachment, error) {
	return s.repo.GetAttachment(ctx, tenantID, attachmentID)
}

// Category operations

// CreateCategory creates a new category.
func (s *Service) CreateCategory(ctx context.Context, req *CreateCategoryRequest) (*TicketCategory, error) {
	category := &TicketCategory{
		ID:          uuid.New(),
		TenantID:    req.TenantID,
		ParentID:    req.ParentID,
		Name:        req.Name,
		Description: req.Description,
		Icon:        req.Icon,
		SortOrder:   req.SortOrder,
		IsActive:    true,
		Metadata:    req.Metadata,
	}

	if err := s.repo.CreateCategory(ctx, req.TenantID, category); err != nil {
		s.logger.Error("failed to create category",
			zap.Error(err),
			zap.String("name", req.Name),
		)
		return nil, err
	}

	s.logger.Info("category created",
		zap.String("category_id", category.ID.String()),
		zap.String("name", category.Name),
	)

	return category, nil
}

// GetCategory retrieves a category by ID.
func (s *Service) GetCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (*TicketCategory, error) {
	return s.repo.GetCategory(ctx, tenantID, categoryID)
}

// ListCategories lists categories with filtering.
func (s *Service) ListCategories(ctx context.Context, tenantID uuid.UUID, filter CategoryFilter) ([]*TicketCategory, int, error) {
	filter.TenantID = tenantID
	return s.repo.ListCategories(ctx, tenantID, filter)
}

// UpdateCategory updates a category.
func (s *Service) UpdateCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID, req *UpdateCategoryRequest) error {
	if err := s.repo.UpdateCategory(ctx, tenantID, categoryID, req); err != nil {
		s.logger.Error("failed to update category",
			zap.Error(err),
			zap.String("category_id", categoryID.String()),
		)
		return err
	}
	return nil
}

// DeleteCategory soft-deletes a category.
func (s *Service) DeleteCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) error {
	// Check for children
	childCount, err := s.repo.GetCategoryChildCount(ctx, tenantID, categoryID)
	if err != nil {
		return err
	}
	if childCount > 0 {
		return ErrCategoryHasChildren
	}

	// Check for tickets
	ticketCount, err := s.repo.GetCategoryTicketCount(ctx, tenantID, categoryID)
	if err != nil {
		return err
	}
	if ticketCount > 0 {
		return ErrCategoryHasTickets
	}

	if err := s.repo.DeleteCategory(ctx, tenantID, categoryID); err != nil {
		s.logger.Error("failed to delete category",
			zap.Error(err),
			zap.String("category_id", categoryID.String()),
		)
		return err
	}

	s.logger.Info("category deleted",
		zap.String("category_id", categoryID.String()),
	)

	return nil
}

// Stats

// GetTicketStats returns ticket statistics.
func (s *Service) GetTicketStats(ctx context.Context, tenantID uuid.UUID) (map[TicketStatus]int, error) {
	return s.repo.GetTicketCountsByStatus(ctx, tenantID)
}

// Helper methods

func (s *Service) isValidStatusTransition(from, to TicketStatus) bool {
	// Define valid transitions
	validTransitions := map[TicketStatus][]TicketStatus{
		StatusNew:             {StatusOpen, StatusInProgress, StatusCancelled},
		StatusOpen:            {StatusInProgress, StatusPendingCustomer, StatusPendingInternal, StatusResolved, StatusClosed, StatusCancelled},
		StatusInProgress:      {StatusOpen, StatusPendingCustomer, StatusPendingInternal, StatusResolved, StatusClosed, StatusCancelled},
		StatusPendingCustomer: {StatusOpen, StatusInProgress, StatusResolved, StatusClosed, StatusCancelled},
		StatusPendingInternal: {StatusOpen, StatusInProgress, StatusResolved, StatusClosed, StatusCancelled},
		StatusResolved:        {StatusOpen, StatusClosed}, // Can reopen or close
		StatusClosed:          {StatusOpen},               // Can only reopen
		StatusCancelled:       {},                         // Terminal state
	}

	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}

	for _, status := range allowed {
		if status == to {
			return true
		}
	}
	return false
}

// GetAnalytics returns combined ticket analytics: by-status, by-priority, and trend.
func (s *Service) GetAnalytics(ctx context.Context, tenantID uuid.UUID, days int) (*AnalyticsResponse, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -days)
	bucketDays := 1
	if days > 90 {
		bucketDays = 7
	}

	byStatus, err := s.repo.GetTicketCountsByStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byPriority, err := s.repo.GetTicketCountsByPriority(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	trend, err := s.repo.GetTicketTrend(ctx, tenantID, from, to, bucketDays)
	if err != nil {
		return nil, err
	}

	return &AnalyticsResponse{
		ByStatus:   byStatus,
		ByPriority: byPriority,
		Trend:      trend,
	}, nil
}
