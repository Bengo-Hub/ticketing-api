package tickets

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// fakeRepo keeps one tenant's tickets in memory; methods the tests do not reach panic (nil embed).
type fakeRepo struct {
	Repository
	tickets map[uuid.UUID]*Ticket
	agents  map[uuid.UUID]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{tickets: map[uuid.UUID]*Ticket{}, agents: map[uuid.UUID]string{}}
}

func (r *fakeRepo) GenerateTicketNumber(context.Context, uuid.UUID) (string, error) {
	return "TKT-0001", nil
}

func (r *fakeRepo) CreateTicket(_ context.Context, _ uuid.UUID, t *Ticket) error {
	t.CreatedAt = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	cp := *t
	r.tickets[t.ID] = &cp
	return nil
}

func (r *fakeRepo) GetTicket(_ context.Context, _ uuid.UUID, id uuid.UUID) (*Ticket, error) {
	t, ok := r.tickets[id]
	if !ok {
		return nil, ErrTicketNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *fakeRepo) UpdateTicketStatus(_ context.Context, _ uuid.UUID, id uuid.UUID, s TicketStatus) error {
	r.tickets[id].Status = s
	return nil
}

func (r *fakeRepo) CreateComment(context.Context, uuid.UUID, *TicketComment) error { return nil }
func (r *fakeRepo) MarkFirstResponse(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *fakeRepo) MarkSLABreached(context.Context, uuid.UUID, uuid.UUID, bool, bool) (bool, error) {
	return true, nil
}

func (r *fakeRepo) GetAgentEmail(_ context.Context, _ uuid.UUID, id uuid.UUID) (string, error) {
	return r.agents[id], nil
}

type sentEvent struct {
	eventType string
	data      map[string]any
}

// fakePublisher records events in order.
type fakePublisher struct{ sent []sentEvent }

func (p *fakePublisher) add(t string, d map[string]any) error {
	p.sent = append(p.sent, sentEvent{t, d})
	return nil
}
func (p *fakePublisher) PublishTicketCreated(_ context.Context, _ uuid.UUID, d map[string]any) error {
	return p.add("ticket.created", d)
}
func (p *fakePublisher) PublishTicketAssigned(_ context.Context, _ uuid.UUID, d map[string]any) error {
	return p.add("ticket.assigned", d)
}
func (p *fakePublisher) PublishTicketCommentAdded(_ context.Context, _ uuid.UUID, d map[string]any) error {
	return p.add("ticket.comment_added", d)
}
func (p *fakePublisher) PublishTicketStatusChanged(_ context.Context, _ uuid.UUID, d map[string]any) error {
	return p.add("ticket.status_changed", d)
}
func (p *fakePublisher) PublishTicketResolved(_ context.Context, _ uuid.UUID, d map[string]any) error {
	return p.add("ticket.resolved", d)
}
func (p *fakePublisher) PublishCRMActivityRequest(_ context.Context, _ uuid.UUID, d map[string]any) error {
	return p.add("crm.activity.log_requested", d)
}

func (p *fakePublisher) only(t *testing.T, eventType string) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, e := range p.sent {
		if e.eventType == eventType {
			found = append(found, e.data)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s published %d times (all: %v)", eventType, len(found), p.sent)
	}
	return found[0]
}

func newTestService() (*Service, *fakeRepo, *fakePublisher) {
	repo := newFakeRepo()
	pub := &fakePublisher{}
	s := NewService(repo, zap.NewNop())
	s.SetPublisher(pub)
	return s, repo, pub
}

func wantKeys(t *testing.T, name string, got map[string]any, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s = %v, want %v", name, k, got[k], v)
		}
	}
}

// Every create path stores the requester as requester_* and ticket.created carries it.
func TestTicketCreatedCarriesTheRequester(t *testing.T) {
	cases := []struct {
		name string
		req  CreateTicketRequest
		want map[string]any
	}{
		{
			name: "S2S fields",
			req: CreateTicketRequest{Subject: "Payout late", Description: "Where is it", Channel: ChannelWhatsApp,
				RequesterName: "Amina", RequesterEmail: " Amina@Example.com ", RequesterPhone: "+254 712 345-678",
				Metadata: map[string]any{"source": "vera"}},
			want: map[string]any{"requester_name": "Amina", "requester_email": "amina@example.com",
				"requester_phone": "+254712345678", "channel": "whatsapp", "source": "vera", "status": "new"},
		},
		{
			name: "public form",
			req: CreateTicketRequest{Subject: "Help", Description: "D", RequesterName: "Jo", RequesterEmail: "jo@example.com",
				Metadata: map[string]any{"source": "public_form"}},
			want: map[string]any{"requester_name": "Jo", "requester_email": "jo@example.com", "requester_phone": "", "channel": "web"},
		},
		{
			name: "legacy customer_* metadata is moved to requester_*",
			req: CreateTicketRequest{Subject: "Help", Description: "D",
				Metadata: map[string]any{"customer_email": "old@example.com", "customer_name": "Old"}},
			want: map[string]any{"requester_name": "Old", "requester_email": "old@example.com"},
		},
		{
			name: "no contact at all",
			req:  CreateTicketRequest{Subject: "Help", Description: "D"},
			want: map[string]any{"requester_email": "", "requester_phone": "", "requester_name": ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, pub := newTestService()
			tc.req.TenantID = uuid.New()
			ticket, err := s.CreateTicket(context.Background(), &tc.req)
			if err != nil {
				t.Fatal(err)
			}
			got := pub.only(t, "ticket.created")
			wantKeys(t, tc.name, got, tc.want)
			if got["ticket_number"] != "TKT-0001" || got["ticket_id"] != ticket.ID.String() || got["created_at"] != "2026-11-13T09:00:00Z" {
				t.Errorf("ticket fields: %v", got)
			}
			stored := repo.tickets[ticket.ID].Metadata
			if _, legacy := stored["customer_email"]; legacy {
				t.Errorf("legacy customer_email still stored: %v", stored)
			}
			if tc.want["requester_email"] != "" && stored["requester_email"] != tc.want["requester_email"] {
				t.Errorf("stored requester_email = %v", stored["requester_email"])
			}
		})
	}
}

// A status change tells the requester; resolved is its own event and carries the requester too.
func TestStatusEventsCarryTheRequester(t *testing.T) {
	s, _, pub := newTestService()
	ctx := context.Background()
	tenant := uuid.New()
	ticket, err := s.CreateTicket(ctx, &CreateTicketRequest{TenantID: tenant, Subject: "S", Description: "D",
		RequesterEmail: "guest@example.com", RequesterPhone: "+254700000001"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTicketStatus(ctx, tenant, ticket.ID, StatusOpen); err != nil {
		t.Fatal(err)
	}
	changed := pub.only(t, "ticket.status_changed")
	wantKeys(t, "status_changed", changed, map[string]any{"old_status": "new", "new_status": "open", "status": "open",
		"requester_email": "guest@example.com", "requester_phone": "+254700000001"})

	if err := s.UpdateTicketStatus(ctx, tenant, ticket.ID, StatusResolved); err != nil {
		t.Fatal(err)
	}
	resolved := pub.only(t, "ticket.resolved")
	wantKeys(t, "resolved", resolved, map[string]any{"old_status": "open", "new_status": "resolved", "status": "resolved",
		"requester_email": "guest@example.com", "requester_phone": "+254700000001", "ticket_number": "TKT-0001"})
	if resolved["resolved_at"] == "" || resolved["resolved_at"] == nil {
		t.Error("resolved_at missing")
	}
	// Resolving does not also publish a status_changed.
	pub.only(t, "ticket.status_changed")
}

// Only a public agent reply produces ticket.comment_added, with the agent's email.
func TestCommentAddedOnlyForPublicAgentReplies(t *testing.T) {
	s, repo, pub := newTestService()
	ctx := context.Background()
	tenant := uuid.New()
	agent := uuid.New()
	repo.agents[agent] = "agent@shop.co.ke"
	ticket, err := s.CreateTicket(ctx, &CreateTicketRequest{TenantID: tenant, Subject: "S", Description: "D", RequesterEmail: "guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []AddCommentRequest{
		{AuthorID: agent, AuthorType: AuthorAgent, CommentType: CommentInternal, Content: "internal note"},
		{AuthorID: uuid.New(), AuthorType: AuthorCustomer, CommentType: CommentPublic, Content: "customer says"},
		{AuthorID: agent, AuthorType: AuthorAgent, CommentType: CommentPublic, Content: "We have sent it"},
	} {
		c.TenantID, c.TicketID = tenant, ticket.ID
		if _, err := s.AddComment(ctx, &c); err != nil {
			t.Fatal(err)
		}
	}
	got := pub.only(t, "ticket.comment_added")
	wantKeys(t, "comment_added", got, map[string]any{"comment": "We have sent it", "author_type": "agent",
		"agent_email": "agent@shop.co.ke", "requester_email": "guest@example.com", "ticket_number": "TKT-0001"})
}

func TestTicketAssignedPayload(t *testing.T) {
	agent := uuid.New()
	tk := &Ticket{ID: uuid.New(), TicketNumber: "TKT-7", Subject: "S", Status: StatusOpen, Priority: PriorityHigh,
		Metadata: map[string]any{"requester_email": "guest@example.com"}}
	got := TicketAssignedPayload(tk, &agent, "agent@shop.co.ke")
	wantKeys(t, "assigned", got, map[string]any{"agent_id": agent.String(), "agent_email": "agent@shop.co.ke",
		"priority": "high", "requester_email": "guest@example.com"})
}

// SLA events carry the assigned agent's email and never the requester's contact.
func TestSLABreachPayload(t *testing.T) {
	agent, team := uuid.New(), uuid.New()
	tk := &Ticket{ID: uuid.New(), TicketNumber: "TKT-9", Subject: "S", Priority: PriorityUrgent, Status: StatusOpen,
		AssignedAgentID: &agent, AssignedTeamID: &team, Metadata: map[string]any{"requester_email": "guest@example.com"}}
	got := SLABreachPayload(tk, "agent@shop.co.ke", false, true)
	wantKeys(t, "sla", got, map[string]any{"event_type": "sla.resolution_breached", "agent_id": agent.String(),
		"agent_email": "agent@shop.co.ke", "team_id": team.String(), "resolution_breached": true, "response_breached": false})
	if _, ok := got["requester_email"]; ok {
		t.Error("an SLA event must not carry the requester's contact")
	}
	if first := SLABreachPayload(&Ticket{ID: uuid.New()}, "", true, false); first["event_type"] != "sla.first_response_breached" || first["agent_id"] != "" {
		t.Errorf("first response: %v", first)
	}
}

func TestRequesterOfReadsLegacyKeys(t *testing.T) {
	tk := &Ticket{Metadata: map[string]any{"customer_email": "Old@Example.com", "customer_name": "Old"}}
	if got := RequesterOf(tk); got.Email != "old@example.com" || got.Name != "Old" {
		t.Fatalf("RequesterOf = %+v", got)
	}
	if got := RequesterOf(&Ticket{Metadata: map[string]any{"requester_phone": "12"}}); got.Phone != "" {
		t.Fatalf("a too-short phone must read as empty: %+v", got)
	}
}

func TestCanReplyAsRequester(t *testing.T) {
	tk := &Ticket{Metadata: map[string]any{"requester_email": "guest@example.com"}}
	if !canReplyAsRequester(tk, "Guest@Example.com") {
		t.Error("the requester must be able to reply")
	}
	if canReplyAsRequester(tk, "other@example.com") {
		t.Error("another email must be refused")
	}
	if canReplyAsRequester(&Ticket{Metadata: map[string]any{"requester_phone": "+254700000001"}}, "any@example.com") {
		t.Error("a ticket with no requester email takes no public replies")
	}
	if !canReplyAsRequester(&Ticket{Metadata: map[string]any{"customer_email": "old@example.com"}}, "old@example.com") {
		t.Error("older public tickets (customer_email) still accept their requester")
	}
}

func TestDefaultRequesterToCaller(t *testing.T) {
	user := uuid.New()
	self := CreateTicketRequest{}
	defaultRequesterToCaller(&self, user.String(), "me@shop.co.ke")
	if self.RequesterEmail != "me@shop.co.ke" {
		t.Errorf("own ticket: %q", self.RequesterEmail)
	}
	own := CreateTicketRequest{CustomerID: user}
	defaultRequesterToCaller(&own, user.String(), "me@shop.co.ke")
	if own.RequesterEmail != "me@shop.co.ke" {
		t.Errorf("own customer_id: %q", own.RequesterEmail)
	}
	forOther := CreateTicketRequest{CustomerID: uuid.New()}
	defaultRequesterToCaller(&forOther, user.String(), "agent@shop.co.ke")
	if forOther.RequesterEmail != "" {
		t.Errorf("an agent raising a ticket for someone else must not become the requester: %q", forOther.RequesterEmail)
	}
	named := CreateTicketRequest{RequesterEmail: "guest@example.com"}
	defaultRequesterToCaller(&named, user.String(), "me@shop.co.ke")
	if named.RequesterEmail != "guest@example.com" {
		t.Errorf("a named requester must be kept: %q", named.RequesterEmail)
	}
}
