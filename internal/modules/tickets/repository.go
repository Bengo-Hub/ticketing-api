package tickets

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository defines the interface for ticket data access. Every method is scoped to tenantID.
type Repository interface {
	// InTx runs fn against a repository bound to one database transaction. The events fn adds to
	// the outbox are written in that transaction, and it commits only when fn returns nil.
	InTx(ctx context.Context, tenantID uuid.UUID, fn func(repo Repository, out *Outbox) error) error
	// NextTicketNumber allocates the tenant's next ticket number atomically.
	NextTicketNumber(ctx context.Context, tenantID uuid.UUID) (string, error)

	// Ticket operations
	CreateTicket(ctx context.Context, tenantID uuid.UUID, ticket *Ticket) error
	GetTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) (*Ticket, error)
	GetTicketByNumber(ctx context.Context, tenantID uuid.UUID, ticketNumber string) (*Ticket, error)
	// FindTicketBySourceRef returns the ticket an internal caller created for (source,
	// source_ref.type, source_ref.id), or ErrTicketNotFound.
	FindTicketBySourceRef(ctx context.Context, tenantID uuid.UUID, source string, ref SourceRef) (*Ticket, error)
	ListTickets(ctx context.Context, tenantID uuid.UUID, filter TicketFilter) ([]*Ticket, int, error)
	UpdateTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, updates *UpdateTicketRequest) error
	UpdateTicketStatus(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, status TicketStatus) error
	AssignTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, agentID *uuid.UUID, teamID *uuid.UUID) error
	DeleteTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) error

	// SLA tracking
	MarkFirstResponse(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) error
	// MarkSLABreached sets breach flags that are still false; changed reports whether it did.
	MarkSLABreached(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, responseBreached, resolutionBreached bool) (changed bool, err error)

	// Comment operations
	CreateComment(ctx context.Context, tenantID uuid.UUID, comment *TicketComment) error
	GetComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) (*TicketComment, error)
	ListComments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketComment, error)
	UpdateComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID, content string) error
	DeleteComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) error

	// Attachment operations
	CreateAttachment(ctx context.Context, tenantID uuid.UUID, attachment *TicketAttachment) error
	GetAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) (*TicketAttachment, error)
	ListAttachments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketAttachment, error)
	DeleteAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) error

	// Category operations
	CreateCategory(ctx context.Context, tenantID uuid.UUID, category *TicketCategory) error
	GetCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (*TicketCategory, error)
	ListCategories(ctx context.Context, tenantID uuid.UUID, filter CategoryFilter) ([]*TicketCategory, int, error)
	UpdateCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID, updates *UpdateCategoryRequest) error
	DeleteCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) error
	GetCategoryTicketCount(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (int, error)
	GetCategoryChildCount(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (int, error)

	// Stats
	GetTicketCountsByStatus(ctx context.Context, tenantID uuid.UUID) (map[TicketStatus]int, error)
	GetTicketCountsByPriority(ctx context.Context, tenantID uuid.UUID) (map[TicketPriority]int, error)
	GetTicketTrend(ctx context.Context, tenantID uuid.UUID, from, to time.Time, bucketDays int) ([]TrendPoint, error)
	GetAgentTicketCount(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) (int, error)
	GetAgentEmail(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) (string, error)
	// GetAgentEmailByUser returns the email of the tenant's agent profile for an auth-api user.
	GetAgentEmailByUser(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (string, error)

	// ListTicketsPendingBreach returns open tickets with past-due SLA targets, across tenants.
	ListTicketsPendingBreach(ctx context.Context, now time.Time, limit int) ([]*Ticket, error)
}
