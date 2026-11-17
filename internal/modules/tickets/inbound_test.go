package tickets

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// inboundRepo adds what the channel ingest reaches: metadata merges, file rows and a comment
// store that refuses a message stored twice.
type inboundRepo struct {
	*workRepo
	files    []*TicketAttachment
	external map[string]bool
}

func newInboundRepo() *inboundRepo {
	return &inboundRepo{workRepo: newWorkRepo(), external: map[string]bool{}}
}

func (r *inboundRepo) InTx(ctx context.Context, tenantID uuid.UUID, fn func(Repository, *Outbox) error) error {
	return r.workRepo.InTx(ctx, tenantID, func(_ Repository, out *Outbox) error { return fn(r, out) })
}

func (r *inboundRepo) CreateComment(ctx context.Context, tenantID uuid.UUID, c *TicketComment) error {
	if id, _ := c.Metadata[CommentMetaExternalID].(string); id != "" {
		if r.external[id] {
			return ErrDuplicateMessage
		}
		r.external[id] = true
	}
	return r.workRepo.CreateComment(ctx, tenantID, c)
}

func (r *inboundRepo) MergeTicketMetadata(_ context.Context, _ uuid.UUID, id uuid.UUID, patch map[string]any) error {
	t := r.tickets[id]
	for k, v := range patch {
		t.Metadata[k] = v
	}
	return nil
}

func (r *inboundRepo) CreateAttachment(_ context.Context, _ uuid.UUID, a *TicketAttachment) error {
	r.files = append(r.files, a)
	return nil
}

// A customer's email on a ticket waiting for them is a public customer comment, keeps its
// Message-ID for the reply thread, records its files and reopens the ticket, all in one change.
func TestAddInboundMessageThreadsAndReopens(t *testing.T) {
	repo := newInboundRepo()
	s := NewService(repo, zap.NewNop())
	tk := repo.addTicket("TKT-000007", StatusPendingCustomer)
	tk.Metadata[MetaEmailMessageIDs] = []any{"first@mail.example.com"}

	in := InboundMessage{
		TenantID: tk.TenantID, TicketID: tk.ID,
		Key:     MessageKey{Channel: ChannelEmail, ID: "second@mail.example.com"},
		From:    RequesterContact{Email: "amina@example.com"},
		Content: "<p>Here is the receipt</p>",
		Files:   []InboundFile{{Key: "email-provisioner/x/attachments/2026/11/r.pdf", Filename: "r.pdf"}},
	}
	c, err := s.AddInboundMessage(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthorType != AuthorCustomer || c.CommentType != CommentPublic || c.Metadata[CommentMetaExternalID] != "second@mail.example.com" {
		t.Fatalf("comment = %+v", c)
	}
	if c.Channel != "email" || c.Sender == nil || c.Sender.Email != "amina@example.com" {
		t.Fatalf("channel fields = %q %+v", c.Channel, c.Sender)
	}
	if got := threadIDs(repo.tickets[tk.ID].Metadata); len(got) != 2 || got[1] != "second@mail.example.com" {
		t.Fatalf("thread ids = %v", got)
	}
	if repo.tickets[tk.ID].Status != StatusOpen {
		t.Fatalf("status = %s, want open", repo.tickets[tk.ID].Status)
	}
	if len(repo.files) != 1 || repo.files[0].CommentID == nil || *repo.files[0].CommentID != c.ID || repo.files[0].Internal {
		t.Fatalf("files = %+v", repo.files)
	}
	// A customer comment never emails the customer; the reopen tells them where it stands.
	for _, typ := range repo.eventTypes() {
		if typ == EventTicketCommentAdded {
			t.Fatal("a customer comment must not publish ticket.comment_added")
		}
	}

	if _, err := s.AddInboundMessage(context.Background(), in); !errors.Is(err, ErrDuplicateMessage) {
		t.Fatalf("the same message twice: %v", err)
	}
}

// A ticket opened from a channel message carries the message key and its files, and every event
// carries what the reply email needs to thread.
func TestInboundCreateAndThreadPayload(t *testing.T) {
	repo := newInboundRepo()
	s := NewService(repo, zap.NewNop())
	tk, err := s.CreateTicket(context.Background(), &CreateTicketRequest{
		Subject: "Payout late", Description: "Where is it", Channel: ChannelEmail, RequesterEmail: "amina@example.com",
		Inbound: &InboundOrigin{Key: MessageKey{Channel: ChannelEmail, ID: "abc@mail.example.com"},
			Files: []InboundFile{{Key: "k/x/attachments/a.pdf", Filename: "a.pdf"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := repo.tickets[tk.ID].Metadata
	ref, _ := stored["source_ref"].(map[string]any)
	if stored["source"] != "email.inbound" || ref["type"] != "email_message" || ref["id"] != "abc@mail.example.com" {
		t.Fatalf("metadata = %v", stored)
	}
	if len(repo.files) != 1 || repo.files[0].CommentID != nil {
		t.Fatalf("files = %+v", repo.files)
	}

	p := TicketCreatedPayload(repo.tickets[tk.ID])
	if p["email_message_token"] != tk.ID.String() || p["email_thread_token"] != tk.ID.String() ||
		p["email_in_reply_to"] != "<abc@mail.example.com>" || p["email_subject_tag"] != "[TKT-0001]" ||
		p["email_reply_subject"] != "Re: Payout late [TKT-0001]" {
		t.Fatalf("payload = %v", p)
	}
	c := &TicketComment{ID: uuid.New()}
	if got := TicketCommentAddedPayload(repo.tickets[tk.ID], c, "", time.Now())["email_message_token"]; got != c.ID.String() {
		t.Fatalf("reply token = %v", got)
	}
}

func TestReplySubject(t *testing.T) {
	if got := ReplySubject("RE: re: Payout late [TKT-000042]", "TKT-000042"); got != "Re: Payout late [TKT-000042]" {
		t.Fatalf("got %q", got)
	}
}

// A channel message shows its channel, sender and (for email) subject from the comment
// metadata; an agent reply or a web comment has none of them.
func TestWithChannelMessage(t *testing.T) {
	email := withChannelMessage(&TicketComment{Metadata: map[string]any{
		CommentMetaChannel: "email", CommentMetaExternalID: "m-1@mail.example.com",
		CommentMetaFromName: "Amina", CommentMetaFromEmail: "amina@example.com", CommentMetaEmailSubject: "Re: [TKT-000007] Refund",
	}})
	if email.Channel != "email" || email.ExternalID != "m-1@mail.example.com" || email.EmailSubject != "Re: [TKT-000007] Refund" ||
		email.Sender == nil || email.Sender.Name != "Amina" || email.Sender.Email != "amina@example.com" {
		t.Fatalf("email = %+v %+v", email, email.Sender)
	}
	wa := withChannelMessage(&TicketComment{Metadata: map[string]any{
		CommentMetaChannel: "whatsapp", CommentMetaExternalID: "wamid.1", CommentMetaFromPhone: "+254712345678",
		CommentMetaEmailSubject: "ignored",
	}})
	if wa.Channel != "whatsapp" || wa.EmailSubject != "" || wa.Sender == nil || wa.Sender.Phone != "+254712345678" {
		t.Fatalf("whatsapp = %+v %+v", wa, wa.Sender)
	}
	old := withChannelMessage(&TicketComment{Metadata: map[string]any{CommentMetaChannel: "whatsapp", CommentMetaExternalID: "wamid.0"}})
	if old.Sender != nil {
		t.Fatalf("a message stored without a sender reports one: %+v", old.Sender)
	}
	agent := withChannelMessage(&TicketComment{AuthorType: AuthorAgent, Metadata: map[string]any{"macro_id": "x"}})
	if agent.Channel != "" || agent.Sender != nil || agent.ExternalID != "" {
		t.Fatalf("agent reply = %+v", agent)
	}
}
