package settings

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SectionChannels is the settings section of the inbound channels.
const SectionChannels = "channels"

// Keys inside the channels section.
const (
	keyWhatsApp       = "whatsapp"
	keyEmail          = "email"
	keyLastReceivedAt = "last_received_at"
)

// EmailReceiptEvery is how often a tenant's channels.email.last_received_at is written at most, so
// a busy mailbox never rewrites the settings row on every message.
const EmailReceiptEvery = time.Minute

// Channels are a tenant's inbound channel settings.
type Channels struct {
	WhatsApp WhatsAppChannel `json:"whatsapp"`
	Email    EmailChannel    `json:"email"`
}

// WhatsAppChannel holds the WhatsApp intake settings.
type WhatsAppChannel struct {
	// AutoTicket opens a ticket for a WhatsApp message that does not continue an open one. Off by
	// default, so a tenant that answers WhatsApp from the notifications inbox is not flooded.
	AutoTicket bool `json:"auto_ticket"`
}

// EmailChannel is the email intake state. Only the ingest writes it; no client can set it.
type EmailChannel struct {
	// LastReceivedAt is when the last forwarded email for the tenant arrived (to the minute), the
	// proof that the tenant's forwarding works. Nil until the first one arrives.
	LastReceivedAt *time.Time `json:"last_received_at"`
}

// sectionStore is the part of Store the service uses.
type sectionStore interface {
	Section(ctx context.Context, tenantID uuid.UUID, name string, into any) (bool, error)
	PutSectionKey(ctx context.Context, tenantID uuid.UUID, section, key string, v any) error
	TouchSectionTime(ctx context.Context, tenantID uuid.UUID, section, object, key string, at time.Time, every time.Duration) error
}

// Service reads and writes typed settings sections.
type Service struct {
	store sectionStore
	now   func() time.Time

	mu sync.Mutex
	// emailSeen is when this replica last wrote each tenant's last_received_at, so most messages
	// skip the database entirely; the store's own comparison covers the other replicas.
	emailSeen map[uuid.UUID]time.Time
}

// NewService creates the settings service.
func NewService(store sectionStore) *Service {
	return &Service{store: store, now: time.Now, emailSeen: map[uuid.UUID]time.Time{}}
}

// Channels returns the tenant's channel settings, the defaults when it never saved any.
func (s *Service) Channels(ctx context.Context, tenantID uuid.UUID) (Channels, error) {
	var c Channels
	if _, err := s.store.Section(ctx, tenantID, SectionChannels, &c); err != nil {
		return Channels{}, err
	}
	return c, nil
}

// SetWhatsApp saves the tenant's WhatsApp intake settings and leaves the rest of the section, the
// email receipt included, as it is.
func (s *Service) SetWhatsApp(ctx context.Context, tenantID uuid.UUID, w WhatsAppChannel) error {
	return s.store.PutSectionKey(ctx, tenantID, SectionChannels, keyWhatsApp, w)
}

// EmailReceived records that a forwarded email for the tenant arrived. It writes at most once a
// minute per tenant: this replica skips a tenant it wrote less than a minute ago, and the store
// writes only when the stored time is older than that.
func (s *Service) EmailReceived(ctx context.Context, tenantID uuid.UUID) error {
	now := s.now().UTC().Truncate(time.Second)
	s.mu.Lock()
	if last, ok := s.emailSeen[tenantID]; ok && now.Sub(last) < EmailReceiptEvery {
		s.mu.Unlock()
		return nil
	}
	s.emailSeen[tenantID] = now
	s.mu.Unlock()

	err := s.store.TouchSectionTime(ctx, tenantID, SectionChannels, keyEmail, keyLastReceivedAt, now, EmailReceiptEvery)
	if err != nil {
		// Forget the attempt so the next message tries again.
		s.mu.Lock()
		if s.emailSeen[tenantID].Equal(now) {
			delete(s.emailSeen, tenantID)
		}
		s.mu.Unlock()
	}
	return err
}
