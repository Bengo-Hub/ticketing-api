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
	ChannelPhone    TicketChannel = "phone"
	ChannelInternal TicketChannel = "internal"
)

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
	ID                   uuid.UUID       `json:"id"`
	TenantID             uuid.UUID       `json:"tenant_id"`
	TicketNumber         string          `json:"ticket_number"`
	CustomerID           uuid.UUID       `json:"customer_id"`
	CategoryID           *uuid.UUID      `json:"category_id,omitempty"`
	AssignedAgentID      *uuid.UUID      `json:"assigned_agent_id,omitempty"`
	AssignedTeamID       *uuid.UUID      `json:"assigned_team_id,omitempty"`
	Subject              string          `json:"subject"`
	Description          string          `json:"description"`
	Status               TicketStatus    `json:"status"`
	Priority             TicketPriority  `json:"priority"`
	Channel              TicketChannel   `json:"channel"`
	OrderID              *uuid.UUID      `json:"order_id,omitempty"`
	ExternalReference    string          `json:"external_reference,omitempty"`
	SLAResponseDueAt     *time.Time      `json:"sla_response_due_at,omitempty"`
	SLAResolutionDueAt   *time.Time      `json:"sla_resolution_due_at,omitempty"`
	SLAResponseBreached  bool            `json:"sla_response_breached"`
	SLAResolutionBreached bool           `json:"sla_resolution_breached"`
	FirstResponseAt      *time.Time      `json:"first_response_at,omitempty"`
	ResolvedAt           *time.Time      `json:"resolved_at,omitempty"`
	ClosedAt             *time.Time      `json:"closed_at,omitempty"`
	ReopenedAt           *time.Time      `json:"reopened_at,omitempty"`
	EscalationLevel      int             `json:"escalation_level"`
	EscalatedAt          *time.Time      `json:"escalated_at,omitempty"`
	Tags                 []string        `json:"tags,omitempty"`
	Metadata             map[string]any  `json:"metadata,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// TicketComment represents a comment on a ticket.
type TicketComment struct {
	ID          uuid.UUID      `json:"id"`
	TenantID    uuid.UUID      `json:"tenant_id"`
	TicketID    uuid.UUID      `json:"ticket_id"`
	AuthorID    uuid.UUID      `json:"author_id"`
	AuthorType  AuthorType     `json:"author_type"`
	CommentType CommentType    `json:"comment_type"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// TicketAttachment represents a file attachment on a ticket.
type TicketAttachment struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"tenant_id"`
	TicketID     uuid.UUID `json:"ticket_id"`
	CommentID    *uuid.UUID `json:"comment_id,omitempty"`
	Filename     string    `json:"filename"`
	ContentType  string    `json:"content_type"`
	Size         int64     `json:"size"`
	StoragePath  string    `json:"storage_path"`
	UploadedByID uuid.UUID `json:"uploaded_by_id"`
	CreatedAt    time.Time `json:"created_at"`
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
	TenantID          uuid.UUID       `json:"-"`
	CustomerID        uuid.UUID       `json:"customer_id"`
	CategoryID        *uuid.UUID      `json:"category_id,omitempty"`
	Subject           string          `json:"subject"`
	Description       string          `json:"description"`
	Priority          TicketPriority  `json:"priority,omitempty"`
	Channel           TicketChannel   `json:"channel,omitempty"`
	OrderID           *uuid.UUID      `json:"order_id,omitempty"`
	ExternalReference string          `json:"external_reference,omitempty"`
	Tags              []string        `json:"tags,omitempty"`
	Metadata          map[string]any  `json:"metadata,omitempty"`
}

// UpdateTicketRequest represents a request to update a ticket.
type UpdateTicketRequest struct {
	CategoryID        *uuid.UUID      `json:"category_id,omitempty"`
	AssignedAgentID   *uuid.UUID      `json:"assigned_agent_id,omitempty"`
	AssignedTeamID    *uuid.UUID      `json:"assigned_team_id,omitempty"`
	Subject           *string         `json:"subject,omitempty"`
	Priority          *TicketPriority `json:"priority,omitempty"`
	Tags              []string        `json:"tags,omitempty"`
	Metadata          map[string]any  `json:"metadata,omitempty"`
}

// AssignTicketRequest represents a request to assign a ticket.
type AssignTicketRequest struct {
	AgentID *uuid.UUID `json:"agent_id,omitempty"`
	TeamID  *uuid.UUID `json:"team_id,omitempty"`
}

// AddCommentRequest represents a request to add a comment.
type AddCommentRequest struct {
	TenantID    uuid.UUID   `json:"-"`
	TicketID    uuid.UUID   `json:"-"`
	AuthorID    uuid.UUID   `json:"author_id"`
	AuthorType  AuthorType  `json:"author_type"`
	CommentType CommentType `json:"comment_type,omitempty"`
	Content     string      `json:"content"`
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
	Tags            []string
	CreatedAfter    *time.Time
	CreatedBefore   *time.Time
	Offset          int
	Limit           int
}

// CategoryFilter represents filtering options for categories.
type CategoryFilter struct {
	TenantID uuid.UUID
	ParentID *uuid.UUID
	IsActive *bool
	Offset   int
	Limit    int
}
