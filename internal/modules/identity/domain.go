package identity

import (
	"time"

	"github.com/google/uuid"
)

// AgentRole represents the role of an agent.
type AgentRole string

const (
	RoleAgent      AgentRole = "agent"
	RoleSupervisor AgentRole = "supervisor"
	RoleAdmin      AgentRole = "admin"
)

// AgentStatus represents the availability status of an agent.
type AgentStatus string

const (
	StatusAvailable AgentStatus = "available"
	StatusBusy      AgentStatus = "busy"
	StatusAway      AgentStatus = "away"
	StatusOffline   AgentStatus = "offline"
)

// Team represents a support team.
type Team struct {
	ID          uuid.UUID              `json:"id"`
	TenantID    uuid.UUID              `json:"tenant_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	IsActive    bool                   `json:"is_active"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	AgentCount  int                    `json:"agent_count,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// Agent represents a support agent.
type Agent struct {
	ID                   uuid.UUID              `json:"id"`
	TenantID             uuid.UUID              `json:"tenant_id"`
	UserID               uuid.UUID              `json:"user_id"`
	TeamID               *uuid.UUID             `json:"team_id,omitempty"`
	DisplayName          string                 `json:"display_name"`
	Email                string                 `json:"email"`
	AvatarURL            string                 `json:"avatar_url,omitempty"`
	Role                 AgentRole              `json:"role"`
	Status               AgentStatus            `json:"status"`
	MaxConcurrentTickets int                    `json:"max_concurrent_tickets"`
	CurrentTicketCount   int                    `json:"current_ticket_count"`
	Skills               []string               `json:"skills,omitempty"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
	IsActive             bool                   `json:"is_active"`
	LastActiveAt         *time.Time             `json:"last_active_at,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

// CreateTeamRequest represents a request to create a team.
type CreateTeamRequest struct {
	TenantID    uuid.UUID              `json:"tenant_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateTeamRequest represents a request to update a team.
type UpdateTeamRequest struct {
	Name        *string                `json:"name,omitempty"`
	Description *string                `json:"description,omitempty"`
	IsActive    *bool                  `json:"is_active,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// CreateAgentRequest represents a request to create an agent.
type CreateAgentRequest struct {
	TenantID             uuid.UUID              `json:"tenant_id"`
	UserID               uuid.UUID              `json:"user_id"`
	TeamID               *uuid.UUID             `json:"team_id,omitempty"`
	DisplayName          string                 `json:"display_name"`
	Email                string                 `json:"email"`
	AvatarURL            string                 `json:"avatar_url,omitempty"`
	Role                 AgentRole              `json:"role"`
	MaxConcurrentTickets int                    `json:"max_concurrent_tickets"`
	Skills               []string               `json:"skills,omitempty"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateAgentRequest represents a request to update an agent.
type UpdateAgentRequest struct {
	TeamID               *uuid.UUID             `json:"team_id,omitempty"`
	DisplayName          *string                `json:"display_name,omitempty"`
	AvatarURL            *string                `json:"avatar_url,omitempty"`
	Role                 *AgentRole             `json:"role,omitempty"`
	Status               *AgentStatus           `json:"status,omitempty"`
	MaxConcurrentTickets *int                   `json:"max_concurrent_tickets,omitempty"`
	Skills               []string               `json:"skills,omitempty"`
	IsActive             *bool                  `json:"is_active,omitempty"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
}

// TeamFilter defines filters for listing teams.
type TeamFilter struct {
	TenantID uuid.UUID
	IsActive *bool
	Limit    int
	Offset   int
}

// AgentFilter defines filters for listing agents.
type AgentFilter struct {
	TenantID uuid.UUID
	TeamID   *uuid.UUID
	Status   *AgentStatus
	Role     *AgentRole
	IsActive *bool
	Limit    int
	Offset   int
}
