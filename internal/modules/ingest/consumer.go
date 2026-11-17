package ingest

import (
	"context"
	"time"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/settings"
)

// Consumer names. Each is both the durable and the deliver group, so every API replica shares
// the consumer and each message is handled once across the fleet.
const (
	EmailDurable    = "ticketing-email-ingest"
	WhatsAppDurable = "ticketing-whatsapp-ingest"
)

// maxDeliver is how often a message is tried before JetStream gives up on it.
const maxDeliver = 5

// ConsumerConfig names the streams and subjects the consumers read.
type ConsumerConfig struct {
	EmailStream     string
	EmailSubject    string
	WhatsAppStream  string
	WhatsAppSubject string
}

// ChannelSettings reads a tenant's channel settings (settings.Service).
type ChannelSettings interface {
	Channels(ctx context.Context, tenantID uuid.UUID) (settings.Channels, error)
}

// Consumers feeds the email and WhatsApp events into the ingest service.
type Consumers struct {
	svc      *Service
	tenants  *TenantResolver
	settings ChannelSettings
	addrs    Addresses
	cfg      ConsumerConfig
	logger   *zap.Logger
}

// NewConsumers wires the consumers.
func NewConsumers(svc *Service, tenants *TenantResolver, cs ChannelSettings, addrs Addresses, cfg ConsumerConfig, logger *zap.Logger) *Consumers {
	return &Consumers{svc: svc, tenants: tenants, settings: cs, addrs: addrs, cfg: cfg, logger: logger.Named("ingest.consumer")}
}

// Start subscribes both durable consumers in the background (SubscribeQueueWithRebind waits for
// a stream that does not exist yet).
func (c *Consumers) Start(js nats.JetStreamContext) {
	if c.cfg.EmailStream != "" && c.cfg.EmailSubject != "" {
		c.subscribe(js, c.cfg.EmailStream, c.cfg.EmailSubject, EmailDurable, c.HandleEmail)
	}
	if c.cfg.WhatsAppStream != "" && c.cfg.WhatsAppSubject != "" {
		c.subscribe(js, c.cfg.WhatsAppStream, c.cfg.WhatsAppSubject, WhatsAppDurable, c.HandleWhatsApp)
	}
}

func (c *Consumers) subscribe(js nats.JetStreamContext, stream, subject, durable string, handle func(context.Context, []byte) error) {
	eventslib.SubscribeQueueWithRebind(c.logger, js, stream, subject, durable, func(m *nats.Msg) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		c.settle(m, subject, handle(ctx, m.Data))
	},
		nats.BindStream(stream),
		nats.Durable(durable),
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.AckWait(60*time.Second),
		nats.MaxDeliver(maxDeliver),
	)
}

// settle acknowledges a handled or permanently failed message and asks for a later retry of a
// transient failure. A permanent failure is logged and never retried.
func (c *Consumers) settle(m *nats.Msg, subject string, err error) {
	if err == nil {
		_ = m.Ack()
		return
	}
	if IsPermanent(err) {
		c.logger.Warn("inbound message dropped", zap.String("subject", subject), zap.Error(err))
		_ = m.Ack()
		return
	}
	attempt := uint64(1)
	if md, mdErr := m.Metadata(); mdErr == nil {
		attempt = md.NumDelivered
	}
	if attempt >= maxDeliver {
		c.logger.Error("inbound message failed on its last delivery", zap.String("subject", subject), zap.Error(err))
	} else {
		c.logger.Warn("inbound message failed, retrying", zap.String("subject", subject), zap.Uint64("attempt", attempt), zap.Error(err))
	}
	_ = m.NakWithDelay(time.Duration(attempt) * 30 * time.Second)
}

// HandleEmail ingests one email.inbound.support event.
func (c *Consumers) HandleEmail(ctx context.Context, data []byte) error {
	var e EmailInbound
	if _, err := decodePayload(data, &e); err != nil {
		return Permanent(err)
	}
	candidates := append([]string{e.Recipient}, append(e.To, e.Cc...)...)
	tenantID, slug, err := c.tenants.Resolve(ctx, candidates...)
	if err != nil {
		return err
	}
	m, ok := EmailMessage(e, tenantID, slug, c.addrs)
	if !ok {
		return Permanent(ErrInvalidMessage)
	}
	return c.ingest(ctx, m)
}

// HandleWhatsApp ingests one notifications.whatsapp.tenant_inbound event.
func (c *Consumers) HandleWhatsApp(ctx context.Context, data []byte) error {
	var e WhatsAppInbound
	envTenant, err := decodePayload(data, &e)
	if err != nil {
		return Permanent(err)
	}
	if e.TenantID == "" {
		e.TenantID = envTenant
	}
	tenantID, err := uuid.Parse(e.TenantID)
	if err != nil {
		return Permanent(ErrInvalidMessage)
	}
	cs, err := c.settings.Channels(ctx, tenantID)
	if err != nil {
		return err
	}
	m, ok := WhatsAppMessage(e, cs.WhatsApp.AutoTicket)
	if !ok {
		return Permanent(ErrInvalidMessage)
	}
	return c.ingest(ctx, m)
}

func (c *Consumers) ingest(ctx context.Context, m Message) error {
	res, err := c.svc.Ingest(ctx, m)
	if err != nil {
		return err
	}
	c.logger.Info("inbound message ingested",
		zap.String("channel", string(m.Channel)), zap.String("tenant_id", m.TenantID.String()),
		zap.String("outcome", string(res.Outcome)), zap.String("ticket_number", res.TicketNumber),
		zap.String("reason", res.Reason))
	return nil
}
