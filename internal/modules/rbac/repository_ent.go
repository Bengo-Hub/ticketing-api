package rbac

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	entagent "github.com/bengobox/ticketing-service/internal/ent/agent"
	"github.com/bengobox/ticketing-service/internal/ent/rolepermission"
	"github.com/bengobox/ticketing-service/internal/ent/ticketingpermission"
	"github.com/bengobox/ticketing-service/internal/ent/ticketingrole"
	"github.com/bengobox/ticketing-service/internal/ent/ticketinguser"
	"github.com/bengobox/ticketing-service/internal/ent/userroleassignment"
)

// EntRepository implements Repository with Ent.
type EntRepository struct{ client *ent.Client }

// NewEntRepository creates the Ent-backed RBAC repository.
func NewEntRepository(client *ent.Client) *EntRepository { return &EntRepository{client: client} }

var _ Repository = (*EntRepository)(nil)

func (r *EntRepository) SeedCatalogue(ctx context.Context, perms []Permission) error {
	for _, p := range perms {
		err := r.client.TicketingPermission.Create().
			SetPermissionCode(p.Code).SetModule(p.Module).SetAction(p.Action).SetDescription(p.Description).
			OnConflictColumns(ticketingpermission.FieldPermissionCode).
			UpdateModule().UpdateAction().UpdateDescription().
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("seed permission %s: %w", p.Code, err)
		}
	}
	return nil
}

func (r *EntRepository) EnsureTenantRoles(ctx context.Context, tenantID uuid.UUID, roles []RoleDef) error {
	perms, err := r.client.TicketingPermission.Query().All(ctx)
	if err != nil {
		return err
	}
	permIDs := make(map[string]uuid.UUID, len(perms))
	for _, p := range perms {
		permIDs[p.PermissionCode] = p.ID
	}
	for _, def := range roles {
		roleID, err := r.client.TicketingRole.Create().
			SetTenantID(tenantID).SetRoleCode(def.Code).SetName(def.Name).SetDescription(def.Description).
			SetIsSystemRole(true).
			OnConflictColumns(ticketingrole.FieldTenantID, ticketingrole.FieldRoleCode).
			UpdateName().UpdateDescription().UpdateIsSystemRole().
			ID(ctx)
		if err != nil {
			return fmt.Errorf("ensure role %s: %w", def.Code, err)
		}
		if _, err := r.client.RolePermission.Delete().Where(
			rolepermission.RoleID(roleID),
			rolepermission.Not(rolepermission.HasPermissionWith(ticketingpermission.PermissionCodeIn(def.Permissions...))),
		).Exec(ctx); err != nil {
			return fmt.Errorf("prune role %s permissions: %w", def.Code, err)
		}
		bulk := make([]*ent.RolePermissionCreate, 0, len(def.Permissions))
		for _, code := range def.Permissions {
			if pid, ok := permIDs[code]; ok {
				bulk = append(bulk, r.client.RolePermission.Create().SetRoleID(roleID).SetPermissionID(pid))
			}
		}
		if len(bulk) == 0 {
			continue
		}
		// Two pods seeding the same tenant together both insert; the (role, permission) key keeps one row.
		if err := r.client.RolePermission.CreateBulk(bulk...).
			OnConflictColumns(rolepermission.FieldRoleID, rolepermission.FieldPermissionID).
			DoNothing().Exec(ctx); err != nil {
			return fmt.Errorf("grant role %s permissions: %w", def.Code, err)
		}
	}
	return nil
}

func (r *EntRepository) UpsertUser(ctx context.Context, id Identity) (uuid.UUID, error) {
	now := time.Now()
	create := r.client.TicketingUser.Create().
		SetTenantID(id.TenantID).SetAuthServiceUserID(id.AuthUserID).
		SetSyncStatus("synced").SetLastSyncAt(now)
	if id.Email != "" {
		create.SetEmail(id.Email)
	}
	if id.Name != "" {
		create.SetName(id.Name)
	}
	return create.
		OnConflictColumns(ticketinguser.FieldTenantID, ticketinguser.FieldAuthServiceUserID).
		Update(func(u *ent.TicketingUserUpsert) {
			u.UpdateLastSyncAt()
			u.UpdateSyncStatus()
			if id.Email != "" {
				u.UpdateEmail()
			}
			if id.Name != "" {
				u.UpdateName()
			}
		}).
		ID(ctx)
}

func (r *EntRepository) FindUser(ctx context.Context, tenantID, authUserID uuid.UUID) (*User, error) {
	u, err := r.client.TicketingUser.Query().
		Where(ticketinguser.TenantID(tenantID), ticketinguser.AuthServiceUserID(authUserID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toUser(u), nil
}

func (r *EntRepository) GetUser(ctx context.Context, tenantID, userID uuid.UUID) (*User, error) {
	u, err := r.client.TicketingUser.Query().
		Where(ticketinguser.TenantID(tenantID), ticketinguser.ID(userID)).
		WithRoleAssignments(func(q *ent.UserRoleAssignmentQuery) { q.WithRole() }).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toUser(u), nil
}

func (r *EntRepository) ListUsers(ctx context.Context, tenantID uuid.UUID, limit int) ([]User, error) {
	rows, err := r.client.TicketingUser.Query().
		Where(ticketinguser.TenantID(tenantID)).
		WithRoleAssignments(func(q *ent.UserRoleAssignmentQuery) { q.WithRole() }).
		Order(ent.Asc(ticketinguser.FieldEmail)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(rows))
	for _, u := range rows {
		out = append(out, *toUser(u))
	}
	return out, nil
}

func (r *EntRepository) HasAssignments(ctx context.Context, tenantID, userID uuid.UUID) (bool, error) {
	return r.client.UserRoleAssignment.Query().
		Where(userroleassignment.TenantID(tenantID), userroleassignment.UserID(userID)).
		Exist(ctx)
}

// tenantRoleIDs returns the tenant's role ids for the given codes; unknown codes are an error.
func tenantRoleIDs(ctx context.Context, c *ent.Client, tenantID uuid.UUID, codes []string) ([]uuid.UUID, error) {
	rows, err := c.TicketingRole.Query().
		Where(ticketingrole.TenantID(tenantID), ticketingrole.RoleCodeIn(codes...)).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(uniqueStrings(codes)) {
		return nil, ErrUnknownRole
	}
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids, nil
}

func (r *EntRepository) SetRoles(ctx context.Context, tenantID, userID, assignedBy uuid.UUID, roleCodes []string) error {
	ids, err := tenantRoleIDs(ctx, r.client, tenantID, roleCodes)
	if err != nil {
		return err
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.UserRoleAssignment.Delete().
		Where(userroleassignment.TenantID(tenantID), userroleassignment.UserID(userID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, id := range ids {
		if err := tx.UserRoleAssignment.Create().
			SetTenantID(tenantID).SetUserID(userID).SetRoleID(id).SetAssignedBy(assignedBy).Exec(ctx); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (r *EntRepository) AddRole(ctx context.Context, tenantID, userID, assignedBy uuid.UUID, roleCode string) error {
	ids, err := tenantRoleIDs(ctx, r.client, tenantID, []string{roleCode})
	if err != nil {
		return err
	}
	return r.client.UserRoleAssignment.Create().
		SetTenantID(tenantID).SetUserID(userID).SetRoleID(ids[0]).SetAssignedBy(assignedBy).
		OnConflictColumns(userroleassignment.FieldTenantID, userroleassignment.FieldUserID, userroleassignment.FieldRoleID).
		DoNothing().
		Exec(ctx)
}

func (r *EntRepository) Grants(ctx context.Context, tenantID, userID, authUserID uuid.UUID, now time.Time) (Grants, error) {
	assignments, err := r.client.UserRoleAssignment.Query().
		Where(userroleassignment.TenantID(tenantID), userroleassignment.UserID(userID),
			userroleassignment.Or(userroleassignment.ExpiresAtIsNil(), userroleassignment.ExpiresAtGT(now))).
		WithRole(func(q *ent.TicketingRoleQuery) { q.WithPermissions() }).
		All(ctx)
	if err != nil {
		return Grants{}, err
	}
	roles, perms := map[string]bool{}, map[string]bool{}
	for _, a := range assignments {
		if a.Edges.Role == nil {
			continue
		}
		roles[a.Edges.Role.RoleCode] = true
		for _, p := range a.Edges.Role.Edges.Permissions {
			perms[p.PermissionCode] = true
		}
	}
	g := Grants{Roles: sortedKeys(roles), Perms: sortedKeys(perms)}
	agentID, err := r.client.Agent.Query().
		Where(entagent.TenantID(tenantID), entagent.UserID(authUserID), entagent.IsActive(true)).
		OnlyID(ctx)
	switch {
	case err == nil:
		g.AgentID = &agentID
	case !ent.IsNotFound(err):
		return Grants{}, err
	}
	return g, nil
}

func (r *EntRepository) ListRoles(ctx context.Context, tenantID uuid.UUID) ([]RoleView, error) {
	rows, err := r.client.TicketingRole.Query().
		Where(ticketingrole.TenantID(tenantID)).
		WithPermissions().
		Order(ent.Asc(ticketingrole.FieldRoleCode)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RoleView, 0, len(rows))
	for _, row := range rows {
		v := RoleView{ID: row.ID, Code: row.RoleCode, Name: row.Name, Description: row.Description,
			System: row.IsSystemRole, Permissions: []string{}}
		for _, p := range row.Edges.Permissions {
			v.Permissions = append(v.Permissions, p.PermissionCode)
		}
		out = append(out, v)
	}
	return out, nil
}

func (r *EntRepository) EnsureAgent(ctx context.Context, in AgentInput) error {
	existing, err := r.client.Agent.Query().
		Where(entagent.TenantID(in.TenantID), entagent.UserID(in.AuthUserID)).Only(ctx)
	if err == nil {
		if agentRank[in.Role] > agentRank[string(existing.Role)] || !existing.IsActive {
			return existing.Update().SetRole(entagent.Role(in.Role)).SetIsActive(true).Exec(ctx)
		}
		return nil
	}
	if !ent.IsNotFound(err) {
		return err
	}
	display := in.Name
	if display == "" {
		display = in.Email
	}
	if display == "" {
		display = "Agent"
	}
	err = r.client.Agent.Create().
		SetTenantID(in.TenantID).SetUserID(in.AuthUserID).
		SetEmail(in.Email).SetDisplayName(display).
		SetRole(entagent.Role(in.Role)).
		Exec(ctx)
	// Another pod created it between the read and the insert; the (tenant_id, user_id) key holds.
	if ent.IsConstraintError(err) {
		return nil
	}
	return err
}

func (r *EntRepository) UpdateAgentContact(ctx context.Context, tenantID, authUserID uuid.UUID, email, name string) error {
	u := r.client.Agent.Update().Where(entagent.TenantID(tenantID), entagent.UserID(authUserID))
	if email != "" {
		u.SetEmail(email)
	}
	if name != "" {
		u.SetDisplayName(name)
	}
	_, err := u.Save(ctx)
	return err
}

var agentRank = map[string]int{"agent": 1, "supervisor": 2, "admin": 3}

func toUser(u *ent.TicketingUser) *User {
	out := &User{
		ID: u.ID, TenantID: u.TenantID, AuthServiceUserID: u.AuthServiceUserID,
		Email: u.Email, Name: u.Name, SyncStatus: u.SyncStatus, LastSyncAt: u.LastSyncAt, Roles: []string{},
	}
	for _, a := range u.Edges.RoleAssignments {
		if a.Edges.Role != nil {
			out.Roles = append(out.Roles, a.Edges.Role.RoleCode)
		}
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
