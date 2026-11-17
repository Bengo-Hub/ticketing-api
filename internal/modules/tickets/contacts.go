package tickets

import (
	"context"
	"errors"
	"strings"

	"github.com/Bengo-Hub/shared-service-client/crm"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ContactUpserter is marketflow's contact lookup and create (shared-service-client crm). Ticketing
// keeps no copy of the contact: a ticket holds only crm_contact_id and the requester fields.
type ContactUpserter interface {
	UpsertStrict(ctx context.Context, in crm.UpsertInput) (crm.Contact, error)
}

// SetContacts lets the service link tickets it opens from a channel message (Convert to ticket) to
// the requester's marketflow contact, as the channel ingest does for the tickets it opens.
func (s *Service) SetContacts(c ContactUpserter) { s.contacts = c }

// ResolveContact resolves a requester to their marketflow contact, creating it when marketflow has
// none. A failed lookup never holds a ticket back: it is logged with ids only and nil is returned,
// so the ticket opens without a contact id. Shared by the channel ingest and the internal create.
func ResolveContact(ctx context.Context, c ContactUpserter, log *zap.Logger, tenantID uuid.UUID, channel, name, email, phone string) *uuid.UUID {
	if c == nil || (email == "" && phone == "") {
		return nil
	}
	first, last := SplitName(name)
	contact, err := c.UpsertStrict(ctx, crm.UpsertInput{
		TenantID: tenantID, Email: email, Phone: phone, FirstName: first, LastName: last,
	})
	if err != nil {
		if !errors.Is(err, crm.ErrDisabled) && log != nil {
			log.Warn("requester not linked to a crm contact",
				zap.String("tenant_id", tenantID.String()), zap.String("channel", channel), zap.Error(err))
		}
		return nil
	}
	if contact.ID == uuid.Nil {
		return nil
	}
	return &contact.ID
}

// SplitName splits a display name into the first word and the rest, the shape marketflow keeps.
func SplitName(name string) (first, last string) {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "", ""
	}
	return parts[0], strings.Join(parts[1:], " ")
}
