package tickets

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SLALabel names an SLA policy and the business calendar its clock counts in (empty for 24x7).
type SLALabel struct {
	PolicyName   string
	CalendarName string
}

// SystemAuthorName is the author name of a system comment.
const SystemAuthorName = "System"

// withSLALabels sets the SLA policy and calendar names on every ticket that has a policy, with
// one lookup for the whole page. A failed lookup leaves the names empty: they are a label, and
// the list itself must not fail for them.
func (s *Service) withSLALabels(ctx context.Context, tenantID uuid.UUID, tickets []*Ticket) {
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, t := range tickets {
		if t != nil && t.SLAPolicyID != nil && !seen[*t.SLAPolicyID] {
			seen[*t.SLAPolicyID] = true
			ids = append(ids, *t.SLAPolicyID)
		}
	}
	if len(ids) == 0 {
		return
	}
	labels, err := s.repo.SLALabels(ctx, tenantID, ids)
	if err != nil {
		s.logger.Warn("sla labels lookup failed", zap.Error(err))
		return
	}
	for _, t := range tickets {
		if t == nil || t.SLAPolicyID == nil {
			continue
		}
		if l, ok := labels[*t.SLAPolicyID]; ok {
			t.SLAPolicyName, t.SLACalendarName = l.PolicyName, l.CalendarName
		}
	}
}

// withAuthorNames sets AuthorName on every comment, with one lookup for the whole page: "System"
// for a system comment, the agent's display name for staff, and for a customer the name on the
// channel message, else their ticketing user name, else the ticket's requester name (t may be
// nil when the ticket is not at hand). A failed lookup leaves the looked-up names empty.
func (s *Service) withAuthorNames(ctx context.Context, tenantID uuid.UUID, t *Ticket, comments []*TicketComment) {
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, c := range comments {
		if c == nil || c.AuthorType == AuthorSystem || c.AuthorID == uuid.Nil || seen[c.AuthorID] {
			continue
		}
		if c.AuthorType == AuthorCustomer && c.Sender != nil && c.Sender.Name != "" {
			continue
		}
		seen[c.AuthorID] = true
		ids = append(ids, c.AuthorID)
	}
	names := map[uuid.UUID]string{}
	if len(ids) > 0 {
		found, err := s.repo.AuthorNames(ctx, tenantID, ids)
		if err != nil {
			s.logger.Warn("comment author names lookup failed", zap.Error(err))
		} else {
			names = found
		}
	}
	requester := ""
	if t != nil {
		requester = metaText(t.Metadata, MetaRequesterName, metaLegacyCustomerName)
	}
	for _, c := range comments {
		if c == nil {
			continue
		}
		switch {
		case c.AuthorType == AuthorSystem:
			c.AuthorName = SystemAuthorName
		case c.AuthorType == AuthorCustomer && c.Sender != nil && c.Sender.Name != "":
			c.AuthorName = c.Sender.Name
		case names[c.AuthorID] != "":
			c.AuthorName = names[c.AuthorID]
		case c.AuthorType == AuthorCustomer:
			c.AuthorName = requester
		}
	}
}
