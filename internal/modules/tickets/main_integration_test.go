//go:build integration

package tickets

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/sla"
	"github.com/bengobox/ticketing-service/internal/platform/events"
	"github.com/bengobox/ticketing-service/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m, "it_tickets") }

// itEnv is the real repository over the package's migrated schema, with the real outbox writer,
// and the service on top of it. Tests share the schema and keep apart by using fresh tenants.
type itEnv struct {
	db   *testsupport.DB
	repo *EntRepository
	svc  *Service
}

func newITEnv(t *testing.T) *itEnv {
	t.Helper()
	db := testsupport.Open(t)
	repo := NewEntRepository(db.Ent, db.SQL, events.NewOutbox(db.SQL))
	return &itEnv{db: db, repo: repo, svc: NewService(repo, zap.NewNop())}
}

// insert stores a ticket through the repository with the next number of its tenant. mut sets the
// fields the test cares about.
func (e *itEnv) insert(t *testing.T, tenant uuid.UUID, mut func(*Ticket)) *Ticket {
	t.Helper()
	ctx := context.Background()
	number, err := e.repo.NextTicketNumber(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	tk := &Ticket{
		ID: uuid.New(), TenantID: tenant, TicketNumber: number, CustomerID: uuid.New(),
		Subject: "Ticket " + number, Description: "Seeded by an integration test",
		Status: StatusOpen, Priority: PriorityNormal, Channel: ChannelWeb,
	}
	if mut != nil {
		mut(tk)
	}
	if err := e.repo.CreateTicket(ctx, tenant, tk); err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	return tk
}

// get reads a ticket back and fails the test when it is gone.
func (e *itEnv) get(t *testing.T, tenant, id uuid.UUID) *Ticket {
	t.Helper()
	tk, err := e.repo.GetTicket(context.Background(), tenant, id)
	if err != nil {
		t.Fatalf("get ticket %s: %v", id, err)
	}
	return tk
}

// itSLA is internal/app's slaAdapter (which this package cannot import): the SLA module seen from
// tickets.
type itSLA struct{ svc *sla.Service }

func (a *itSLA) ComputeDeadlines(ctx context.Context, tenantID uuid.UUID, priority string, categoryID *uuid.UUID, start time.Time) (*SLAPlan, error) {
	r, err := a.svc.ComputeDeadlines(ctx, tenantID, sla.Priority(priority), categoryID, start)
	if err != nil || r == nil {
		return nil, err
	}
	return &SLAPlan{PolicyID: r.PolicyID, ResponseDueAt: r.ResponseDueAt, ResolutionDueAt: r.ResolutionDueAt}, nil
}

func (a *itSLA) PolicyClock(ctx context.Context, tenantID uuid.UUID, policyID *uuid.UUID) (SLAClock, error) {
	c, err := a.svc.PolicyClock(ctx, tenantID, policyID)
	if err != nil {
		return SLAClock{}, err
	}
	return SLAClock{Calendar: c.Calendar, PauseOnPendingInternal: c.PauseOnPendingInternal}, nil
}

func itPtr[T any](v T) *T { return &v }
