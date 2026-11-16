package tickets

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestValidateMerge(t *testing.T) {
	open := func() *Ticket { return &Ticket{ID: uuid.New(), Status: StatusOpen, Metadata: map[string]any{}} }
	src, tgt := open(), open()
	if err := ValidateMerge(src, tgt); err != nil {
		t.Fatalf("two open tickets merge: %v", err)
	}
	if err := ValidateMerge(src, src); err != ErrMergeSelf {
		t.Errorf("self merge: %v", err)
	}
	merged := open()
	merged.Metadata[MetaMergedInto] = uuid.NewString()
	if err := ValidateMerge(merged, tgt); err != ErrAlreadyMerged {
		t.Errorf("already merged source: %v", err)
	}
	if err := ValidateMerge(src, merged); err != ErrMergeTargetMerged {
		t.Errorf("merged target: %v", err)
	}
	for _, s := range []TicketStatus{StatusClosed, StatusCancelled} {
		closed := open()
		closed.Status = s
		if err := ValidateMerge(src, closed); err != ErrMergeTargetClosed {
			t.Errorf("target %s: %v", s, err)
		}
		if err := ValidateMerge(closed, tgt); err != ErrMergeSourceClosed {
			t.Errorf("source %s: %v", s, err)
		}
	}
	resolved := open()
	resolved.Status = StatusResolved
	if err := ValidateMerge(resolved, tgt); err != nil {
		t.Errorf("a resolved ticket can still be merged: %v", err)
	}
}

func TestMergeTicketMovesClosesAndNotes(t *testing.T) {
	repo := newWorkRepo()
	svc := newWorkService(repo)
	src := repo.addTicket("TKT-000001", StatusOpen)
	src.Tags = []string{"billing"}
	tgt := repo.addTicket("TKT-000002", StatusInProgress)
	tgt.Tags = []string{"vip"}
	actor := uuid.New()

	got, err := svc.MergeTicket(context.Background(), uuid.Nil, src.ID, tgt.ID, actor)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != tgt.ID {
		t.Fatalf("answers the target, got %v", got.ID)
	}
	if len(repo.moved) != 1 || repo.moved[0] != [2]uuid.UUID{src.ID, tgt.ID} {
		t.Fatalf("thread move: %v", repo.moved)
	}
	if repo.tickets[src.ID].Status != StatusClosed || repo.tickets[src.ID].Metadata[MetaMergedInto] != tgt.ID.String() {
		t.Fatalf("source must close with merged_into: %+v", repo.tickets[src.ID])
	}
	from, _ := repo.tickets[tgt.ID].Metadata[MetaMergedFrom].([]any)
	if len(from) != 1 || from[0] != src.ID.String() {
		t.Fatalf("target merged_from: %v", repo.tickets[tgt.ID].Metadata)
	}
	if tags := repo.tickets[tgt.ID].Tags; len(tags) != 2 || tags[0] != "vip" || tags[1] != "billing" {
		t.Fatalf("target takes the source's tags: %v", tags)
	}
	if len(repo.comments) != 2 {
		t.Fatalf("a system note on each ticket, got %d", len(repo.comments))
	}
	for _, c := range repo.comments {
		if c.CommentType != CommentSystem || c.AuthorType != AuthorSystem || c.AuthorID != actor {
			t.Errorf("merge note: %+v", c)
		}
	}
	if ev := repo.eventTypes(); len(ev) != 1 || ev[0] != EventTicketMerged {
		t.Fatalf("events: %v", ev)
	}
	if repo.events[0].Data["merged_into_number"] != "TKT-000002" {
		t.Errorf("merged payload: %v", repo.events[0].Data)
	}

	// A second merge of the same source is refused and records nothing more.
	other := repo.addTicket("TKT-000003", StatusOpen)
	if _, err := svc.MergeTicket(context.Background(), uuid.Nil, src.ID, other.ID, actor); err != ErrAlreadyMerged {
		t.Fatalf("re-merge: %v", err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("a refused merge records no event: %v", repo.eventTypes())
	}
}
