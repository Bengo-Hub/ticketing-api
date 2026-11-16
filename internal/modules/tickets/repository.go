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
	// ListTickets returns up to filter.Page.Limit+1 tickets after the cursor, so the caller learns
	// whether another page exists.
	ListTickets(ctx context.Context, tenantID uuid.UUID, filter TicketFilter) ([]*Ticket, error)
	// ListTags returns the tags of the tenant's recent tickets with their counts, most used
	// first, optionally only those starting with prefix.
	ListTags(ctx context.Context, tenantID uuid.UUID, prefix string, limit int) ([]TagCount, error)
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
	// ListComments returns up to q.Page.Limit+1 comments after the cursor, oldest first.
	ListComments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, q CommentQuery) ([]*TicketComment, error)
	UpdateComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID, content string) error
	DeleteComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) error

	// Attachment operations
	CreateAttachment(ctx context.Context, tenantID uuid.UUID, attachment *TicketAttachment) error
	GetAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) (*TicketAttachment, error)
	// ListAttachments returns up to q.Page.Limit+1 attachments after the cursor, oldest first.
	ListAttachments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, q AttachmentQuery) ([]*TicketAttachment, error)
	DeleteAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) error

	// MoveThread moves every comment and attachment of one ticket to another (a merge).
	MoveThread(ctx context.Context, tenantID uuid.UUID, fromTicketID, toTicketID uuid.UUID) error

	// AgentEmails returns the email of each id that is an agent of the tenant; ids of other
	// tenants or unknown ids are absent from the map.
	AgentEmails(ctx context.Context, tenantID uuid.UUID, agentIDs []uuid.UUID) (map[uuid.UUID]string, error)
	// TeamExists reports whether the team belongs to the tenant.
	TeamExists(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID) (bool, error)

	// Saved views
	CreateView(ctx context.Context, tenantID uuid.UUID, v *TicketView) error
	GetView(ctx context.Context, tenantID uuid.UUID, viewID uuid.UUID) (*TicketView, error)
	// ListViews returns the user's own views and the tenant's shared views, by name.
	ListViews(ctx context.Context, tenantID uuid.UUID, ownerUserID uuid.UUID, limit int) ([]*TicketView, error)
	UpdateView(ctx context.Context, tenantID uuid.UUID, v *TicketView) error
	DeleteView(ctx context.Context, tenantID uuid.UUID, viewID uuid.UUID) error

	// Macros
	CreateMacro(ctx context.Context, tenantID uuid.UUID, m *Macro) error
	GetMacro(ctx context.Context, tenantID uuid.UUID, macroID uuid.UUID) (*Macro, error)
	ListMacros(ctx context.Context, tenantID uuid.UUID, activeOnly bool, limit int) ([]*Macro, error)
	UpdateMacro(ctx context.Context, tenantID uuid.UUID, m *Macro) error
	DeleteMacro(ctx context.Context, tenantID uuid.UUID, macroID uuid.UUID) error

	// Ticket links
	CreateLink(ctx context.Context, tenantID uuid.UUID, l *TicketLink) error
	// ListLinks returns the links in which the ticket is either end.
	ListLinks(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, limit int) ([]*TicketLink, error)
	// DeleteLink removes a link that has the ticket at either end.
	DeleteLink(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, linkID uuid.UUID) error

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
