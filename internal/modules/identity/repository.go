package identity

import (
	"context"

	"github.com/google/uuid"
)

// Repository abstracts persistence for identity entities.
type Repository interface {
	// Team operations
	CreateTeam(ctx context.Context, tenantID uuid.UUID, team *Team) error
	GetTeam(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID) (*Team, error)
	GetTeamByName(ctx context.Context, tenantID uuid.UUID, name string) (*Team, error)
	// ListTeams returns up to filter.Limit+1 teams after the cursor, newest first.
	ListTeams(ctx context.Context, tenantID uuid.UUID, filter TeamFilter) ([]*Team, error)
	UpdateTeam(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID, updates *UpdateTeamRequest) error
	DeleteTeam(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID) error

	// Agent operations
	CreateAgent(ctx context.Context, tenantID uuid.UUID, agent *Agent) error
	GetAgent(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) (*Agent, error)
	GetAgentByUserID(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (*Agent, error)
	// ListAgents returns up to filter.Limit+1 agents after the cursor, newest first.
	ListAgents(ctx context.Context, tenantID uuid.UUID, filter AgentFilter) ([]*Agent, error)
	UpdateAgent(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID, updates *UpdateAgentRequest) error
	UpdateAgentStatus(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID, status AgentStatus) error
	UpdateAgentTicketCount(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID, delta int) error
	DeleteAgent(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) error

	// Availability
	GetAvailableAgents(ctx context.Context, tenantID uuid.UUID, teamID *uuid.UUID, limit int) ([]*Agent, error)
	GetLeastLoadedAgent(ctx context.Context, tenantID uuid.UUID, teamID *uuid.UUID, skills []string) (*Agent, error)
}
