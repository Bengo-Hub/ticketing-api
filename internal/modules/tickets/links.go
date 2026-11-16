package tickets

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// LinkKind is how two tickets relate. A parent link runs from the parent to the child.
type LinkKind string

const (
	LinkParent    LinkKind = "parent"
	LinkRelated   LinkKind = "related"
	LinkDuplicate LinkKind = "duplicate"
)

// TicketLink is one stored link.
type TicketLink struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"-"`
	FromTicketID uuid.UUID `json:"from_ticket_id"`
	ToTicketID   uuid.UUID `json:"to_ticket_id"`
	Kind         LinkKind  `json:"kind"`
	CreatedBy    uuid.UUID `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// LinkedTicket is the other end of a link as GET /tickets/{id}/links shows it.
type LinkedTicket struct {
	ID           uuid.UUID    `json:"id"`
	TicketNumber string       `json:"ticket_number"`
	Subject      string       `json:"subject"`
	Status       TicketStatus `json:"status"`
}

// LinkView is a link seen from one ticket. Relation is parent (the other ticket is this one's
// parent), child, related or duplicate.
type LinkView struct {
	TicketLink
	Relation string        `json:"relation"`
	Ticket   *LinkedTicket `json:"ticket,omitempty"`
}

// CreateLinkRequest is the body of POST /tickets/{id}/links. Kind is from the URL ticket's side:
// parent makes it the parent of ticket_id, child makes it the child, related and duplicate
// relate the two.
type CreateLinkRequest struct {
	TicketID uuid.UUID `json:"ticket_id"`
	Kind     string    `json:"kind"`
}

// maxLinksPerTicket bounds GET /tickets/{id}/links.
const maxLinksPerTicket = 100

// linkFor turns a request on ticketID into the stored link (from, to, kind).
func linkFor(ticketID uuid.UUID, req CreateLinkRequest) (*TicketLink, error) {
	if req.TicketID == uuid.Nil || req.TicketID == ticketID {
		return nil, ErrLinkInvalid
	}
	l := &TicketLink{ID: uuid.New(), FromTicketID: ticketID, ToTicketID: req.TicketID}
	switch req.Kind {
	case string(LinkParent):
		l.Kind = LinkParent
	case "child":
		l.Kind, l.FromTicketID, l.ToTicketID = LinkParent, req.TicketID, ticketID
	case string(LinkRelated):
		l.Kind = LinkRelated
	case string(LinkDuplicate):
		l.Kind = LinkDuplicate
	default:
		return nil, ErrLinkInvalid
	}
	return l, nil
}

// relationFrom names a link from the side of ticketID.
func relationFrom(l *TicketLink, ticketID uuid.UUID) (string, uuid.UUID) {
	other := l.ToTicketID
	if l.ToTicketID == ticketID {
		other = l.FromTicketID
	}
	if l.Kind != LinkParent {
		return string(l.Kind), other
	}
	if l.FromTicketID == ticketID {
		return "child", other
	}
	return "parent", other
}

// LinkTickets links the URL ticket to another of the tenant. A parent link is refused when the
// reverse parent link exists, so a ticket is never its own ancestor in one step.
func (s *Service) LinkTickets(ctx context.Context, tenantID, ticketID, actorID uuid.UUID, req CreateLinkRequest) (*TicketLink, error) {
	l, err := linkFor(ticketID, req)
	if err != nil {
		return nil, err
	}
	l.TenantID, l.CreatedBy = tenantID, actorID
	if _, err := s.repo.GetTicket(ctx, tenantID, l.FromTicketID); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetTicket(ctx, tenantID, l.ToTicketID); err != nil {
		return nil, err
	}
	if l.Kind == LinkParent {
		existing, err := s.repo.ListLinks(ctx, tenantID, l.ToTicketID, maxLinksPerTicket)
		if err != nil {
			return nil, err
		}
		for _, e := range existing {
			if e.Kind == LinkParent && e.FromTicketID == l.ToTicketID && e.ToTicketID == l.FromTicketID {
				return nil, ErrLinkInvalid
			}
		}
	}
	if err := s.repo.CreateLink(ctx, tenantID, l); err != nil {
		return nil, err
	}
	s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": ticketID})
	return l, nil
}

// ListLinks returns the ticket's links with a summary of each other ticket. A link whose other
// ticket was deleted is left out.
func (s *Service) ListLinks(ctx context.Context, tenantID, ticketID uuid.UUID) ([]LinkView, error) {
	links, err := s.repo.ListLinks(ctx, tenantID, ticketID, maxLinksPerTicket)
	if err != nil {
		return nil, err
	}
	out := make([]LinkView, 0, len(links))
	for _, l := range links {
		rel, otherID := relationFrom(l, ticketID)
		other, err := s.repo.GetTicket(ctx, tenantID, otherID)
		if err == ErrTicketNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, LinkView{TicketLink: *l, Relation: rel, Ticket: &LinkedTicket{
			ID: other.ID, TicketNumber: other.TicketNumber, Subject: other.Subject, Status: other.Status,
		}})
	}
	return out, nil
}

// UnlinkTickets removes a link of the ticket.
func (s *Service) UnlinkTickets(ctx context.Context, tenantID, ticketID, linkID uuid.UUID) error {
	if err := s.repo.DeleteLink(ctx, tenantID, ticketID, linkID); err != nil {
		return err
	}
	s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": ticketID})
	return nil
}
