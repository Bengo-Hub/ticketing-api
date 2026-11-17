package rbac

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/auditlog"
	"github.com/bengobox/ticketing-service/internal/ent/rolepermission"
	"github.com/bengobox/ticketing-service/internal/ent/ticketingpermission"
	"github.com/bengobox/ticketing-service/internal/ent/ticketingrole"
	"github.com/bengobox/ticketing-service/internal/ent/userroleassignment"
)

// withTx runs fn in a transaction and rolls back when it fails.
func (r *EntRepository) withTx(ctx context.Context, fn func(tx *ent.Tx) error) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func createRolePermissions(ctx context.Context, tx *ent.Tx, roleID uuid.UUID, permissionIDs []uuid.UUID) error {
	if len(permissionIDs) == 0 {
		return nil
	}
	bulk := make([]*ent.RolePermissionCreate, len(permissionIDs))
	for i, id := range permissionIDs {
		bulk[i] = tx.RolePermission.Create().SetRoleID(roleID).SetPermissionID(id)
	}
	if err := tx.RolePermission.CreateBulk(bulk...).Exec(ctx); err != nil {
		return fmt.Errorf("add role permissions: %w", err)
	}
	return nil
}

func (r *EntRepository) ListRoles(ctx context.Context, tenantID uuid.UUID) ([]Role, error) {
	rows, err := r.client.TicketingRole.Query().
		Where(ticketingrole.TenantID(tenantID)).
		Order(ent.Asc(ticketingrole.FieldName), ent.Asc(ticketingrole.FieldRoleCode)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	out := make([]Role, len(rows))
	for i, row := range rows {
		out[i] = toRole(row)
	}
	return out, nil
}

func (r *EntRepository) GetRole(ctx context.Context, tenantID, roleID uuid.UUID) (*Role, error) {
	row, err := r.client.TicketingRole.Query().
		Where(ticketingrole.TenantID(tenantID), ticketingrole.ID(roleID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}
	role := toRole(row)
	return &role, nil
}

func (r *EntRepository) GetRoleByCode(ctx context.Context, tenantID uuid.UUID, code string) (*Role, error) {
	row, err := r.client.TicketingRole.Query().
		Where(ticketingrole.TenantID(tenantID), ticketingrole.RoleCode(code)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get role by code: %w", err)
	}
	role := toRole(row)
	return &role, nil
}

func (r *EntRepository) CreateRoleWithPermissions(ctx context.Context, role *Role, permissionIDs []uuid.UUID) error {
	if role.TenantID == nil {
		return fmt.Errorf("create role: tenant is required")
	}
	return r.withTx(ctx, func(tx *ent.Tx) error {
		create := tx.TicketingRole.Create().
			SetID(role.ID).
			SetTenantID(*role.TenantID).
			SetRoleCode(role.RoleCode).
			SetName(role.Name).
			SetIsSystemRole(role.IsSystemRole)
		if role.Description != nil {
			create.SetDescription(*role.Description)
		}
		row, err := create.Save(ctx)
		if ent.IsConstraintError(err) {
			return ErrRoleCodeTaken
		}
		if err != nil {
			return fmt.Errorf("create role: %w", err)
		}
		*role = toRole(row)
		return createRolePermissions(ctx, tx, role.ID, permissionIDs)
	})
}

func (r *EntRepository) UpdateRoleDetails(ctx context.Context, tenantID, roleID uuid.UUID, name, description *string) error {
	update := r.client.TicketingRole.Update().
		Where(ticketingrole.TenantID(tenantID), ticketingrole.ID(roleID))
	if name != nil {
		update.SetName(*name)
	}
	if description != nil {
		update.SetDescription(*description)
	}
	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	return nil
}

func (r *EntRepository) MarkRoleCustomized(ctx context.Context, tenantID, roleID uuid.UUID) error {
	if _, err := r.client.TicketingRole.Update().
		Where(ticketingrole.TenantID(tenantID), ticketingrole.ID(roleID)).
		SetIsSystemRole(false).
		Save(ctx); err != nil {
		return fmt.Errorf("customize role: %w", err)
	}
	return nil
}

func (r *EntRepository) ReplaceRolePermissions(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error {
	return r.withTx(ctx, func(tx *ent.Tx) error {
		if _, err := tx.RolePermission.Delete().Where(rolepermission.RoleID(roleID)).Exec(ctx); err != nil {
			return fmt.Errorf("clear role permissions: %w", err)
		}
		return createRolePermissions(ctx, tx, roleID, permissionIDs)
	})
}

func (r *EntRepository) DeleteRole(ctx context.Context, tenantID, roleID uuid.UUID) error {
	return r.withTx(ctx, func(tx *ent.Tx) error {
		if _, err := tx.RolePermission.Delete().Where(rolepermission.RoleID(roleID)).Exec(ctx); err != nil {
			return fmt.Errorf("clear role permissions: %w", err)
		}
		if _, err := tx.UserRoleAssignment.Delete().
			Where(userroleassignment.TenantID(tenantID), userroleassignment.RoleID(roleID)).
			Exec(ctx); err != nil {
			return fmt.Errorf("clear ended assignments: %w", err)
		}
		if _, err := tx.TicketingRole.Delete().
			Where(ticketingrole.TenantID(tenantID), ticketingrole.ID(roleID)).
			Exec(ctx); err != nil {
			return fmt.Errorf("delete role: %w", err)
		}
		return nil
	})
}

// roleCount is one row of a count grouped by role.
type roleCount struct {
	RoleID uuid.UUID `json:"role_id"`
	Count  int       `json:"count"`
}

func countsByRole(rows []roleCount) map[uuid.UUID]int {
	out := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		out[row.RoleID] = row.Count
	}
	return out
}

func (r *EntRepository) RolePermissionCounts(ctx context.Context, roleIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	if len(roleIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}
	var rows []roleCount
	if err := r.client.RolePermission.Query().
		Where(rolepermission.RoleIDIn(roleIDs...)).
		GroupBy(rolepermission.FieldRoleID).
		Aggregate(ent.Count()).
		Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("count role permissions: %w", err)
	}
	return countsByRole(rows), nil
}

func (r *EntRepository) RoleMemberCounts(ctx context.Context, tenantID uuid.UUID, now time.Time) (map[uuid.UUID]int, error) {
	var rows []roleCount
	if err := r.client.UserRoleAssignment.Query().
		Where(userroleassignment.TenantID(tenantID), notExpired(now)).
		GroupBy(userroleassignment.FieldRoleID).
		Aggregate(ent.Count()).
		Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("count role members: %w", err)
	}
	return countsByRole(rows), nil
}

func (r *EntRepository) RolePermissionCodes(ctx context.Context, roleID uuid.UUID) ([]string, error) {
	codes, err := r.client.TicketingPermission.Query().
		Where(ticketingpermission.HasRolesWith(ticketingrole.ID(roleID))).
		Order(ent.Asc(ticketingpermission.FieldPermissionCode)).
		Select(ticketingpermission.FieldPermissionCode).
		Strings(ctx)
	if err != nil {
		return nil, fmt.Errorf("role permissions: %w", err)
	}
	return codes, nil
}

func (r *EntRepository) ListPermissions(ctx context.Context) ([]PermissionRecord, error) {
	rows, err := r.client.TicketingPermission.Query().
		Order(ent.Asc(ticketingpermission.FieldPermissionCode)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	out := make([]PermissionRecord, len(rows))
	for i, p := range rows {
		out[i] = PermissionRecord{
			ID: p.ID, PermissionCode: p.PermissionCode, Name: PermissionName(p.Module, p.Action),
			Module: p.Module, Action: p.Action, CreatedAt: p.CreatedAt,
		}
		if p.Resource != "" {
			res := p.Resource
			out[i].Resource = &res
		}
		if p.Description != "" {
			desc := p.Description
			out[i].Description = &desc
		}
	}
	return out, nil
}

func (r *EntRepository) PermissionIDs(ctx context.Context, codes []string) (map[string]uuid.UUID, error) {
	out := make(map[string]uuid.UUID, len(codes))
	if len(codes) == 0 {
		return out, nil
	}
	rows, err := r.client.TicketingPermission.Query().
		Where(ticketingpermission.PermissionCodeIn(codes...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve permissions: %w", err)
	}
	for _, p := range rows {
		out[p.PermissionCode] = p.ID
	}
	return out, nil
}

func (r *EntRepository) findAssignment(ctx context.Context, a *Assignment) (*ent.UserRoleAssignment, error) {
	return r.client.UserRoleAssignment.Query().
		Where(
			userroleassignment.TenantID(a.TenantID),
			userroleassignment.UserID(a.UserID),
			userroleassignment.RoleID(a.RoleID),
		).
		Only(ctx)
}

func renewAssignment(ctx context.Context, existing *ent.UserRoleAssignment, a *Assignment) error {
	update := existing.Update().SetAssignedBy(a.AssignedBy)
	if a.ExpiresAt != nil {
		update.SetExpiresAt(*a.ExpiresAt)
	} else {
		update.ClearExpiresAt()
	}
	if err := update.Exec(ctx); err != nil {
		return fmt.Errorf("renew assignment: %w", err)
	}
	return nil
}

// SaveAssignment implements Repository. The tenant, user and role triple is unique, so an ended row
// is renewed in place; inserting beside it would fail.
func (r *EntRepository) SaveAssignment(ctx context.Context, a *Assignment) error {
	existing, err := r.findAssignment(ctx, a)
	if err == nil {
		return renewAssignment(ctx, existing, a)
	}
	if !ent.IsNotFound(err) {
		return fmt.Errorf("find assignment: %w", err)
	}
	create := r.client.UserRoleAssignment.Create().
		SetTenantID(a.TenantID).SetUserID(a.UserID).SetRoleID(a.RoleID).SetAssignedBy(a.AssignedBy)
	if a.ExpiresAt != nil {
		create.SetExpiresAt(*a.ExpiresAt)
	}
	err = create.Exec(ctx)
	if err == nil || !ent.IsConstraintError(err) {
		return err
	}
	// Another request granted it first.
	existing, err = r.findAssignment(ctx, a)
	if err != nil {
		return fmt.Errorf("find assignment after conflict: %w", err)
	}
	return renewAssignment(ctx, existing, a)
}

func (r *EntRepository) EndAssignment(ctx context.Context, tenantID, assignmentID uuid.UUID, at time.Time) error {
	if _, err := r.client.UserRoleAssignment.Update().
		Where(userroleassignment.TenantID(tenantID), userroleassignment.ID(assignmentID)).
		SetExpiresAt(at).
		Save(ctx); err != nil {
		return fmt.Errorf("end assignment: %w", err)
	}
	return nil
}

func (r *EntRepository) ListAssignments(ctx context.Context, tenantID uuid.UUID, f AssignmentFilters, now time.Time) ([]Assignment, error) {
	q := r.client.UserRoleAssignment.Query().Where(userroleassignment.TenantID(tenantID))
	if f.UserID != nil {
		q = q.Where(userroleassignment.UserID(*f.UserID))
	}
	if f.RoleID != nil {
		q = q.Where(userroleassignment.RoleID(*f.RoleID))
	}
	if len(f.UserIDs) > 0 {
		q = q.Where(userroleassignment.UserIDIn(f.UserIDs...))
	}
	if f.ActiveOnly {
		q = q.Where(notExpired(now))
	}
	rows, err := q.WithRole().Order(ent.Asc(userroleassignment.FieldAssignedAt)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	out := make([]Assignment, len(rows))
	for i, row := range rows {
		out[i] = Assignment{
			ID: row.ID, TenantID: row.TenantID, UserID: row.UserID, RoleID: row.RoleID,
			AssignedBy: row.AssignedBy, AssignedAt: row.AssignedAt, ExpiresAt: row.ExpiresAt,
		}
		if row.Edges.Role != nil {
			out[i].RoleCode = row.Edges.Role.RoleCode
			out[i].RoleName = row.Edges.Role.Name
		}
	}
	return out, nil
}

func (r *EntRepository) UserRoles(ctx context.Context, tenantID, id uuid.UUID, now time.Time) ([]Role, error) {
	rows, err := r.client.UserRoleAssignment.Query().
		Where(
			userroleassignment.TenantID(tenantID),
			userroleassignment.HasUserWith(userMatches(tenantID, id)),
			notExpired(now),
		).
		QueryRole().
		Order(ent.Asc(ticketingrole.FieldName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("user roles: %w", err)
	}
	out := make([]Role, len(rows))
	for i, row := range rows {
		out[i] = toRole(row)
	}
	return out, nil
}

func (r *EntRepository) WriteAudit(ctx context.Context, e AuditEntry) error {
	create := r.client.AuditLog.Create().
		SetUserID(e.UserID).
		SetUserEmail(e.UserEmail).
		SetAction(e.Action).
		SetResourceType(e.ResourceType).
		SetResourceID(e.ResourceID).
		SetNillableTenantID(e.TenantID).
		SetNillableChanges(e.Changes).
		SetNillableIPAddress(e.IPAddress)
	return create.Exec(ctx)
}

func (r *EntRepository) ListAudit(ctx context.Context, tenantID uuid.UUID, f AuditFilters, limit, offset int) ([]AuditEntry, int, error) {
	q := r.client.AuditLog.Query().Where(auditlog.TenantID(tenantID))
	if len(f.ResourceTypes) > 0 {
		q = q.Where(auditlog.ResourceTypeIn(f.ResourceTypes...))
	}
	if f.ResourceType != "" {
		q = q.Where(auditlog.ResourceType(f.ResourceType))
	}
	if f.UserID != nil {
		q = q.Where(auditlog.UserID(*f.UserID))
	}
	if f.Action != "" {
		q = q.Where(auditlog.Action(f.Action))
	}
	if f.From != nil {
		q = q.Where(auditlog.CreatedAtGTE(*f.From))
	}
	if f.To != nil {
		q = q.Where(auditlog.CreatedAtLT(*f.To))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}
	rows, err := q.Order(ent.Desc(auditlog.FieldCreatedAt), ent.Desc(auditlog.FieldID)).
		Limit(limit).Offset(offset).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit logs: %w", err)
	}
	out := make([]AuditEntry, len(rows))
	for i, l := range rows {
		out[i] = AuditEntry{
			ID: l.ID, TenantID: l.TenantID, UserID: l.UserID, UserEmail: l.UserEmail, Action: l.Action,
			ResourceType: l.ResourceType, ResourceID: l.ResourceID, Changes: l.Changes, IPAddress: l.IPAddress,
			CreatedAt: l.CreatedAt,
		}
	}
	return out, total, nil
}

func toRole(row *ent.TicketingRole) Role {
	tid := row.TenantID
	role := Role{
		ID: row.ID, TenantID: &tid, RoleCode: row.RoleCode, Name: row.Name,
		IsSystemRole: row.IsSystemRole, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.Description != "" {
		desc := row.Description
		role.Description = &desc
	}
	return role
}
