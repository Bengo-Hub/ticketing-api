//go:build integration

package rbac

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m, "it_rbac") }

func itRBAC(t *testing.T) (*testsupport.DB, *EntRepository, *Service) {
	t.Helper()
	db := testsupport.Open(t)
	repo := NewEntRepository(db.Ent)
	svc := NewService(repo, zap.NewNop())
	if err := svc.Seed(context.Background()); err != nil {
		t.Fatalf("seed catalogue: %v", err)
	}
	return db, repo, svc
}

// itAgent reads the agent profile of an auth-api user in a tenant.
func itAgent(t *testing.T, db *testsupport.DB, tenant, authUser uuid.UUID) (role string, active, found bool) {
	t.Helper()
	err := db.SQL.QueryRow(`SELECT role, is_active FROM agents WHERE tenant_id = $1 AND user_id = $2`, tenant, authUser).Scan(&role, &active)
	if err != nil {
		return "", false, false
	}
	return role, active, true
}

// Granting writes one assignment row per (tenant, user, role) and gives a staff role an agent
// profile; revoking ends the row and keeps it; the last staff role revoked deactivates the
// profile; granting again renews the same row.
func TestIntegrationRoleAssignRevokeAndAgentProfile(t *testing.T) {
	db, repo, svc := itRBAC(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	admin, authUser := uuid.New(), uuid.New()

	user, err := svc.AddMember(ctx, tenant, authUser, "pat@example.com")
	if err != nil {
		t.Fatal(err)
	}
	// Seeding the tenant's roles again changes nothing.
	before, err := repo.ListRoles(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureTenantRoles(ctx, tenant, SystemRoles); err != nil {
		t.Fatal(err)
	}
	if after, _ := repo.ListRoles(ctx, tenant); len(after) != len(before) || len(before) == 0 {
		t.Fatalf("roles %d after a second seed, %d before", len(after), len(before))
	}
	agentRole, err := repo.GetRoleByCode(ctx, tenant, RoleAgent)
	if err != nil || agentRole == nil {
		t.Fatalf("agent role: %v %v", agentRole, err)
	}
	supRole, err := repo.GetRoleByCode(ctx, tenant, RoleSupervisor)
	if err != nil || supRole == nil {
		t.Fatalf("supervisor role: %v %v", supRole, err)
	}
	rows := func(roleID uuid.UUID) int64 {
		return db.Int64(t, `SELECT count(*) FROM user_role_assignments WHERE tenant_id = $1 AND user_id = $2 AND role_id = $3`,
			tenant, user.ID, roleID)
	}

	if _, err := svc.AssignRole(ctx, tenant, user.ID, agentRole.ID, admin, nil); err != nil {
		t.Fatal(err)
	}
	if role, active, ok := itAgent(t, db, tenant, authUser); !ok || !active || role != "agent" {
		t.Fatalf("agent profile after grant = %q active=%v found=%v", role, active, ok)
	}
	g, err := repo.Grants(ctx, tenant, user.ID, authUser, time.Now())
	if err != nil || g.AgentID == nil || len(g.Perms) == 0 {
		t.Fatalf("grants after agent role = %+v, %v", g, err)
	}

	// A stronger staff role raises the profile.
	if _, err := svc.AssignRole(ctx, tenant, user.ID, supRole.ID, admin, nil); err != nil {
		t.Fatal(err)
	}
	if role, active, _ := itAgent(t, db, tenant, authUser); !active || role != "supervisor" {
		t.Fatalf("agent profile after supervisor grant = %q active=%v", role, active)
	}

	// Revoking one staff role keeps the profile while another staff role is held.
	if _, err := svc.RevokeRole(ctx, tenant, nil, user.ID, supRole.ID); err != nil {
		t.Fatal(err)
	}
	if rows(supRole.ID) != 1 {
		t.Errorf("revoked assignment row gone or duplicated")
	}
	ended := db.Int64(t, `SELECT count(*) FROM user_role_assignments WHERE tenant_id = $1 AND user_id = $2 AND role_id = $3 AND expires_at IS NOT NULL`,
		tenant, user.ID, supRole.ID)
	if ended != 1 {
		t.Error("revoked assignment has no end time")
	}
	if _, active, _ := itAgent(t, db, tenant, authUser); !active {
		t.Error("profile deactivated while the agent role is still held")
	}

	// The last staff role goes: the profile goes inactive and no grant reaches it.
	if _, err := svc.RevokeRole(ctx, tenant, nil, user.ID, agentRole.ID); err != nil {
		t.Fatal(err)
	}
	if _, active, ok := itAgent(t, db, tenant, authUser); !ok || active {
		t.Errorf("profile after the last staff role: found=%v active=%v, want inactive", ok, active)
	}
	held, err := repo.UserRoles(ctx, tenant, user.ID, time.Now())
	if err != nil || len(held) != 0 {
		t.Errorf("roles held after revoking all = %v, %v", held, err)
	}
	if g, err := repo.Grants(ctx, tenant, user.ID, authUser, time.Now()); err != nil || g.AgentID != nil || len(g.Roles) != 0 {
		t.Errorf("grants after revoking all = %+v, %v", g, err)
	}
	if _, err := svc.RevokeRole(ctx, tenant, nil, user.ID, agentRole.ID); !errors.Is(err, ErrAssignmentMissing) {
		t.Errorf("second revoke = %v, want ErrAssignmentMissing", err)
	}
	// The ended rows stay, so sign-in knows an admin took the roles away.
	if has, err := repo.HasAssignments(ctx, tenant, user.ID); err != nil || !has {
		t.Errorf("HasAssignments after revoke = %v, %v", has, err)
	}

	// Granting again renews the same row and reactivates the profile.
	if _, err := svc.AssignRole(ctx, tenant, user.ID, agentRole.ID, admin, nil); err != nil {
		t.Fatal(err)
	}
	if rows(agentRole.ID) != 1 {
		t.Errorf("regrant wrote %d rows, want the one renewed", rows(agentRole.ID))
	}
	if _, active, _ := itAgent(t, db, tenant, authUser); !active {
		t.Error("profile still inactive after regrant")
	}

	// Nothing of this tenant shows in another.
	if list, err := repo.ListAssignments(ctx, other, AssignmentFilters{UserID: &user.ID}, time.Now()); err != nil || len(list) != 0 {
		t.Errorf("assignments seen from another tenant: %v, %v", list, err)
	}
	if u, err := repo.GetUser(ctx, other, user.ID); err != nil || u != nil {
		t.Errorf("user seen from another tenant: %v, %v", u, err)
	}
}

// The audit log keeps one row per write, lists a tenant's rows newest first with their total,
// filters by type, action, user and time, and never shows another tenant's rows.
func TestIntegrationAuditLog(t *testing.T) {
	_, repo, _ := itRBAC(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	actor, someone := uuid.New(), uuid.New()
	changes := `{"role":"agent"}`
	write := func(tenantID *uuid.UUID, user uuid.UUID, action, resource string) time.Time {
		t.Helper()
		if err := repo.WriteAudit(ctx, AuditEntry{TenantID: tenantID, UserID: user, UserEmail: "admin@example.com",
			Action: action, ResourceType: resource, ResourceID: uuid.NewString(), Changes: &changes}); err != nil {
			t.Fatal(err)
		}
		// Distinct created_at values keep the newest-first order deterministic.
		time.Sleep(15 * time.Millisecond)
		return time.Now()
	}
	write(&tenant, actor, "role.assigned", "user_role")
	mid := write(&tenant, actor, "role.revoked", "user_role")
	write(&tenant, someone, "role.updated", "role")
	write(&other, actor, "role.assigned", "user_role")
	write(nil, actor, "permission.seeded", "permission")

	all, total, err := repo.ListAudit(ctx, tenant, AuditFilters{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("tenant audit = %d rows, total %d; want 3", len(all), total)
	}
	if all[0].Action != "role.updated" || all[2].Action != "role.assigned" {
		t.Errorf("order = %s, %s, %s; want newest first", all[0].Action, all[1].Action, all[2].Action)
	}
	for _, e := range all {
		if e.TenantID == nil || *e.TenantID != tenant {
			t.Errorf("row of tenant %v in tenant %s's log", e.TenantID, tenant)
		}
		if e.Changes == nil || *e.Changes != changes {
			t.Errorf("changes = %v", e.Changes)
		}
	}

	page, total, err := repo.ListAudit(ctx, tenant, AuditFilters{}, 1, 1)
	if err != nil || total != 3 || len(page) != 1 || page[0].ID != all[1].ID {
		t.Errorf("second page of one = %v, total %d, %v", page, total, err)
	}
	check := func(label string, f AuditFilters, want int) {
		t.Helper()
		rows, total, err := repo.ListAudit(ctx, tenant, f, 10, 0)
		if err != nil || total != want || len(rows) != want {
			t.Errorf("%s: %d rows, total %d, err %v; want %d", label, len(rows), total, err, want)
		}
	}
	check("resource types", AuditFilters{ResourceTypes: []string{"user_role"}}, 2)
	check("resource type", AuditFilters{ResourceType: "role"}, 1)
	check("action", AuditFilters{Action: "role.revoked"}, 1)
	check("user", AuditFilters{UserID: &someone}, 1)
	check("from", AuditFilters{From: &mid}, 1)
	check("to", AuditFilters{To: &mid}, 2)
}
