package tickets

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// A page of comments resolves every author name with one lookup of the distinct user ids: staff
// by their agent display name, customers by the channel sender, their user name or the ticket's
// requester name, and system comments as "System" without a lookup.
func TestPageCommentsNamesAuthorsInOneLookup(t *testing.T) {
	agent, customer, unknown := uuid.New(), uuid.New(), uuid.New()
	base := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	mk := func(i int, author uuid.UUID, kind AuthorType) *TicketComment {
		return &TicketComment{ID: uuid.New(), AuthorID: author, AuthorType: kind, CreatedAt: base.Add(time.Duration(i) * time.Minute)}
	}
	agentReply := mk(0, agent, AuthorAgent)
	agentNote := mk(1, agent, AuthorAgent)
	signedIn := mk(2, customer, AuthorCustomer)
	viaEmail := mk(3, unknown, AuthorCustomer)
	viaEmail.Sender = &CommentSender{Name: "Wanjiru (finance)", Email: "w@example.com"}
	anonymous := mk(4, unknown, AuthorCustomer)
	system := mk(5, uuid.Nil, AuthorSystem)
	repo := &listRepo{
		comments:    []*TicketComment{agentReply, agentNote, signedIn, viaEmail, anonymous, system},
		authorNames: map[uuid.UUID]string{agent: "Otieno", customer: "Amina Hassan"},
	}
	tk := &Ticket{ID: uuid.New(), Metadata: map[string]any{MetaRequesterName: "Amina H."}}

	page, err := NewService(repo, zap.NewNop()).PageTicketComments(context.Background(), tk, CommentQuery{Page: PageParams{Limit: 25}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]string{
		agentReply.ID: "Otieno",
		agentNote.ID:  "Otieno",
		signedIn.ID:   "Amina Hassan",
		viaEmail.ID:   "Wanjiru (finance)",
		anonymous.ID:  "Amina H.",
		system.ID:     SystemAuthorName,
	}
	for _, c := range page.Data {
		if c.AuthorName != want[c.ID] {
			t.Errorf("comment by %s: author_name %q, want %q", c.AuthorType, c.AuthorName, want[c.ID])
		}
	}
	if repo.nameCalls != 1 {
		t.Fatalf("%d name lookups for one page, want 1", repo.nameCalls)
	}
	// agent (twice), customer, and unknown for the unnamed message only (the channel sender is
	// already named): three distinct ids, the system comment left out.
	if n := len(repo.nameIDs[0]); n != 3 {
		t.Fatalf("looked up %d ids, want the 3 distinct authors", n)
	}
}

// A page of system comments only, or with every customer named by the channel, needs no lookup.
func TestPageCommentsSkipsLookupWhenNothingToResolve(t *testing.T) {
	sender := &TicketComment{ID: uuid.New(), AuthorID: uuid.New(), AuthorType: AuthorCustomer, Sender: &CommentSender{Name: "Jo"}}
	repo := &listRepo{comments: []*TicketComment{
		{ID: uuid.New(), AuthorType: AuthorSystem},
		sender,
	}}
	page, err := NewService(repo, zap.NewNop()).PageComments(context.Background(), uuid.Nil, uuid.Nil, CommentQuery{Page: PageParams{Limit: 25}})
	if err != nil {
		t.Fatal(err)
	}
	if repo.nameCalls != 0 {
		t.Fatalf("%d name lookups, want 0", repo.nameCalls)
	}
	if page.Data[0].AuthorName != SystemAuthorName || page.Data[1].AuthorName != "Jo" {
		t.Fatalf("names: %q %q", page.Data[0].AuthorName, page.Data[1].AuthorName)
	}
}
