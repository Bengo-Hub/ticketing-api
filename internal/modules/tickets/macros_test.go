package tickets

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMacroValidate(t *testing.T) {
	bogus := TicketStatus("bogus")
	pending := StatusPendingCustomer
	cases := []struct {
		name string
		in   MacroInput
		ok   bool
	}{
		{"no name", MacroInput{Body: "Hi"}, false},
		{"empty macro", MacroInput{Name: "Nothing"}, false},
		{"bad status", MacroInput{Name: "x", Actions: MacroActions{Status: &bogus}}, false},
		{"system reply", MacroInput{Name: "x", Body: "Hi", Actions: MacroActions{CommentType: CommentSystem}}, false},
		{"reply only", MacroInput{Name: "Thanks", Body: "Thanks!"}, true},
		{"actions only", MacroInput{Name: "Wait", Actions: MacroActions{Status: &pending}}, true},
	}
	for _, c := range cases {
		err := c.in.Validate()
		var verr *ValidationError
		if c.ok != (err == nil) || (!c.ok && !errors.As(err, &verr)) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestRenderMacroBody(t *testing.T) {
	tk := &Ticket{TicketNumber: "TKT-000007", Subject: "Card declined", Metadata: map[string]any{MetaRequesterName: "Amina"}}
	got := RenderMacroBody("Hi {{requester_name}}, about {{ticket_number}} ({{subject}}).", tk)
	if got != "Hi Amina, about TKT-000007 (Card declined)." {
		t.Fatalf("got %q", got)
	}
}

func TestApplyMacroChangesFieldsAndReplies(t *testing.T) {
	repo := newWorkRepo()
	svc := newWorkService(repo)
	tk := repo.addTicket("TKT-000005", StatusOpen)
	tk.Tags = []string{"billing"}
	tk.Metadata[MetaRequesterEmail] = "amina@example.com"
	high, pending := PriorityHigh, StatusPendingCustomer
	team := uuid.New()
	repo.teams[team] = true
	m := &Macro{ID: uuid.New(), Name: "Need receipt", IsActive: true, Body: "Hi, please send the receipt for {{ticket_number}}.",
		Actions: MacroActions{Priority: &high, Status: &pending, TeamID: &team, AddTags: []string{"Receipt"}}}
	repo.macros[m.ID] = m
	agentUser := uuid.New()

	res, err := svc.ApplyMacro(context.Background(), uuid.Nil, tk.ID, m.ID, agentUser)
	if err != nil {
		t.Fatal(err)
	}
	got := repo.tickets[tk.ID]
	if got.Priority != PriorityHigh || got.Status != StatusPendingCustomer || got.AssignedTeamID == nil || *got.AssignedTeamID != team {
		t.Fatalf("fields: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[1] != "receipt" {
		t.Fatalf("tags: %v", got.Tags)
	}
	if res.Comment == nil || res.Comment.Content != "Hi, please send the receipt for TKT-000005." ||
		res.Comment.CommentType != CommentPublic || res.Comment.AuthorID != agentUser {
		t.Fatalf("reply: %+v", res.Comment)
	}
	want := []string{EventTicketAssigned, EventTicketStatusChanged, EventTicketCommentAdded}
	ev := repo.eventTypes()
	if len(ev) != len(want) {
		t.Fatalf("events: %v, want %v", ev, want)
	}
	for i := range want {
		if ev[i] != want[i] {
			t.Fatalf("events: %v, want %v", ev, want)
		}
	}
}

func TestApplyMacroRefusesAnInvalidTransitionWhole(t *testing.T) {
	repo := newWorkRepo()
	svc := newWorkService(repo)
	tk := repo.addTicket("TKT-000006", StatusCancelled)
	open := StatusOpen
	m := &Macro{ID: uuid.New(), Name: "Reopen", IsActive: true, Body: "Reopening.", Actions: MacroActions{Status: &open}}
	repo.macros[m.ID] = m
	if _, err := svc.ApplyMacro(context.Background(), uuid.Nil, tk.ID, m.ID, uuid.New()); err != ErrInvalidTransition {
		t.Fatalf("got %v", err)
	}
	if len(repo.comments) != 0 || len(repo.events) != 0 {
		t.Fatal("a refused macro posts no reply and records no event")
	}
	m.IsActive = false
	if _, err := svc.ApplyMacro(context.Background(), uuid.Nil, tk.ID, m.ID, uuid.New()); err != ErrMacroNotFound {
		t.Fatalf("inactive macro: %v", err)
	}
}
