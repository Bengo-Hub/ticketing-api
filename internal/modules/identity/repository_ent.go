package identity

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/bengobox/ticketing-service/internal/ent"
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

func (r *EntRepository) ListTeams(ctx context.Context, tenantID uuid.UUID, filter TeamFilter) ([]*Team, error) {
	query := r.client.Team.Query().
		Where(
			team.TenantID(tenantID),
			team.DeletedAtIsNil(),
		)

	if filter.IsActive != nil {
		query = query.Where(team.IsActive(*filter.IsActive))
	}
	if c := filter.After; c != nil {
		query = query.Where(func(s *entsql.Selector) { s.Where(beforeRow(s, c)) })
	}

	teams, err := query.
		Order(ent.Desc(team.FieldCreatedAt), ent.Desc(team.FieldID)).
		Limit(filter.Limit + 1).
		All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*Team, len(teams))
	for i, t := range teams {
		result[i] = entTeamToModel(t)
	}
	return result, nil
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
