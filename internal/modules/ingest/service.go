package ingest

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// DefaultThreadWindow is how long a WhatsApp conversation stays on one ticket: a message from
// the same phone continues the latest open WhatsApp ticket active in this window.
const DefaultThreadWindow = 7 * 24 * time.Hour

// maxThreadRefs bounds the Message-ID lookups one email can cause.
const maxThreadRefs = 20

// Service decides, for every channel, whether a message continues a ticket or opens one.
type Service struct {
	tickets      Tickets
	contacts     Contacts
	threadWindow time.Duration
	logger       *zap.Logger
	now          func() time.Time
}

// NewService creates the ingest service over the tickets service.
func NewService(t Tickets, logger *zap.Logger) *Service {
	return &Service{tickets: t, threadWindow: DefaultThreadWindow, logger: logger.Named("ingest"), now: time.Now}
}

// SetContacts links each new ticket's requester to their marketflow contact. Without it a ticket
// carries the requester fields only.
func (s *Service) SetContacts(c Contacts) { s.contacts = c }

// SetThreadWindow changes the WhatsApp conversation window (zero keeps the default).
func (s *Service) SetThreadWindow(d time.Duration) {
	if d > 0 {
		s.threadWindow = d
	}
}

// Ingest stores one message: a duplicate and a refused message change nothing, a message that
// continues an open ticket becomes a public customer comment on it, and any other message opens a
// ticket (unless ThreadOnly). A reply to a closed or cancelled ticket opens a follow-up ticket.
func (s *Service) Ingest(ctx context.Context, m Message) (Result, error) {
	m.ExternalID = strings.TrimSpace(m.ExternalID)
	if m.TenantID == uuid.Nil || m.ExternalID == "" || (m.Requester.Email == "" && m.Requester.Phone == "") {
		return Result{}, ErrInvalidMessage
	}
	if m.Refusal != "" {
		return Result{Outcome: OutcomeRefused, Reason: m.Refusal}, nil
	}
	key := tickets.MessageKey{Channel: m.Channel, ID: m.ExternalID}
	seen, err := s.tickets.MessageIngested(ctx, m.TenantID, key)
	if err != nil {
		return Result{}, err
	}
	if seen {
		return Result{Outcome: OutcomeDuplicate}, nil
	}
	target, err := s.findThread(ctx, m)
	if err != nil {
		return Result{}, err
	}
	if target != nil && continues(target.Status) {
		return s.thread(ctx, m, key, target)
	}
	if m.ThreadOnly {
		return Result{Outcome: OutcomeSkipped, Reason: "auto-ticketing is off"}, nil
	}
	return s.create(ctx, m, key, target)
}

// continues reports whether a ticket in this status takes the customer's message as a comment.
// Closed and cancelled tickets are final: a reply opens a follow-up ticket instead.
func continues(status tickets.TicketStatus) bool {
	return status != tickets.StatusClosed && status != tickets.StatusCancelled
}

// findThread finds the ticket a message belongs to, or nil. Email: a Message-ID ticketing sent
// or received, then the [TKT-n] tag when the sender is the ticket's requester. WhatsApp: the
// sender's latest open WhatsApp ticket within the conversation window.
func (s *Service) findThread(ctx context.Context, m Message) (*tickets.Ticket, error) {
	switch m.Channel {
	case tickets.ChannelEmail:
		refs := m.ThreadRefs
		if len(refs) > maxThreadRefs {
			refs = refs[:maxThreadRefs]
		}
		for _, ref := range refs {
			t, err := s.ticketForRef(ctx, m.TenantID, ref)
			if t != nil || err != nil {
				return t, err
			}
		}
		if m.TicketNumber == "" {
			return nil, nil
		}
		t, err := s.tickets.GetTicketByNumber(ctx, m.TenantID, m.TicketNumber)
		if errors.Is(err, tickets.ErrTicketNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		// A subject tag is guessable, so it threads only the requester's own mail.
		if !strings.EqualFold(tickets.RequesterOf(t).Email, m.Requester.Email) {
			return nil, nil
		}
		return t, nil
	case tickets.ChannelWhatsApp:
		return s.tickets.LatestChannelTicket(ctx, m.TenantID, tickets.ChannelWhatsApp, m.Requester.Phone, s.now().Add(-s.threadWindow))
	}
	return nil, nil
}

// ticketForRef resolves one Message-ID: one of our own (its local part is a ticket or comment
// id), or a customer email already on a ticket.
func (s *Service) ticketForRef(ctx context.Context, tenantID uuid.UUID, ref string) (*tickets.Ticket, error) {
	if id, ok := OwnToken(ref); ok {
		t, err := s.tickets.TicketForOwnID(ctx, tenantID, id)
		if t != nil || err != nil {
			return t, err
		}
	}
	return s.tickets.TicketForMessage(ctx, tenantID, tickets.MessageKey{Channel: tickets.ChannelEmail, ID: ref})
}

func (s *Service) thread(ctx context.Context, m Message, key tickets.MessageKey, t *tickets.Ticket) (Result, error) {
	_, err := s.tickets.AddInboundMessage(ctx, tickets.InboundMessage{
		TenantID: m.TenantID, TicketID: t.ID, Key: key, From: m.Requester,
		Content: messageBody(m, true), Metadata: commentExtra(s.extra(m), m), Files: m.Attachments,
		PhoneNumberID: m.PhoneNumberID, ReceivedAt: m.ReceivedAt,
	})
	if errors.Is(err, tickets.ErrDuplicateMessage) {
		return Result{Outcome: OutcomeDuplicate, TicketID: t.ID, TicketNumber: t.TicketNumber}, nil
	}
	if err != nil {
		return Result{}, err
	}
	return Result{Outcome: OutcomeThreaded, TicketID: t.ID, TicketNumber: t.TicketNumber}, nil
}

func (s *Service) create(ctx context.Context, m Message, key tickets.MessageKey, followUp *tickets.Ticket) (Result, error) {
	meta := s.extra(m)
	if followUp != nil {
		meta[tickets.MetaFollowUpOf] = followUp.TicketNumber
	}
	req := &tickets.CreateTicketRequest{
		TenantID:       m.TenantID,
		CustomerID:     tickets.CustomerIDFor(m.Requester),
		Subject:        ticketSubject(m),
		Description:    messageBody(m, false),
		Channel:        m.Channel,
		Metadata:       meta,
		RequesterName:  m.Requester.Name,
		RequesterEmail: m.Requester.Email,
		RequesterPhone: m.Requester.Phone,
		CRMContactID:   s.contactID(ctx, m),
		Inbound: &tickets.InboundOrigin{Key: key, Files: m.Attachments,
			PhoneNumberID: m.PhoneNumberID, ReceivedAt: m.ReceivedAt},
	}
	t, err := s.tickets.CreateTicket(ctx, req)
	if errors.Is(err, tickets.ErrDuplicateSourceRef) {
		return Result{Outcome: OutcomeDuplicate}, nil
	}
	if err != nil {
		return Result{}, err
	}
	return Result{Outcome: OutcomeCreated, TicketID: t.ID, TicketNumber: t.TicketNumber}, nil
}

// contactID resolves the requester to their marketflow contact through the tickets package's
// shared resolver; a failed lookup never holds the ticket back.
func (s *Service) contactID(ctx context.Context, m Message) *uuid.UUID {
	if s.contacts == nil {
		return nil
	}
	return tickets.ResolveContact(ctx, s.contacts, s.logger, m.TenantID, string(m.Channel),
		m.Requester.Name, m.Requester.Email, m.Requester.Phone)
}

// maxCommentSubject bounds the email subject kept on a threaded comment.
const maxCommentSubject = 300

// commentExtra adds what the agent's thread shows of a message that continues a ticket: for
// email, the subject the customer wrote under (the ticket keeps its own subject).
func commentExtra(meta map[string]any, m Message) map[string]any {
	if m.Channel != tickets.ChannelEmail {
		return meta
	}
	subject := strings.Join(strings.Fields(m.Subject), " ")
	if r := []rune(subject); len(r) > maxCommentSubject {
		subject = string(r[:maxCommentSubject])
	}
	if subject != "" {
		meta[tickets.CommentMetaEmailSubject] = subject
	}
	return meta
}

// extra copies the channel detail so the caller's map is never shared with a stored row.
func (s *Service) extra(m Message) map[string]any {
	out := make(map[string]any, len(m.Extra)+1)
	for k, v := range m.Extra {
		out[k] = v
	}
	if !m.ReceivedAt.IsZero() {
		out["received_at"] = m.ReceivedAt.UTC().Format(time.RFC3339)
	}
	return out
}
