package tickets

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// mention is the Tiptap mention markup the shared RichTextEditor writes.
func mention(id uuid.UUID, label string) string {
	return `<span data-type="mention" data-id="` + id.String() + `" data-label="` + label + `">@` + label + `</span>`
}

func TestParseMentions(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	// Attributes in another order still count.
	reordered := `<span class="mention" data-id="` + b.String() + `" data-type="mention">@Bob</span>`
	content := "<p>Hi " + mention(a, "Jane Doe") + ", see " + reordered + " and again " + mention(a, "Jane") + ".</p>" +
		`<p>Not mentions: @Jane, <span data-type="mention" data-id="not-a-uuid">@Bad</span>, ` +
		`<span data-id="` + uuid.NewString() + `">no type</span>, <a data-type="mention" data-id="` + uuid.NewString() + `">a link</a>.</p>`
	got := ParseMentions(content)
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("got %v, want [%v %v]", got, a, b)
	}
	if ParseMentions("<p>no mentions here</p>") != nil {
		t.Error("plain text mentions nobody")
	}
	many := ""
	for i := 0; i < 30; i++ {
		many += mention(uuid.New(), "A") + " "
	}
	if n := len(ParseMentions(many)); n != maxMentions {
		t.Errorf("capped at %d, got %d", maxMentions, n)
	}
}

func TestCommentMentionsNotifyTenantAgentsOnly(t *testing.T) {
	repo := newWorkRepo()
	svc := newWorkService(repo)
	ticket := repo.addTicket("TKT-000009", StatusOpen)
	agent, stranger := uuid.New(), uuid.New()
	repo.agents[agent] = "jane@shop.co.ke"
	note := "<p>" + mention(agent, "Jane") + " and " + mention(stranger, "Who") + " please check</p>"

	c, err := svc.AddComment(context.Background(), &AddCommentRequest{
		TicketID: ticket.ID, AuthorID: uuid.New(), AuthorType: AuthorAgent, CommentType: CommentInternal, Content: note,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Mentions) != 1 || c.Mentions[0] != agent {
		t.Fatalf("only the tenant's agent is stored: %v", c.Mentions)
	}
	if ev := repo.eventTypes(); len(ev) != 1 || ev[0] != EventTicketMentioned {
		t.Fatalf("an internal note with a mention sends ticket.mentioned only: %v", ev)
	}
	data := repo.events[0].Data
	if data["agent_id"] != agent.String() || data["agent_email"] != "jane@shop.co.ke" || data["comment_type"] != "internal" {
		t.Fatalf("payload: %v", data)
	}
	if _, has := data["requester_email"]; has {
		t.Fatal("a mention never carries the requester's contact")
	}

	// A customer typing the token mentions nobody.
	repo.events = nil
	c, err = svc.AddComment(context.Background(), &AddCommentRequest{
		TicketID: ticket.ID, AuthorID: uuid.New(), AuthorType: AuthorCustomer, CommentType: CommentPublic, Content: note,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Mentions) != 0 || len(repo.events) != 0 {
		t.Fatalf("customer mention: %v %v", c.Mentions, repo.eventTypes())
	}
}
