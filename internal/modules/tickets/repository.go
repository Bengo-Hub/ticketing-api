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
	// LatestOpenTicketByPhone returns the requester's most recently updated ticket on the channel
	// that is not closed or cancelled and was updated at or after since, or ErrTicketNotFound.
	LatestOpenTicketByPhone(ctx context.Context, tenantID uuid.UUID, channel TicketChannel, phone string, since time.Time) (*Ticket, error)
	// MergeTicketMetadata merges patch into the ticket's metadata at its top level in one statement.
	MergeTicketMetadata(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, patch map[string]any) error
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
	// FindCommentByExternalID returns the comment a channel message became, deleted or not, or
	// ErrCommentNotFound.
	FindCommentByExternalID(ctx context.Context, tenantID uuid.UUID, channel, externalID string) (*TicketComment, error)
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
	// TeamAlertEmails returns who hears about a team's SLA alerts: its active supervisors and
	// admins, or all its active agents when it has none, at most 20.
	TeamAlertEmails(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID) ([]string, error)

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

	// UpdateSLA writes the SLA columns the update names.
	UpdateSLA(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, u SLAUpdate) error
	// ListBreachWarnings returns targets due within their policy's notify_before_breach_min and
	// not yet warned, across tenants, soonest first.
	ListBreachWarnings(ctx context.Context, now time.Time, limit int) ([]BreachWarning, error)
	// MarkSLAWarned records the warning for one target in ticket metadata; changed is false when
	// it was already recorded.
	MarkSLAWarned(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, target string, at time.Time) (changed bool, err error)
	// ListEscalationsDue returns tickets whose next escalation rule is due, across tenants.
	ListEscalationsDue(ctx context.Context, now time.Time, limit int) ([]EscalationDue, error)
	// Escalate moves a ticket from fromLevel to fromLevel+1 with the new priority and team (nil
	// leaves each alone; a new team clears the agent). changed is false when another run already
	// moved the level.
	Escalate(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, fromLevel int, priority *TicketPriority, teamID *uuid.UUID, at time.Time) (changed bool, err error)
}
