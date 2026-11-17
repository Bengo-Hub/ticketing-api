package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// fakeTickets keeps tickets in memory and records what the ingest asked for.
type fakeTickets struct {
	ingested  map[string]bool
	byMessage map[string]*tickets.Ticket
	byID      map[uuid.UUID]*tickets.Ticket
	byNumber  map[string]*tickets.Ticket
	latest    *tickets.Ticket
	since     time.Time
	created   []*tickets.CreateTicketRequest
	added     []tickets.InboundMessage
	createErr error
	addErr    error
}

func newFakeTickets() *fakeTickets {
	return &fakeTickets{ingested: map[string]bool{}, byMessage: map[string]*tickets.Ticket{},
		byID: map[uuid.UUID]*tickets.Ticket{}, byNumber: map[string]*tickets.Ticket{}}
}

func (f *fakeTickets) MessageIngested(_ context.Context, _ uuid.UUID, k tickets.MessageKey) (bool, error) {
	return f.ingested[string(k.Channel)+"|"+k.ID], nil
}

func (f *fakeTickets) TicketForMessage(_ context.Context, _ uuid.UUID, k tickets.MessageKey) (*tickets.Ticket, error) {
	return f.byMessage[k.ID], nil
}

func (f *fakeTickets) TicketForOwnID(_ context.Context, _ uuid.UUID, id uuid.UUID) (*tickets.Ticket, error) {
	return f.byID[id], nil
}

func (f *fakeTickets) GetTicketByNumber(_ context.Context, _ uuid.UUID, n string) (*tickets.Ticket, error) {
	if t, ok := f.byNumber[n]; ok {
		return t, nil
	}
	return nil, tickets.ErrTicketNotFound
}

func (f *fakeTickets) LatestChannelTicket(_ context.Context, _ uuid.UUID, _ tickets.TicketChannel, phone string, since time.Time) (*tickets.Ticket, error) {
	f.since = since
	if f.latest != nil && tickets.RequesterOf(f.latest).Phone == phone {
		return f.latest, nil
	}
	return nil, nil
}

func (f *fakeTickets) CreateTicket(_ context.Context, req *tickets.CreateTicketRequest) (*tickets.Ticket, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, req)
	return &tickets.Ticket{ID: uuid.New(), TicketNumber: "TKT-000099", Status: tickets.StatusNew}, nil
}

func (f *fakeTickets) AddInboundMessage(_ context.Context, in tickets.InboundMessage) (*tickets.TicketComment, error) {
	if f.addErr != nil {
		return nil, f.addErr
	}
	f.added = append(f.added, in)
	return &tickets.TicketComment{ID: uuid.New(), TicketID: in.TicketID}, nil
}

// ticket is a stored ticket of the given number, status and requester.
func (f *fakeTickets) ticket(number string, status tickets.TicketStatus, email, phone string) *tickets.Ticket {
	meta := map[string]any{}
	if email != "" {
		meta[tickets.MetaRequesterEmail] = email
	}
	if phone != "" {
		meta[tickets.MetaRequesterPhone] = phone
	}
	t := &tickets.Ticket{ID: uuid.New(), TicketNumber: number, Status: status, Metadata: meta}
	f.byID[t.ID] = t
	f.byNumber[number] = t
	return t
}

func newTestService(f *fakeTickets) *Service {
	s := NewService(f, zap.NewNop())
	s.now = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }
	return s
}

var tenant = uuid.MustParse("6f1d7a52-2a8e-4d55-9a43-0b0c1e2f3a4b")

func emailMsg(id string) Message {
	return Message{TenantID: tenant, Channel: tickets.ChannelEmail, ExternalID: id,
		Requester: tickets.RequesterContact{Name: "Amina", Email: "amina@example.com"},
		Subject:   "Payout late", Text: "Where is my payout?"}
}

// Threading order: our own Message-ID, then a received Message-ID, then the subject tag (only for
// the requester), else a new ticket.
func TestEmailThreadingOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("own Message-ID wins over the subject tag", func(t *testing.T) {
		f := newFakeTickets()
		own := f.ticket("TKT-000001", tickets.StatusOpen, "amina@example.com", "")
		f.ticket("TKT-000002", tickets.StatusOpen, "amina@example.com", "")
		m := emailMsg("reply-1@mail.example.com")
		m.ThreadRefs = []string{own.ID.String() + "@tenant.example.com"}
		m.TicketNumber = "TKT-000002"
		res, err := newTestService(f).Ingest(ctx, m)
		if err != nil || res.Outcome != OutcomeThreaded || res.TicketID != own.ID {
			t.Fatalf("got %+v %v, want threaded on %s", res, err, own.TicketNumber)
		}
	})

	t.Run("a received Message-ID in References", func(t *testing.T) {
		f := newFakeTickets()
		first := f.ticket("TKT-000003", tickets.StatusPendingCustomer, "amina@example.com", "")
		f.byMessage["original@mail.example.com"] = first
		m := emailMsg("reply-2@mail.example.com")
		m.ThreadRefs = []string{"unknown@else.example.com", "original@mail.example.com"}
		res, err := newTestService(f).Ingest(ctx, m)
		if err != nil || res.Outcome != OutcomeThreaded || res.TicketID != first.ID {
			t.Fatalf("got %+v %v", res, err)
		}
		if got := f.added[0]; got.Key.ID != "reply-2@mail.example.com" || got.Key.Channel != tickets.ChannelEmail {
			t.Fatalf("comment key = %+v", got.Key)
		}
	})

	t.Run("subject tag from the requester", func(t *testing.T) {
		f := newFakeTickets()
		tagged := f.ticket("TKT-000004", tickets.StatusOpen, "amina@example.com", "")
		m := emailMsg("reply-3@mail.example.com")
		m.TicketNumber = "TKT-000004"
		res, _ := newTestService(f).Ingest(ctx, m)
		if res.Outcome != OutcomeThreaded || res.TicketID != tagged.ID {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("subject tag from someone else opens a new ticket", func(t *testing.T) {
		f := newFakeTickets()
		f.ticket("TKT-000005", tickets.StatusOpen, "owner@example.com", "")
		m := emailMsg("reply-4@mail.example.com")
		m.TicketNumber = "TKT-000005"
		res, _ := newTestService(f).Ingest(ctx, m)
		if res.Outcome != OutcomeCreated || len(f.added) != 0 {
			t.Fatalf("a guessed tag must not thread: %+v", res)
		}
	})

	t.Run("no hint opens a ticket through CreateTicket", func(t *testing.T) {
		f := newFakeTickets()
		res, err := newTestService(f).Ingest(ctx, emailMsg("new@mail.example.com"))
		if err != nil || res.Outcome != OutcomeCreated || len(f.created) != 1 {
			t.Fatalf("got %+v %v", res, err)
		}
		req := f.created[0]
		if req.Channel != tickets.ChannelEmail || req.RequesterEmail != "amina@example.com" || req.Subject != "Payout late" {
			t.Fatalf("create request = %+v", req)
		}
		if req.Inbound == nil || req.Inbound.Key.ID != "new@mail.example.com" {
			t.Fatalf("the message key must travel with the create: %+v", req.Inbound)
		}
		if req.CustomerID != tickets.CustomerIDFor(tickets.RequesterContact{Email: "amina@example.com"}) {
			t.Fatal("customer id must derive from the requester")
		}
	})
}

// A reply to a closed ticket opens a follow-up; a resolved ticket takes the comment (and reopens).
func TestCreateVersusComment(t *testing.T) {
	ctx := context.Background()
	f := newFakeTickets()
	closed := f.ticket("TKT-000010", tickets.StatusClosed, "amina@example.com", "")
	m := emailMsg("after-close@mail.example.com")
	m.ThreadRefs = []string{closed.ID.String() + "@x.example.com"}
	res, _ := newTestService(f).Ingest(ctx, m)
	if res.Outcome != OutcomeCreated || f.created[0].Metadata[tickets.MetaFollowUpOf] != "TKT-000010" {
		t.Fatalf("closed ticket: %+v %+v", res, f.created)
	}

	resolved := f.ticket("TKT-000011", tickets.StatusResolved, "amina@example.com", "")
	m = emailMsg("after-resolve@mail.example.com")
	m.ThreadRefs = []string{resolved.ID.String() + "@x.example.com"}
	res, _ = newTestService(f).Ingest(ctx, m)
	if res.Outcome != OutcomeThreaded || res.TicketID != resolved.ID {
		t.Fatalf("resolved ticket: %+v", res)
	}
}

// A redelivered message is stored once, whichever check catches it.
func TestDedupe(t *testing.T) {
	ctx := context.Background()

	f := newFakeTickets()
	f.ingested["email|seen@mail.example.com"] = true
	res, _ := newTestService(f).Ingest(ctx, emailMsg("seen@mail.example.com"))
	if res.Outcome != OutcomeDuplicate || len(f.created)+len(f.added) != 0 {
		t.Fatalf("already ingested: %+v", res)
	}

	f = newFakeTickets()
	f.createErr = tickets.ErrDuplicateSourceRef
	res, err := newTestService(f).Ingest(ctx, emailMsg("race@mail.example.com"))
	if err != nil || res.Outcome != OutcomeDuplicate {
		t.Fatalf("create race: %+v %v", res, err)
	}

	f = newFakeTickets()
	open := f.ticket("TKT-000020", tickets.StatusOpen, "amina@example.com", "")
	f.addErr = tickets.ErrDuplicateMessage
	m := emailMsg("race2@mail.example.com")
	m.TicketNumber = open.TicketNumber
	res, err = newTestService(f).Ingest(ctx, m)
	if err != nil || res.Outcome != OutcomeDuplicate {
		t.Fatalf("comment race: %+v %v", res, err)
	}
}

// A refused message (auto-reply, STOP) opens and continues nothing, even with a matching ticket.
func TestRefusedMessageChangesNothing(t *testing.T) {
	f := newFakeTickets()
	open := f.ticket("TKT-000030", tickets.StatusOpen, "amina@example.com", "")
	m := emailMsg("ooo@mail.example.com")
	m.TicketNumber = open.TicketNumber
	m.Refusal = "auto-submitted"
	res, err := newTestService(f).Ingest(context.Background(), m)
	if err != nil || res.Outcome != OutcomeRefused || len(f.created)+len(f.added) != 0 {
		t.Fatalf("got %+v %v", res, err)
	}
}

// A message with no tenant, id or sender can never succeed, so it is permanent.
func TestInvalidMessageIsPermanent(t *testing.T) {
	m := emailMsg("")
	if _, err := newTestService(newFakeTickets()).Ingest(context.Background(), m); !IsPermanent(err) {
		t.Fatalf("want a permanent error, got %v", err)
	}
}

// WhatsApp: off threads onto an open conversation but never opens one; on opens one; the window
// passed to the lookup is the configured one.
func TestWhatsAppOptInGating(t *testing.T) {
	ctx := context.Background()
	event := WhatsAppInbound{TenantID: tenant.String(), WAMessageID: "wamid.1", From: "254712345678", Type: "text",
		Text: "My order has not arrived", ContactName: "Otieno"}

	off, ok := WhatsAppMessage(event, false)
	if !ok || off.Requester.Phone != "+254712345678" || !off.ThreadOnly {
		t.Fatalf("message = %+v", off)
	}
	f := newFakeTickets()
	res, _ := newTestService(f).Ingest(ctx, off)
	if res.Outcome != OutcomeSkipped || len(f.created) != 0 {
		t.Fatalf("auto-ticketing off must open nothing: %+v", res)
	}

	f = newFakeTickets()
	f.latest = f.ticket("TKT-000040", tickets.StatusOpen, "", "+254712345678")
	svc := newTestService(f)
	res, _ = svc.Ingest(ctx, off)
	if res.Outcome != OutcomeThreaded || res.TicketID != f.latest.ID {
		t.Fatalf("an open conversation continues even when off: %+v", res)
	}
	if want := svc.now().Add(-DefaultThreadWindow); !f.since.Equal(want) {
		t.Fatalf("window since = %v, want %v", f.since, want)
	}

	on, _ := WhatsAppMessage(event, true)
	f = newFakeTickets()
	res, _ = newTestService(f).Ingest(ctx, on)
	if res.Outcome != OutcomeCreated || f.created[0].Channel != tickets.ChannelWhatsApp || f.created[0].Subject != "My order has not arrived" {
		t.Fatalf("auto-ticketing on: %+v %+v", res, f.created)
	}
	if f.created[0].Inbound.Key.Source() != "notifications.whatsapp" {
		t.Fatal("WhatsApp tickets share the notifications inbox source so the two paths dedupe")
	}

	stop, _ := WhatsAppMessage(WhatsAppInbound{TenantID: tenant.String(), WAMessageID: "wamid.2", From: "254712345678", Text: " Stop "}, true)
	if stop.Refusal == "" {
		t.Fatal("a STOP reply must be refused")
	}
	if _, ok := WhatsAppMessage(WhatsAppInbound{TenantID: "nope", WAMessageID: "w", From: "254712345678"}, true); ok {
		t.Fatal("an event without a tenant is invalid")
	}
}
