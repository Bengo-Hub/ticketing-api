package identity

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service provides identity business logic.
type Service struct {
	repo   Repository
	logger *zap.Logger
}

// NewService creates a new identity service.
func NewService(repo Repository, logger *zap.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: logger.Named("identity"),
	}
}

// Team operations

// CreateTeam creates a new team.
func (s *Service) CreateTeam(ctx context.Context, req *CreateTeamRequest) (*Team, error) {
	team := &Team{
		ID:          uuid.New(),
		TenantID:    req.TenantID,
		Name:        req.Name,
		Description: req.Description,
		IsActive:    true,
		Metadata:    req.Metadata,
	}

	if err := s.repo.CreateTeam(ctx, req.TenantID, team); err != nil {
		s.logger.Error("failed to create team",
			zap.Error(err),
			zap.String("name", req.Name),
		)
		return nil, err
	}

	s.logger.Info("team created",
		zap.String("team_id", team.ID.String()),
		zap.String("name", team.Name),
	)

	return team, nil
}

// GetTeam retrieves a team by ID.
func (s *Service) GetTeam(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID) (*Team, error) {
	return s.repo.GetTeam(ctx, tenantID, teamID)
}

// ListTeams lists teams with optional filtering.
func (s *Service) ListTeams(ctx context.Context, tenantID uuid.UUID, filter TeamFilter) ([]*Team, int, error) {
	filter.TenantID = tenantID
	return s.repo.ListTeams(ctx, tenantID, filter)
}

// UpdateTeam updates a team.
func (s *Service) UpdateTeam(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID, req *UpdateTeamRequest) error {
	if err := s.repo.UpdateTeam(ctx, tenantID, teamID, req); err != nil {
		s.logger.Error("failed to update team",
			zap.Error(err),
			zap.String("team_id", teamID.String()),
		)
		return err
	}
	return nil
}

// DeleteTeam soft deletes a team.
func (s *Service) DeleteTeam(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID) error {
	if err := s.repo.DeleteTeam(ctx, tenantID, teamID); err != nil {
		s.logger.Error("failed to delete team",
			zap.Error(err),
			zap.String("team_id", teamID.String()),
		)
		return err
	}

	s.logger.Info("team deleted",
		zap.String("team_id", teamID.String()),
	)

	return nil
}

// Agent operations

// CreateAgent creates a new agent.
func (s *Service) CreateAgent(ctx context.Context, req *CreateAgentRequest) (*Agent, error) {
	if req.MaxConcurrentTickets == 0 {
		req.MaxConcurrentTickets = 10 // default
	}

	if req.Role == "" {
		req.Role = RoleAgent
	}

	agent := &Agent{
		ID:                   uuid.New(),
		TenantID:             req.TenantID,
		UserID:               req.UserID,
		TeamID:               req.TeamID,
		DisplayName:          req.DisplayName,
		Email:                req.Email,
		AvatarURL:            req.AvatarURL,
		Role:                 req.Role,
		Status:               StatusOffline,
		MaxConcurrentTickets: req.MaxConcurrentTickets,
		CurrentTicketCount:   0,
		Skills:               req.Skills,
		Metadata:             req.Metadata,
		IsActive:             true,
	}

	if err := s.repo.CreateAgent(ctx, req.TenantID, agent); err != nil {
		s.logger.Error("failed to create agent",
			zap.Error(err),
			zap.String("user_id", req.UserID.String()),
		)
		return nil, err
	}

	s.logger.Info("agent created",
		zap.String("agent_id", agent.ID.String()),
		zap.String("display_name", agent.DisplayName),
	)

	return agent, nil
}

// GetAgent retrieves an agent by ID.
func (s *Service) GetAgent(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) (*Agent, error) {
	return s.repo.GetAgent(ctx, tenantID, agentID)
}

// GetAgentByUserID retrieves an agent by user ID.
func (s *Service) GetAgentByUserID(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (*Agent, error) {
	return s.repo.GetAgentByUserID(ctx, tenantID, userID)
}

// ListAgents lists agents with optional filtering.
func (s *Service) ListAgents(ctx context.Context, tenantID uuid.UUID, filter AgentFilter) ([]*Agent, int, error) {
	filter.TenantID = tenantID
	return s.repo.ListAgents(ctx, tenantID, filter)
}

// UpdateAgent updates an agent.
func (s *Service) UpdateAgent(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID, req *UpdateAgentRequest) error {
	if err := s.repo.UpdateAgent(ctx, tenantID, agentID, req); err != nil {
		s.logger.Error("failed to update agent",
			zap.Error(err),
			zap.String("agent_id", agentID.String()),
		)
		return err
	}
	return nil
}

// UpdateAgentStatus updates an agent's availability status.
func (s *Service) UpdateAgentStatus(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID, status AgentStatus) error {
	if err := s.repo.UpdateAgentStatus(ctx, tenantID, agentID, status); err != nil {
		s.logger.Error("failed to update agent status",
			zap.Error(err),
			zap.String("agent_id", agentID.String()),
		)
		return err
	}

	s.logger.Info("agent status updated",
		zap.String("agent_id", agentID.String()),
		zap.String("status", string(status)),
	)

	return nil
}

// DeleteAgent deactivates an agent.
func (s *Service) DeleteAgent(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) error {
	if err := s.repo.DeleteAgent(ctx, tenantID, agentID); err != nil {
		s.logger.Error("failed to delete agent",
			zap.Error(err),
			zap.String("agent_id", agentID.String()),
		)
		return err
	}

	s.logger.Info("agent deactivated",
		zap.String("agent_id", agentID.String()),
	)

	return nil
}

// Availability operations

// GetAvailableAgents returns agents available for assignment.
func (s *Service) GetAvailableAgents(ctx context.Context, tenantID uuid.UUID, teamID *uuid.UUID, limit int) ([]*Agent, error) {
	return s.repo.GetAvailableAgents(ctx, tenantID, teamID, limit)
}

// GetLeastLoadedAgent returns the agent with lowest ticket count.
func (s *Service) GetLeastLoadedAgent(ctx context.Context, tenantID uuid.UUID, teamID *uuid.UUID, skills []string) (*Agent, error) {
	return s.repo.GetLeastLoadedAgent(ctx, tenantID, teamID, skills)
}

// IncrementAgentTicketCount increments agent's current ticket count.
func (s *Service) IncrementAgentTicketCount(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) error {
	return s.repo.UpdateAgentTicketCount(ctx, tenantID, agentID, 1)
}

// DecrementAgentTicketCount decrements agent's current ticket count.
func (s *Service) DecrementAgentTicketCount(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) error {
	return s.repo.UpdateAgentTicketCount(ctx, tenantID, agentID, -1)
}
