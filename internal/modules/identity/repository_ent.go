package identity

import (
	"context"
	"time"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/agent"
	"github.com/bengobox/ticketing-service/internal/ent/team"
	"github.com/google/uuid"
)

// EntRepository implements Repository using Ent ORM.
type EntRepository struct {
	client *ent.Client
}

// NewEntRepository creates a new EntRepository.
func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client}
}

// Team operations

func (r *EntRepository) CreateTeam(ctx context.Context, tenantID uuid.UUID, t *Team) error {
	created, err := r.client.Team.Create().
		SetID(t.ID).
		SetTenantID(tenantID).
		SetName(t.Name).
		SetNillableDescription(&t.Description).
		SetIsActive(true).
		SetMetadata(t.Metadata).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ErrTeamExists
		}
		return err
	}
	t.ID = created.ID
	t.CreatedAt = created.CreatedAt
	t.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *EntRepository) GetTeam(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID) (*Team, error) {
	t, err := r.client.Team.Query().
		Where(
			team.ID(teamID),
			team.TenantID(tenantID),
			team.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTeamNotFound
		}
		return nil, err
	}
	return entTeamToModel(t), nil
}

func (r *EntRepository) GetTeamByName(ctx context.Context, tenantID uuid.UUID, name string) (*Team, error) {
	t, err := r.client.Team.Query().
		Where(
			team.TenantID(tenantID),
			team.Name(name),
			team.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTeamNotFound
		}
		return nil, err
	}
	return entTeamToModel(t), nil
}

func (r *EntRepository) ListTeams(ctx context.Context, tenantID uuid.UUID, filter TeamFilter) ([]*Team, int, error) {
	query := r.client.Team.Query().
		Where(
			team.TenantID(tenantID),
			team.DeletedAtIsNil(),
		)

	if filter.IsActive != nil {
		query = query.Where(team.IsActive(*filter.IsActive))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	teams, err := query.Order(team.ByName()).All(ctx)
	if err != nil {
		return nil, 0, err
	}

	result := make([]*Team, len(teams))
	for i, t := range teams {
		result[i] = entTeamToModel(t)
	}
	return result, total, nil
}

func (r *EntRepository) UpdateTeam(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID, updates *UpdateTeamRequest) error {
	update := r.client.Team.Update().
		Where(
			team.ID(teamID),
			team.TenantID(tenantID),
			team.DeletedAtIsNil(),
		)

	if updates.Name != nil {
		update = update.SetName(*updates.Name)
	}
	if updates.Description != nil {
		update = update.SetDescription(*updates.Description)
	}
	if updates.IsActive != nil {
		update = update.SetIsActive(*updates.IsActive)
	}
	if updates.Metadata != nil {
		update = update.SetMetadata(updates.Metadata)
	}

	n, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTeamNotFound
	}
	return nil
}

func (r *EntRepository) DeleteTeam(ctx context.Context, tenantID uuid.UUID, teamID uuid.UUID) error {
	now := time.Now()
	n, err := r.client.Team.Update().
		Where(
			team.ID(teamID),
			team.TenantID(tenantID),
			team.DeletedAtIsNil(),
		).
		SetDeletedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTeamNotFound
	}
	return nil
}

// Agent operations

func (r *EntRepository) CreateAgent(ctx context.Context, tenantID uuid.UUID, a *Agent) error {
	create := r.client.Agent.Create().
		SetID(a.ID).
		SetTenantID(tenantID).
		SetUserID(a.UserID).
		SetDisplayName(a.DisplayName).
		SetEmail(a.Email).
		SetRole(agent.Role(a.Role)).
		SetStatus(agent.StatusOffline).
		SetMaxConcurrentTickets(a.MaxConcurrentTickets).
		SetCurrentTicketCount(0).
		SetIsActive(true)

	if a.TeamID != nil {
		create = create.SetTeamID(*a.TeamID)
	}
	if a.AvatarURL != "" {
		create = create.SetAvatarURL(a.AvatarURL)
	}
	if a.Skills != nil {
		create = create.SetSkills(a.Skills)
	}
	if a.Metadata != nil {
		create = create.SetMetadata(a.Metadata)
	}

	created, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ErrAgentExists
		}
		return err
	}
	a.ID = created.ID
	a.Status = AgentStatus(created.Status)
	a.CreatedAt = created.CreatedAt
	a.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *EntRepository) GetAgent(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) (*Agent, error) {
	a, err := r.client.Agent.Query().
		Where(
			agent.ID(agentID),
			agent.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	return entAgentToModel(a), nil
}

func (r *EntRepository) GetAgentByUserID(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (*Agent, error) {
	a, err := r.client.Agent.Query().
		Where(
			agent.TenantID(tenantID),
			agent.UserID(userID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	return entAgentToModel(a), nil
}

func (r *EntRepository) ListAgents(ctx context.Context, tenantID uuid.UUID, filter AgentFilter) ([]*Agent, int, error) {
	query := r.client.Agent.Query().
		Where(agent.TenantID(tenantID))

	if filter.TeamID != nil {
		query = query.Where(agent.TeamID(*filter.TeamID))
	}
	if filter.Status != nil {
		query = query.Where(agent.StatusEQ(agent.Status(*filter.Status)))
	}
	if filter.Role != nil {
		query = query.Where(agent.RoleEQ(agent.Role(*filter.Role)))
	}
	if filter.IsActive != nil {
		query = query.Where(agent.IsActive(*filter.IsActive))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	agents, err := query.Order(agent.ByDisplayName()).All(ctx)
	if err != nil {
		return nil, 0, err
	}

	result := make([]*Agent, len(agents))
	for i, a := range agents {
		result[i] = entAgentToModel(a)
	}
	return result, total, nil
}

func (r *EntRepository) UpdateAgent(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID, updates *UpdateAgentRequest) error {
	update := r.client.Agent.Update().
		Where(
			agent.ID(agentID),
			agent.TenantID(tenantID),
		)

	if updates.TeamID != nil {
		update = update.SetTeamID(*updates.TeamID)
	}
	if updates.DisplayName != nil {
		update = update.SetDisplayName(*updates.DisplayName)
	}
	if updates.AvatarURL != nil {
		update = update.SetAvatarURL(*updates.AvatarURL)
	}
	if updates.Role != nil {
		update = update.SetRole(agent.Role(*updates.Role))
	}
	if updates.Status != nil {
		update = update.SetStatus(agent.Status(*updates.Status))
	}
	if updates.MaxConcurrentTickets != nil {
		update = update.SetMaxConcurrentTickets(*updates.MaxConcurrentTickets)
	}
	if updates.Skills != nil {
		update = update.SetSkills(updates.Skills)
	}
	if updates.IsActive != nil {
		update = update.SetIsActive(*updates.IsActive)
	}
	if updates.Metadata != nil {
		update = update.SetMetadata(updates.Metadata)
	}

	n, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAgentNotFound
	}
	return nil
}

func (r *EntRepository) UpdateAgentStatus(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID, status AgentStatus) error {
	now := time.Now()
	n, err := r.client.Agent.Update().
		Where(
			agent.ID(agentID),
			agent.TenantID(tenantID),
		).
		SetStatus(agent.Status(status)).
		SetLastActiveAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAgentNotFound
	}
	return nil
}

func (r *EntRepository) UpdateAgentTicketCount(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID, delta int) error {
	n, err := r.client.Agent.Update().
		Where(
			agent.ID(agentID),
			agent.TenantID(tenantID),
		).
		AddCurrentTicketCount(delta).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAgentNotFound
	}
	return nil
}

func (r *EntRepository) DeleteAgent(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) error {
	n, err := r.client.Agent.Update().
		Where(
			agent.ID(agentID),
			agent.TenantID(tenantID),
		).
		SetIsActive(false).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAgentNotFound
	}
	return nil
}

// Availability

func (r *EntRepository) GetAvailableAgents(ctx context.Context, tenantID uuid.UUID, teamID *uuid.UUID) ([]*Agent, error) {
	query := r.client.Agent.Query().
		Where(
			agent.TenantID(tenantID),
			agent.IsActive(true),
			agent.StatusEQ(agent.StatusAvailable),
		)

	if teamID != nil {
		query = query.Where(agent.TeamID(*teamID))
	}

	agents, err := query.All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*Agent, len(agents))
	for i, a := range agents {
		result[i] = entAgentToModel(a)
	}
	return result, nil
}

func (r *EntRepository) GetLeastLoadedAgent(ctx context.Context, tenantID uuid.UUID, teamID *uuid.UUID, skills []string) (*Agent, error) {
	query := r.client.Agent.Query().
		Where(
			agent.TenantID(tenantID),
			agent.IsActive(true),
			agent.StatusIn(agent.StatusAvailable, agent.StatusBusy),
		)

	if teamID != nil {
		query = query.Where(agent.TeamID(*teamID))
	}

	// Get agent with lowest current ticket count that's below max capacity
	a, err := query.
		Order(agent.ByCurrentTicketCount()).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}

	// Check if agent is at capacity
	if a.CurrentTicketCount >= a.MaxConcurrentTickets {
		return nil, ErrAgentAtCapacity
	}

	return entAgentToModel(a), nil
}

// Converters

func entTeamToModel(t *ent.Team) *Team {
	return &Team{
		ID:          t.ID,
		TenantID:    t.TenantID,
		Name:        t.Name,
		Description: t.Description,
		IsActive:    t.IsActive,
		Metadata:    t.Metadata,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

func entAgentToModel(a *ent.Agent) *Agent {
	return &Agent{
		ID:                   a.ID,
		TenantID:             a.TenantID,
		UserID:               a.UserID,
		TeamID:               a.TeamID,
		DisplayName:          a.DisplayName,
		Email:                a.Email,
		AvatarURL:            a.AvatarURL,
		Role:                 AgentRole(a.Role),
		Status:               AgentStatus(a.Status),
		MaxConcurrentTickets: a.MaxConcurrentTickets,
		CurrentTicketCount:   a.CurrentTicketCount,
		Skills:               a.Skills,
		Metadata:             a.Metadata,
		IsActive:             a.IsActive,
		LastActiveAt:         a.LastActiveAt,
		CreatedAt:            a.CreatedAt,
		UpdatedAt:            a.UpdatedAt,
	}
}
