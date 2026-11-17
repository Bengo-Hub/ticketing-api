package rbac

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ListUsersWithRoles pages the tenant's people with the roles they hold or held.
func (s *Service) ListUsersWithRoles(ctx context.Context, tenantID uuid.UUID, search string, limit, offset int) ([]UserView, int, error) {
	users, total, err := s.repo.ListUsers(ctx, tenantID, search, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	views := make([]UserView, len(users))
	if len(users) == 0 {
		return views, total, nil
	}
	userIDs := make([]uuid.UUID, len(users))
	for i, u := range users {
		userIDs[i] = u.ID
	}
	now := s.now()
	assignments, err := s.repo.ListAssignments(ctx, tenantID, AssignmentFilters{UserIDs: userIDs}, now)
	if err != nil {
		return nil, 0, err
	}
	byUser := make(map[uuid.UUID][]UserRoleView, len(users))
	for _, a := range assignments {
		byUser[a.UserID] = append(byUser[a.UserID], UserRoleView{
			AssignmentID: a.ID,
			RoleID:       a.RoleID,
			RoleCode:     a.RoleCode,
			Name:         a.RoleName,
			AssignedAt:   a.AssignedAt,
			ExpiresAt:    a.ExpiresAt,
			Expired:      a.ExpiresAt != nil && !a.ExpiresAt.After(now),
		})
	}
	for i, u := range users {
		held := byUser[u.ID]
		if held == nil {
			held = []UserRoleView{}
		}
		views[i] = UserView{User: u, Roles: held}
	}
	return views, total, nil
}

// GetUser reads a tenant's user by local id or auth-api user id.
func (s *Service) GetUser(ctx context.Context, tenantID, id uuid.UUID) (*User, error) {
	u, err := s.repo.GetUser(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	return u, nil
}

// GetUserRoles returns the roles a user holds now.
func (s *Service) GetUserRoles(ctx context.Context, tenantID, id uuid.UUID) ([]Role, error) {
	roles, err := s.repo.UserRoles(ctx, tenantID, id, s.now())
	if roles == nil && err == nil {
		roles = []Role{}
	}
	return roles, err
}

// AddMember records a person auth-api has just made a member of the tenant, so they show in the
// team list before their first sign-in. Their ticketing role comes from sign-in or an admin grant.
func (s *Service) AddMember(ctx context.Context, tenantID, authUserID uuid.UUID, email string) (*User, error) {
	if err := s.ensureTenantRoles(ctx, tenantID); err != nil {
		return nil, err
	}
	local, err := s.repo.UpsertUser(ctx, Identity{TenantID: tenantID, AuthUserID: authUserID, Email: email})
	if err != nil {
		return nil, fmt.Errorf("record member: %w", err)
	}
	return s.GetUser(ctx, tenantID, local)
}

// AssignRole grants or renews a role until expiresAt, or with no end when it is nil. A staff role
// gives the person an agent profile.
func (s *Service) AssignRole(ctx context.Context, tenantID, userID, roleID, assignedBy uuid.UUID, expiresAt *time.Time) (*Role, error) {
	if expiresAt != nil && !expiresAt.After(s.now()) {
		return nil, ErrInvalidExpiry
	}
	user, err := s.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	role, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveAssignment(ctx, &Assignment{
		TenantID:   tenantID,
		UserID:     user.ID,
		RoleID:     role.ID,
		AssignedBy: assignedBy,
		ExpiresAt:  expiresAt,
	}); err != nil {
		return nil, fmt.Errorf("assign role: %w", err)
	}
	if agentRole := AgentRole([]string{role.RoleCode}); agentRole != "" {
		if err := s.repo.EnsureAgent(ctx, AgentInput{
			TenantID: tenantID, AuthUserID: user.AuthServiceUserID, Email: user.Email, Name: user.Name, Role: agentRole,
		}); err != nil {
			return nil, fmt.Errorf("ensure agent profile: %w", err)
		}
	}
	s.grants.Delete(userKey(tenantID, user.AuthServiceUserID))
	return role, nil
}

// RevokeRole ends a person's role and keeps the ended row, so sign-in never gives it back. guard
// stops someone removing their own way to manage users.
func (s *Service) RevokeRole(ctx context.Context, tenantID uuid.UUID, guard *uuid.UUID, userID, roleID uuid.UUID) (*Role, error) {
	user, err := s.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	held, err := s.repo.ListAssignments(ctx, tenantID, AssignmentFilters{UserID: &user.ID, RoleID: &roleID, ActiveOnly: true}, s.now())
	if err != nil {
		return nil, err
	}
	if len(held) == 0 {
		return nil, ErrAssignmentMissing
	}
	role, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	if guard != nil && *guard == user.AuthServiceUserID {
		if lose, err := s.losesManage(ctx, tenantID, guard, role.ID, nil); err != nil {
			return nil, err
		} else if lose {
			return nil, ErrSelfLockout
		}
	}
	if err := s.repo.EndAssignment(ctx, tenantID, held[0].ID, s.now()); err != nil {
		return nil, err
	}
	s.grants.Delete(userKey(tenantID, user.AuthServiceUserID))
	// Without a staff role left the person stops being an agent (owner decision 2026-11-17): the
	// profile goes inactive so assignment lists drop them; their tickets stay for reassignment.
	if AgentRole([]string{role.RoleCode}) != "" {
		left, err := s.repo.ListAssignments(ctx, tenantID, AssignmentFilters{UserID: &user.ID, ActiveOnly: true}, s.now())
		if err != nil {
			return nil, err
		}
		codes := make([]string, 0, len(left))
		for _, a := range left {
			codes = append(codes, a.RoleCode)
		}
		if AgentRole(codes) == "" {
			if err := s.repo.DeactivateAgent(ctx, tenantID, user.AuthServiceUserID); err != nil {
				return nil, fmt.Errorf("deactivate agent profile: %w", err)
			}
		}
	}
	return role, nil
}

// Audit stores one audit log row.
func (s *Service) Audit(ctx context.Context, e AuditEntry) error {
	return s.repo.WriteAudit(ctx, e)
}

// ListAudit pages the tenant's audit log, newest first.
func (s *Service) ListAudit(ctx context.Context, tenantID uuid.UUID, f AuditFilters, limit, offset int) ([]AuditEntry, int, error) {
	return s.repo.ListAudit(ctx, tenantID, f, limit, offset)
}
