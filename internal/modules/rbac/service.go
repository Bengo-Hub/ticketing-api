package rbac

import (
	"context"
	"errors"
	"fmt"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// grantsTTL bounds how long another pod serves a changed role; this pod drops its copy at once.
	grantsTTL = 60 * time.Second
	// syncTTL throttles the just-in-time user write to once per user per pod in this window.
	syncTTL = 5 * time.Minute
	// tenantRolesTTL throttles the per-tenant system role check.
	tenantRolesTTL = 30 * time.Minute
)

// Service seeds the catalogue, provisions users just in time, resolves their grants and runs role
// administration.
type Service struct {
	repo   Repository
	log    *zap.Logger
	now    func() time.Time
	grants *sharedcache.Local[string, Grants]
	synced *sharedcache.Local[string, uuid.UUID]
	tenant *sharedcache.Local[uuid.UUID, bool]
}

// NewService creates the RBAC service.
func NewService(repo Repository, log *zap.Logger) *Service {
	return &Service{
		repo:   repo,
		log:    log.Named("rbac"),
		now:    time.Now,
		grants: sharedcache.NewLocal[string, Grants](20000, grantsTTL),
		synced: sharedcache.NewLocal[string, uuid.UUID](20000, syncTTL),
		tenant: sharedcache.NewLocal[uuid.UUID, bool](5000, tenantRolesTTL),
	}
}

// Seed upserts the permission catalogue. Idempotent; run at startup.
func (s *Service) Seed(ctx context.Context) error {
	return s.repo.SeedCatalogue(ctx, Catalogue())
}

func userKey(tenantID, authUserID uuid.UUID) string {
	return tenantID.String() + ":" + authUserID.String()
}

// ensureTenantRoles seeds the tenant's system roles once per pod per tenantRolesTTL.
func (s *Service) ensureTenantRoles(ctx context.Context, tenantID uuid.UUID) error {
	if _, ok := s.tenant.Get(tenantID); ok {
		return nil
	}
	if err := s.repo.EnsureTenantRoles(ctx, tenantID, SystemRoles); err != nil {
		return err
	}
	s.tenant.Set(tenantID, true)
	return nil
}

// addDefaultRole assigns the sign-in default. A system role deleted on another pod since this pod
// last seeded the tenant is seeded again and the grant retried once.
func (s *Service) addDefaultRole(ctx context.Context, id Identity, local uuid.UUID, role string) error {
	err := s.repo.AddRole(ctx, id.TenantID, local, id.AuthUserID, role)
	if !errors.Is(err, ErrUnknownRole) {
		return err
	}
	s.tenant.Delete(id.TenantID)
	if err := s.ensureTenantRoles(ctx, id.TenantID); err != nil {
		return err
	}
	return s.repo.AddRole(ctx, id.TenantID, local, id.AuthUserID, role)
}

// EnsureUser creates or refreshes the tenant's ticketing user from the token. A user who never
// held a role gets the role mapped from their global roles (MapGlobalRoles), and a staff role gets
// an agent profile. A user whose role an admin ended keeps the ended row, so sign-in never adds it
// back. It writes at most once per user per pod every syncTTL and returns the local id.
func (s *Service) EnsureUser(ctx context.Context, id Identity) (uuid.UUID, error) {
	key := userKey(id.TenantID, id.AuthUserID)
	if local, ok := s.synced.Get(key); ok {
		return local, nil
	}
	if err := s.ensureTenantRoles(ctx, id.TenantID); err != nil {
		return uuid.Nil, fmt.Errorf("ensure tenant roles: %w", err)
	}
	local, err := s.repo.UpsertUser(ctx, id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert ticketing user: %w", err)
	}
	has, err := s.repo.HasAssignments(ctx, id.TenantID, local)
	if err != nil {
		return uuid.Nil, err
	}
	if !has {
		role := MapGlobalRoles(id.GlobalRoles)
		if err := s.addDefaultRole(ctx, id, local, role); err != nil {
			return uuid.Nil, fmt.Errorf("assign default role %s: %w", role, err)
		}
		s.grants.Delete(key)
	}
	g, err := s.grantsFor(ctx, id.TenantID, local, id.AuthUserID)
	if err != nil {
		return uuid.Nil, err
	}
	if role := AgentRole(g.Roles); role != "" {
		if err := s.repo.EnsureAgent(ctx, AgentInput{
			TenantID: id.TenantID, AuthUserID: id.AuthUserID, Email: id.Email, Name: id.Name, Role: role,
		}); err != nil {
			return uuid.Nil, fmt.Errorf("ensure agent profile: %w", err)
		}
		if g.AgentID == nil {
			s.grants.Delete(key)
		}
	}
	s.synced.Set(key, local)
	return local, nil
}

// SyncFromEvent applies an auth.user.created or auth.user.updated event: the ticketing user is
// created or refreshed with the same rules as sign-in, and an existing agent profile takes the
// new email and name.
func (s *Service) SyncFromEvent(ctx context.Context, id Identity) error {
	s.synced.Delete(userKey(id.TenantID, id.AuthUserID))
	if _, err := s.EnsureUser(ctx, id); err != nil {
		return err
	}
	return s.repo.UpdateAgentContact(ctx, id.TenantID, id.AuthUserID, id.Email, id.Name)
}

// Grants returns the user's roles, permissions and agent profile in the tenant, cached briefly.
func (s *Service) Grants(ctx context.Context, tenantID, localUserID, authUserID uuid.UUID) (Grants, error) {
	return s.grantsFor(ctx, tenantID, localUserID, authUserID)
}

func (s *Service) grantsFor(ctx context.Context, tenantID, localUserID, authUserID uuid.UUID) (Grants, error) {
	key := userKey(tenantID, authUserID)
	if g, ok := s.grants.Get(key); ok {
		return g, nil
	}
	g, err := s.repo.Grants(ctx, tenantID, localUserID, authUserID, s.now())
	if err != nil {
		return Grants{}, err
	}
	s.grants.Set(key, g)
	return g, nil
}

// HasPermission reports whether a user holds a permission code exactly in the tenant.
func (s *Service) HasPermission(ctx context.Context, tenantID, authUserID uuid.UUID, code string) bool {
	u, err := s.repo.FindUser(ctx, tenantID, authUserID)
	if err != nil || u == nil {
		return false
	}
	g, err := s.grantsFor(ctx, tenantID, u.ID, authUserID)
	if err != nil {
		s.log.Warn("permission resolve failed", zap.Error(err))
		return false
	}
	for _, p := range g.Perms {
		if p == code {
			return true
		}
	}
	return false
}

// FindUser returns the caller's ticketing user, or nil.
func (s *Service) FindUser(ctx context.Context, tenantID, authUserID uuid.UUID) (*User, error) {
	return s.repo.FindUser(ctx, tenantID, authUserID)
}

// forgetGrants drops this pod's cached grants after a role or assignment changed. Other pods
// catch up within grantsTTL.
func (s *Service) forgetGrants() { s.grants.Purge() }
