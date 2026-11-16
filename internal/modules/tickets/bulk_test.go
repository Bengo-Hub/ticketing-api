package tickets

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func ids(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

func TestBulkValidateLimits(t *testing.T) {
	closed := StatusClosed
	bogus := TicketStatus("bogus")
	high := PriorityHigh
	cases := []struct {
		name string
		req  BulkUpdateRequest
		ok   bool
	}{
		{"no ids", BulkUpdateRequest{Status: &closed}, false},
		{"101 ids", BulkUpdateRequest{IDs: ids(101), Status: &closed}, false},
		{"100 ids", BulkUpdateRequest{IDs: ids(100), Status: &closed}, true},
		{"nothing to change", BulkUpdateRequest{IDs: ids(2)}, false},
		{"blank tags change nothing", BulkUpdateRequest{IDs: ids(2), AddTags: []string{" ", ""}}, false},
		{"unknown status", BulkUpdateRequest{IDs: ids(1), Status: &bogus}, false},
		{"priority", BulkUpdateRequest{IDs: ids(1), Priority: &high}, true},
		{"tags", BulkUpdateRequest{IDs: ids(1), AddTags: []string{"vip"}}, true},
	}
	for _, c := range cases {
		err := c.req.Validate()
		var verr *ValidationError
		if c.ok && err != nil {
			t.Errorf("%s: unexpected %v", c.name, err)
		}
		if !c.ok && !errors.As(err, &verr) {
			t.Errorf("%s: want a validation error, got %v", c.name, err)
		}
	}
	// Repeated ids count once, so 150 copies of one id is one ticket.
	one := uuid.New()
	req := BulkUpdateRequest{Status: &closed}
	for i := 0; i < 150; i++ {
		req.IDs = append(req.IDs, one)
	}
	if err := req.Validate(); err != nil || len(req.IDs) != 1 {
		t.Fatalf("dedupe: %v %v", err, req.IDs)
	}
}

func TestBulkUpdateAppliesPerTicket(t *testing.T) {
	repo := newWorkRepo()
	svc := newWorkService(repo)
	open := repo.addTicket("TKT-000001", StatusOpen)
	fresh := repo.addTicket("TKT-000002", StatusNew)
	missing := uuid.New()
	resolved := StatusResolved
	req := &BulkUpdateRequest{IDs: []uuid.UUID{open.ID, fresh.ID, missing}, Status: &resolved, AddTags: []string{"Outage"}}

	results, err := svc.BulkUpdate(context.Background(), uuid.Nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || !results[0].OK || results[1].OK || results[2].OK {
		t.Fatalf("results: %+v", results)
	}
	if results[1].Error != ErrInvalidTransition.Error() || results[2].Error != ErrTicketNotFound.Error() {
		t.Fatalf("errors: %+v", results)
	}
	if repo.tickets[open.ID].Status != StatusResolved || len(repo.tickets[open.ID].Tags) != 1 {
		t.Fatalf("open ticket: %+v", repo.tickets[open.ID])
	}
	if repo.tickets[fresh.ID].Status != StatusNew || repo.tickets[fresh.ID].Tags != nil {
		t.Fatalf("a skipped ticket is untouched: %+v", repo.tickets[fresh.ID])
	}
	if ev := repo.eventTypes(); len(ev) != 1 || ev[0] != EventTicketResolved {
		t.Fatalf("events: %v", ev)
	}
}

func TestBulkAssignOpensNewTicketsAndChecksTheAgent(t *testing.T) {
	repo := newWorkRepo()
	svc := newWorkService(repo)
	fresh := repo.addTicket("TKT-000001", StatusNew)
	agent := uuid.New()
	repo.agents[agent] = "agent@shop.co.ke"
	inProgress := StatusInProgress

	// Assigning opens the new ticket first, so new to in_progress is allowed in one request.
	res, err := svc.BulkUpdate(context.Background(), uuid.Nil, &BulkUpdateRequest{IDs: []uuid.UUID{fresh.ID}, AgentID: &agent, Status: &inProgress})
	if err != nil || !res[0].OK {
		t.Fatalf("%v %+v", err, res)
	}
	if got := repo.tickets[fresh.ID]; got.Status != StatusInProgress || got.AssignedAgentID == nil || *got.AssignedAgentID != agent {
		t.Fatalf("ticket: %+v", got)
	}
	if ev := repo.eventTypes(); len(ev) != 2 || ev[0] != EventTicketAssigned || ev[1] != EventTicketStatusChanged {
		t.Fatalf("events: %v", ev)
	}

	stranger := uuid.New()
	if _, err := svc.BulkUpdate(context.Background(), uuid.Nil, &BulkUpdateRequest{IDs: []uuid.UUID{fresh.ID}, AgentID: &stranger}); err != ErrAssigneeNotFound {
		t.Fatalf("an agent of another tenant: %v", err)
	}
}
