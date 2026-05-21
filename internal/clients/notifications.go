package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/config"
)

// NotificationsClient is an S2S client to the notifications-api.
type NotificationsClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewNotificationsClient creates a new notifications service client.
func NewNotificationsClient(cfg config.NotificationsConfig, logger *zap.Logger) *NotificationsClient {
	return &NotificationsClient{
		baseURL: strings.TrimRight(cfg.ServiceURL, "/"),
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
		logger: logger.Named("clients.notifications"),
	}
}

// SendMessageRequest is the notifications-api send payload.
type SendMessageRequest struct {
	Channel  string                 `json:"channel"`
	Tenant   string                 `json:"tenant"`
	Template string                 `json:"template"`
	To       []string               `json:"to"`
	Data     map[string]interface{} `json:"data"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SendMessage sends a notification via the notifications-api.
func (c *NotificationsClient) SendMessage(ctx context.Context, tenantSlug string, req SendMessageRequest) error {
	if c.baseURL == "" {
		return nil
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/%s/notifications/messages", c.baseURL, tenantSlug)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("notifications-api returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// SendTicketCreated sends a ticket created notification to the customer.
func (c *NotificationsClient) SendTicketCreated(ctx context.Context, tenantSlug string, customerEmail string, data map[string]interface{}) {
	if customerEmail == "" {
		return
	}
	if err := c.SendMessage(ctx, tenantSlug, SendMessageRequest{
		Channel:  "email",
		Tenant:   tenantSlug,
		Template: "ticket_created",
		To:       []string{customerEmail},
		Data:     data,
	}); err != nil {
		c.logger.Warn("failed to send ticket_created notification", zap.Error(err))
	}
}

// SendTicketAssigned sends an assignment notification to the assigned agent.
func (c *NotificationsClient) SendTicketAssigned(ctx context.Context, tenantSlug string, agentEmail string, data map[string]interface{}) {
	if agentEmail == "" {
		return
	}
	if err := c.SendMessage(ctx, tenantSlug, SendMessageRequest{
		Channel:  "email",
		Tenant:   tenantSlug,
		Template: "ticket_assigned",
		To:       []string{agentEmail},
		Data:     data,
	}); err != nil {
		c.logger.Warn("failed to send ticket_assigned notification", zap.Error(err))
	}
}

// SendTicketResolved sends a resolution notification to the customer.
func (c *NotificationsClient) SendTicketResolved(ctx context.Context, tenantSlug string, customerEmail string, data map[string]interface{}) {
	if customerEmail == "" {
		return
	}
	if err := c.SendMessage(ctx, tenantSlug, SendMessageRequest{
		Channel:  "email",
		Tenant:   tenantSlug,
		Template: "ticket_resolved",
		To:       []string{customerEmail},
		Data:     data,
	}); err != nil {
		c.logger.Warn("failed to send ticket_resolved notification", zap.Error(err))
	}
}

// SendAgentComment sends a new comment notification to the customer.
func (c *NotificationsClient) SendAgentComment(ctx context.Context, tenantSlug string, customerEmail string, data map[string]interface{}) {
	if customerEmail == "" {
		return
	}
	if err := c.SendMessage(ctx, tenantSlug, SendMessageRequest{
		Channel:  "email",
		Tenant:   tenantSlug,
		Template: "ticket_comment_added",
		To:       []string{customerEmail},
		Data:     data,
	}); err != nil {
		c.logger.Warn("failed to send ticket_comment_added notification", zap.Error(err))
	}
}

// ensure NotificationsClient satisfies an interface (compile-time check).
var _ interface {
	SendMessage(context.Context, string, SendMessageRequest) error
} = (*NotificationsClient)(nil)

