package tickets

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestFormatTicketNumber(t *testing.T) {
	for n, want := range map[int64]string{1: "TKT-000001", 42: "TKT-000042", 1234567: "TKT-1234567"} {
		if got := FormatTicketNumber(n); got != want {
			t.Errorf("FormatTicketNumber(%d) = %q, want %q", n, got, want)
		}
	}
}

// The allocation is one upsert keyed by tenant that returns the new value, so it is atomic in
// Postgres and needs no read before the write.
func TestAllocateTicketNumberSQL(t *testing.T) {
	sql := strings.Join(strings.Fields(allocateTicketNumberSQL), " ")
	for _, part := range []string{
		"INSERT INTO ticket_sequences",
		"ON CONFLICT (tenant_id) DO UPDATE SET next_value = ticket_sequences.next_value + 1",
		"RETURNING next_value",
	} {
		if !strings.Contains(sql, part) {
			t.Errorf("allocation SQL lacks %q: %s", part, sql)
		}
	}
	if strings.Contains(strings.ToUpper(sql), "COUNT(") {
		t.Error("ticket numbers must not come from a row count")
	}
}

// counterRepo numbers tickets per tenant the way the sequence row does.
type counterRepo struct {
	fakeRepo
	mu   sync.Mutex
	next map[uuid.UUID]int64
}

func (r *counterRepo) InTx(_ context.Context, _ uuid.UUID, fn func(Repository, *Outbox) error) error {
	return fn(r, &Outbox{})
}

func (r *counterRepo) NextTicketNumber(_ context.Context, tenant uuid.UUID) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next[tenant]++
	return FormatTicketNumber(r.next[tenant]), nil
}

func (r *counterRepo) CreateTicket(context.Context, uuid.UUID, *Ticket) error { return nil }

// Numbers are per tenant: two tenants both start at TKT-000001, and concurrent creates in one
// tenant never share a number.
func TestTicketNumbersArePerTenantAndUnique(t *testing.T) {
	repo := &counterRepo{fakeRepo: *newFakeRepo(), next: map[uuid.UUID]int64{}}
	s := NewService(repo, zap.NewNop())
	a, b := uuid.New(), uuid.New()
	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(tenant uuid.UUID) {
			defer wg.Done()
			tk, err := s.CreateTicket(context.Background(), &CreateTicketRequest{TenantID: tenant, Subject: "S", Description: "D"})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			key := tenant.String() + tk.TicketNumber
			if seen[key] {
				t.Errorf("duplicate number %s in tenant %s", tk.TicketNumber, tenant)
			}
			seen[key] = true
		}(map[bool]uuid.UUID{true: a, false: b}[i%2 == 0])
	}
	wg.Wait()
	if !seen[a.String()+"TKT-000001"] || !seen[b.String()+"TKT-000001"] || len(seen) != 50 {
		t.Fatalf("numbers: %v", seen)
	}
}

func TestSLABreachEventType(t *testing.T) {
	cases := map[string]struct {
		in   map[string]any
		want string
	}{
		"prefixed resolution":  {map[string]any{"event_type": "sla.resolution_breached"}, "sla.resolution_breached"},
		"bare first response":  {map[string]any{"event_type": "first_response_breached"}, "sla.first_response_breached"},
		"missing defaults":     {map[string]any{}, "sla.first_response_breached"},
		"empty string default": {map[string]any{"event_type": ""}, "sla.first_response_breached"},
	}
	for name, c := range cases {
		if got := SLABreachEventType(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}
