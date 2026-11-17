package settings

import (
	"context"

	"github.com/google/uuid"
)

// SectionChannels is the settings section of the inbound channels.
const SectionChannels = "channels"

// Channels are a tenant's inbound channel settings.
type Channels struct {
	WhatsApp WhatsAppChannel `json:"whatsapp"`
}

// WhatsAppChannel holds the WhatsApp intake settings.
type WhatsAppChannel struct {
	// AutoTicket opens a ticket for a WhatsApp message that does not continue an open one. Off by
	// default, so a tenant that answers WhatsApp from the notifications inbox is not flooded.
	AutoTicket bool `json:"auto_ticket"`
}

// sectionStore is the part of Store the service uses.
type sectionStore interface {
	Section(ctx context.Context, tenantID uuid.UUID, name string, into any) (bool, error)
	PutSection(ctx context.Context, tenantID uuid.UUID, name string, v any) error
}

// Service reads and writes typed settings sections.
type Service struct {
	store sectionStore
}

// NewService creates the settings service.
func NewService(store sectionStore) *Service { return &Service{store: store} }

// Channels returns the tenant's channel settings, the defaults when it never saved any.
func (s *Service) Channels(ctx context.Context, tenantID uuid.UUID) (Channels, error) {
	var c Channels
	if _, err := s.store.Section(ctx, tenantID, SectionChannels, &c); err != nil {
		return Channels{}, err
	}
	return c, nil
}

// SetChannels saves the tenant's channel settings.
func (s *Service) SetChannels(ctx context.Context, tenantID uuid.UUID, c Channels) error {
	return s.store.PutSection(ctx, tenantID, SectionChannels, c)
}
