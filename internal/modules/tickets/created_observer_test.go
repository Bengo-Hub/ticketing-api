package tickets

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// blockingObserver stands in for a slow or hung AI call.
type blockingObserver struct {
	seen    chan Ticket
	release chan struct{}
}

func (o *blockingObserver) TicketCreated(_ context.Context, t Ticket) {
	o.seen <- t
	<-o.release
}

// Never blocking: a create returns while the observer (auto-classify) is still running, and the
// observer sees the committed ticket.
func TestCreateNeverWaitsOnTheCreatedObserver(t *testing.T) {
	repo := newFakeRepo()
	s := NewService(repo, zap.NewNop())
	obs := &blockingObserver{seen: make(chan Ticket, 1), release: make(chan struct{})}
	defer close(obs.release)
	s.SetCreatedObserver(obs)

	done := make(chan *Ticket, 1)
	go func() {
		t2, err := s.CreateTicket(context.Background(), &CreateTicketRequest{TenantID: uuid.New(), Subject: "S", Description: "D"})
		if err != nil {
			t.Error(err)
		}
		done <- t2
	}()
	var created *Ticket
	select {
	case created = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the create waited on the observer")
	}
	if _, ok := repo.tickets[created.ID]; !ok {
		t.Fatal("ticket not saved")
	}
	select {
	case seen := <-obs.seen:
		if seen.ID != created.ID || seen.TicketNumber == "" {
			t.Errorf("observer saw %+v", seen)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the observer was never called")
	}
}

type panickingObserver struct{ called chan struct{} }

func (o panickingObserver) TicketCreated(context.Context, Ticket) {
	close(o.called)
	panic("boom")
}

// A panic in the observer never reaches the create or the process.
func TestCreatedObserverPanicIsContained(t *testing.T) {
	s := NewService(newFakeRepo(), zap.NewNop())
	obs := panickingObserver{called: make(chan struct{})}
	s.SetCreatedObserver(obs)
	if _, err := s.CreateTicket(context.Background(), &CreateTicketRequest{TenantID: uuid.New(), Subject: "S"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-obs.called:
	case <-time.After(2 * time.Second):
		t.Fatal("observer not called")
	}
	time.Sleep(20 * time.Millisecond) // the recover runs; the test process must still be alive
}
