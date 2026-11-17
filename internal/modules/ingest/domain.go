// Package ingest turns customer messages from every channel (email through email-provisioner,
// WhatsApp through notifications-api) into tickets. One service decides for every channel: a
// message continues the requester's matching open ticket as a public customer comment, or opens
// a ticket through the tickets service's CreateTicket, so numbering, routing, SLA and the outbox
// events apply exactly as for any other ticket. A message is stored once however often it is
// delivered, and an auto-reply never opens or continues anything.
package ingest

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// Tickets is the part of the tickets service the ingest uses. The finders return nil, nil when
// nothing matches.
type Tickets interface {
	MessageIngested(ctx context.Context, tenantID uuid.UUID, key tickets.MessageKey) (bool, error)
	TicketForMessage(ctx context.Context, tenantID uuid.UUID, key tickets.MessageKey) (*tickets.Ticket, error)
	TicketForOwnID(ctx context.Context, tenantID, id uuid.UUID) (*tickets.Ticket, error)
	// GetTicketByNumber returns tickets.ErrTicketNotFound when there is no such ticket.
	GetTicketByNumber(ctx context.Context, tenantID uuid.UUID, number string) (*tickets.Ticket, error)
	LatestChannelTicket(ctx context.Context, tenantID uuid.UUID, channel tickets.TicketChannel, phone string, since time.Time) (*tickets.Ticket, error)
	CreateTicket(ctx context.Context, req *tickets.CreateTicketRequest) (*tickets.Ticket, error)
	AddInboundMessage(ctx context.Context, in tickets.InboundMessage) (*tickets.TicketComment, error)
}

// Message is one inbound customer message, already read from its channel's event.
type Message struct {
	TenantID uuid.UUID
	Channel  tickets.TicketChannel
	// ExternalID is the email Message-ID (without angle brackets) or the WhatsApp message id.
	ExternalID string
	// ThreadRefs are the email Message-IDs the message answers: In-Reply-To first, then
	// References from the newest back.
	ThreadRefs []string
	// TicketNumber is the ticket named by a [TKT-n] subject tag, normalised (TKT-000042).
	TicketNumber string
	Requester    tickets.RequesterContact
	Subject      string
	Text         string
	HTML         string
	Attachments  []tickets.InboundFile
	// Refusal, set by the channel adapter, says why the message must not open or continue a
	// ticket (an auto-reply, a bounce, a no-reply sender, spam, a STOP reply).
	Refusal string
	// ThreadOnly continues an open ticket but never opens one (WhatsApp with auto-ticketing off).
	ThreadOnly bool
	// Extra is channel detail kept on the comment, or on a new ticket's metadata.
	Extra      map[string]any
	ReceivedAt time.Time
}

// Outcome is what the ingest did with a message.
type Outcome string

const (
	OutcomeCreated   Outcome = "created"
	OutcomeThreaded  Outcome = "threaded"
	OutcomeDuplicate Outcome = "duplicate"
	OutcomeRefused   Outcome = "refused"
	OutcomeSkipped   Outcome = "skipped"
)

// Result reports the outcome and the ticket concerned.
type Result struct {
	Outcome      Outcome
	TicketID     uuid.UUID
	TicketNumber string
	Reason       string
}

// ErrInvalidMessage is a message that can never be ingested (no tenant, no id, no sender).
var ErrInvalidMessage = errors.New("ingest: invalid message")

// permanentError marks a failure that retrying cannot fix; the consumer acknowledges it.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent wraps err so the consumer acknowledges the message instead of retrying it.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether retrying the message cannot help.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p) || errors.Is(err, ErrInvalidMessage)
}
