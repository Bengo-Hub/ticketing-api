package tickets

import (
	"time"

	"github.com/google/uuid"
)

// TicketStatus represents the status of a ticket.
type TicketStatus string

const (
	StatusNew             TicketStatus = "new"
	StatusOpen            TicketStatus = "open"
	StatusInProgress      TicketStatus = "in_progress"
	StatusPendingCustomer TicketStatus = "pending_customer"
	StatusPendingInternal TicketStatus = "pending_internal"
	StatusResolved        TicketStatus = "resolved"
	StatusClosed          TicketStatus = "closed"
	StatusCancelled       TicketStatus = "cancelled"
)

// TicketPriority represents ticket priority levels.
type TicketPriority string

const (
	PriorityLow      TicketPriority = "low"
	PriorityNormal   TicketPriority = "normal"
	PriorityHigh     TicketPriority = "high"
	PriorityUrgent   TicketPriority = "urgent"
	PriorityCritical TicketPriority = "critical"
)

// TicketChannel represents the channel through which a ticket was created.
type TicketChannel string

const (
	ChannelWeb      TicketChannel = "web"
	ChannelEmail    TicketChannel = "email"
	ChannelAPI      TicketChannel = "api"
	ChannelChat     TicketChannel = "chat"
	ChannelWhatsApp TicketChannel = "whatsapp"
	ChannelPhone    TicketChannel = "phone"
	ChannelInternal TicketChannel = "internal"
)

// Valid reports whether c is one of the ticket channels.
func (c TicketChannel) Valid() bool {
	switch c {
	case ChannelWeb, ChannelEmail, ChannelAPI, ChannelChat, ChannelWhatsApp, ChannelPhone, ChannelInternal:
		return true
	}
	return false
}

// CommentType represents different types of comments.
type CommentType string

const (
	CommentPublic   CommentType = "public"
	CommentInternal CommentType = "internal"
	CommentSystem   CommentType = "system"
)

// AuthorType represents the type of comment author.
type AuthorType string

const (
	AuthorCustomer AuthorType = "customer"
	AuthorAgent    AuthorType = "agent"
	AuthorSystem   AuthorType = "system"
)

// Ticket represents a support ticket.
type Ticket struct {
	ID                    uuid.UUID      `json:"id"`
	TenantID              uuid.UUID      `json:"tenant_id"`
	TicketNumber          string         `json:"ticket_number"`
	CustomerID            uuid.UUID      `json:"customer_id"`
	CategoryID            *uuid.UUID     `json:"category_id,omitempty"`
	AssignedAgentID       *uuid.UUID     `json:"assigned_agent_id,omitempty"`
	AssignedTeamID        *uuid.UUID     `json:"assigned_team_id,omitempty"`
	Subject               string         `json:"subject"`
	Description           string         `json:"description"`
	Status                TicketStatus   `json:"status"`
	Priority              TicketPriority `json:"priority"`
	Channel               TicketChannel  `json:"channel"`
	OrderID               *uuid.UUID     `json:"order_id,omitempty"`
	ExternalReference     string         `json:"external_reference,omitempty"`
	CRMContactID          *uuid.UUID     `json:"crm_contact_id,omitempty"`
	CRMLeadID             *uuid.UUID     `json:"crm_lead_id,omitempty"`
	SLAResponseDueAt      *time.Time     `json:"sla_response_due_at,omitempty"`
	SLAResolutionDueAt    *time.Time     `json:"sla_resolution_due_at,omitempty"`
	SLAResponseBreached   bool           `json:"sla_response_breached"`
	SLAResolutionBreached bool           `json:"sla_resolution_breached"`
	SLAPolicyID           *uuid.UUID     `json:"sla_policy_id,omitempty"`
	// SLAPausedAt is set while a pending status stops the SLA clock.
	SLAPausedAt      *time.Time `json:"sla_paused_at,omitempty"`
	SLAPausedMinutes int        `json:"sla_paused_minutes"`
	// The elapsed minutes to first response and to resolution in the policy's clock, net of pauses.
	SLAFirstResponseMinutes *int           `json:"sla_first_response_minutes,omitempty"`
	SLAResolutionMinutes    *int           `json:"sla_resolution_minutes,omitempty"`
	FirstResponseAt         *time.Time     `json:"first_response_at,omitempty"`
	ResolvedAt              *time.Time     `json:"resolved_at,omitempty"`
	ClosedAt                *time.Time     `json:"closed_at,omitempty"`
	ReopenedAt              *time.Time     `json:"reopened_at,omitempty"`
	EscalationLevel         int            `json:"escalation_level"`
	EscalatedAt             *time.Time     `json:"escalated_at,omitempty"`
	Tags                    []string       `json:"tags,omitempty"`
	Metadata                map[string]any `json:"metadata,omitempty"`
	// WhatsAppWindowExpiresAt is when the 24-hour WhatsApp reply window closes (derived from
	// metadata whatsapp_last_inbound_at, never stored); null for other channels and for a
	// WhatsApp ticket with no inbound message yet.
	WhatsAppWindowExpiresAt *time.Time `json:"whatsapp_window_expires_at"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// TicketComment represents a comment on a ticket.
type TicketComment struct {
	ID          uuid.UUID   `json:"id"`
	TenantID    uuid.UUID   `json:"tenant_id"`
	TicketID    uuid.UUID   `json:"ticket_id"`
	AuthorID    uuid.UUID   `json:"author_id"`
	AuthorType  AuthorType  `json:"author_type"`
	CommentType CommentType `json:"comment_type"`
	Content     string      `json:"content"`
	// Mentions are the agent ids the comment @mentions (staff comments only).
	Mentions []uuid.UUID    `json:"mentions,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	// Channel, ExternalID, Sender and EmailSubject describe a customer message that came in on a
	// channel (email or WhatsApp). They are read from the comment metadata, never stored apart,
	// and are empty on every other comment.
	Channel      string         `json:"channel,omitempty"`
	ExternalID   string         `json:"external_id,omitempty"`
	Sender       *CommentSender `json:"sender,omitempty"`
	EmailSubject string         `json:"email_subject,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// CommentSender is who wrote a channel message: the requester, or a colleague of theirs on a
// threaded email.
type CommentSender struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

// TicketAttachment represents a file attachment on a ticket. The storage key never leaves the
// API; clients ask for a signed download link.
type TicketAttachment struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	TicketID     uuid.UUID  `json:"ticket_id"`
	CommentID    *uuid.UUID `json:"comment_id,omitempty"`
	Filename     string     `json:"filename"`
	ContentType  string     `json:"content_type"`
	Size         int64      `json:"size"`
	StoragePath  string     `json:"-"`
	UploadedByID uuid.UUID  `json:"uploaded_by_id"`
	// Internal attachments belong to an internal note and are never shown to the customer.
	Internal  bool           `json:"internal"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// TicketCategory represents a ticket category.
type TicketCategory struct {
	ID          uuid.UUID      `json:"id"`
	TenantID    uuid.UUID      `json:"tenant_id"`
	ParentID    *uuid.UUID     `json:"parent_id,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Icon        string         `json:"icon,omitempty"`
	SortOrder   int            `json:"sort_order"`
	IsActive    bool           `json:"is_active"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Request types

// CreateTicketRequest represents a request to create a ticket.
type CreateTicketRequest struct {
	TenantID          uuid.UUID      `json:"-"`
	CustomerID        uuid.UUID      `json:"customer_id"`
	CategoryID        *uuid.UUID     `json:"category_id,omitempty"`
	AssignedAgentID   *uuid.UUID     `json:"assigned_agent_id,omitempty"`
	AssignedTeamID    *uuid.UUID     `json:"assigned_team_id,omitempty"`
	Subject           string         `json:"subject"`
	Description       string         `json:"description"`
	Priority          TicketPriority `json:"priority,omitempty"`
	Channel           TicketChannel  `json:"channel,omitempty"`
	OrderID           *uuid.UUID     `json:"order_id,omitempty"`
	ExternalReference string         `json:"external_reference,omitempty"`
	CRMContactID      *uuid.UUID     `json:"crm_contact_id,omitempty"`
	CRMLeadID         *uuid.UUID     `json:"crm_lead_id,omitempty"`
	Tags              []string       `json:"tags,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	// Requester contact, stored in metadata as requester_name/requester_email/requester_phone on
	// every create path. Ticket events carry it so the requester is told by email, WhatsApp or SMS.
	RequesterName  string `json:"requester_name,omitempty"`
	RequesterEmail string `json:"requester_email,omitempty"`
	RequesterPhone string `json:"requester_phone,omitempty"`
	// Inbound is set only by the channel ingest: the email or WhatsApp message the ticket opens
	// from (its dedupe key) and the files already stored for it. Never read from a body.
	Inbound *InboundOrigin `json:"-"`
}

// UpdateTicketRequest represents a request to update a ticket.
type UpdateTicketRequest struct {
	CategoryID      *uuid.UUID      `json:"category_id,omitempty"`
	AssignedAgentID *uuid.UUID      `json:"assigned_agent_id,omitempty"`
	AssignedTeamID  *uuid.UUID      `json:"assigned_team_id,omitempty"`
	Subject         *string         `json:"subject,omitempty"`
	Priority        *TicketPriority `json:"priority,omitempty"`
	Tags            []string        `json:"tags,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
}

// AssignTicketRequest represents a request to assign a ticket.
type AssignTicketRequest struct {
	AgentID *uuid.UUID `json:"agent_id,omitempty"`
	TeamID  *uuid.UUID `json:"team_id,omitempty"`
}

// AddCommentRequest represents a request to add a comment.
type AddCommentRequest struct {
	TenantID uuid.UUID `json:"-"`
	TicketID uuid.UUID `json:"-"`
	// AuthorID and AuthorType come from the signed-in caller, never from the body.
	AuthorID    uuid.UUID   `json:"-"`
	AuthorType  AuthorType  `json:"-"`
	CommentType CommentType `json:"comment_type,omitempty"`
	Content     string      `json:"content"`
	// Metadata is set by the service (merge notes), never from the body.
	Metadata map[string]any `json:"-"`
}

// CreateCategoryRequest represents a request to create a category.
type CreateCategoryRequest struct {
	TenantID    uuid.UUID      `json:"-"`
	ParentID    *uuid.UUID     `json:"parent_id,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Icon        string         `json:"icon,omitempty"`
	SortOrder   int            `json:"sort_order,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// UpdateCategoryRequest represents a request to update a category.
type UpdateCategoryRequest struct {
	ParentID    *uuid.UUID     `json:"parent_id,omitempty"`
	Name        *string        `json:"name,omitempty"`
	Description *string        `json:"description,omitempty"`
	Icon        *string        `json:"icon,omitempty"`
	SortOrder   *int           `json:"sort_order,omitempty"`
	IsActive    *bool          `json:"is_active,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// CreatePublicTicketRequest is the payload for unauthenticated ticket creation.
type CreatePublicTicketRequest struct {
	TenantID      uuid.UUID     `json:"-"`
	CustomerEmail string        `json:"email"`
	CustomerName  string        `json:"name"`
	Subject       string        `json:"subject"`
	Description   string        `json:"description"`
	CategoryID    *uuid.UUID    `json:"category_id,omitempty"`
	Channel       TicketChannel `json:"channel,omitempty"`
	OrderID       *uuid.UUID    `json:"order_id,omitempty"`
}

// PublicTicketResponse is the slim response returned to unauthenticated callers.
type PublicTicketResponse struct {
	TicketID     uuid.UUID    `json:"ticket_id"`
	TicketNumber string       `json:"ticket_number"`
	Status       TicketStatus `json:"status"`
}

// AddPublicCommentRequest is the payload for an unauthenticated customer reply.
type AddPublicCommentRequest struct {
	TenantID      uuid.UUID `json:"-"`
	TicketID      uuid.UUID `json:"-"`
	CustomerEmail string    `json:"-"` // verified by OTP
	Content       string    `json:"content"`
}

// Filter types

// TicketFilter represents filtering options for tickets.
type TicketFilter struct {
	TenantID        uuid.UUID
	Status          *TicketStatus
	Statuses        []TicketStatus
	Priority        *TicketPriority
	CustomerID      *uuid.UUID
	AssignedAgentID *uuid.UUID
	AssignedTeamID  *uuid.UUID
	CategoryID      *uuid.UUID
	OrderID         *uuid.UUID
	Channel         *TicketChannel
	// Tags must all be on a ticket (jsonb containment).
	Tags          []string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	// Search is the normalised search term (pagination.SearchTerm).
	Search string
	// Sort is SortCreatedDesc (default) or SortCreatedAsc.
	Sort string
	Page PageParams
}

// Ticket list orders. Both page by (created_at, id).
const (
	SortCreatedDesc = "created_desc"
	SortCreatedAsc  = "created_asc"
)

// ValidSort reports whether s is a ticket list order.
func ValidSort(s string) bool { return s == SortCreatedDesc || s == SortCreatedAsc }

// CommentQuery pages a ticket's comments oldest first. PublicOnly leaves out internal notes.
type CommentQuery struct {
	Page       PageParams
	PublicOnly bool
	// Newest pages from the latest comment back, so a long thread opens at its end.
	Newest bool
}

// AttachmentQuery pages a ticket's attachments oldest first. PublicOnly leaves out internal ones.
type AttachmentQuery struct {
	Page       PageParams
	PublicOnly bool
}

// CategoryFilter represents filtering options for categories.
type CategoryFilter struct {
	TenantID uuid.UUID
	ParentID *uuid.UUID
	IsActive *bool
	Offset   int
	Limit    int
}
