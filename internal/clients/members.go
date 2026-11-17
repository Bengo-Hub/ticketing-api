package clients

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// MembersClient adds people to a tenant through auth-api's S2S member endpoint,
// POST /api/v1/s2s/tenants/{tenant_id}/members (X-API-Key with INTERNAL_SERVICE_KEY). auth-api
// finds or creates the SSO account by email, upserts the membership and publishes
// auth.user.created. The call is an upsert, so the shared client's retries are safe.
type MembersClient struct {
	apiKey string
	client *serviceclient.Client
	logger *zap.Logger
}

// NewMembersClient creates the client. apiKey is the shared INTERNAL_SERVICE_KEY.
func NewMembersClient(authServiceURL, apiKey string, logger *zap.Logger) *MembersClient {
	log := logger.Named("members-client")
	cfg := serviceclient.DefaultConfig(strings.TrimSuffix(authServiceURL, "/"), "ticketing-service", log)
	cfg.Timeout = 10 * time.Second
	return &MembersClient{apiKey: apiKey, client: serviceclient.New(cfg), logger: log}
}

type memberResponse struct {
	UserID string `json:"user_id"`
}

// AddMember makes the person with this email a member of the tenant and returns their auth-api
// user id. Empty roles means auth's default (staff). auth-api replaces an existing membership's
// roles with the list sent.
func (c *MembersClient) AddMember(ctx context.Context, tenantID uuid.UUID, email string, roles []string) (uuid.UUID, error) {
	if c.apiKey == "" {
		return uuid.Nil, errors.New("members: INTERNAL_SERVICE_KEY is not configured")
	}
	if tenantID == uuid.Nil {
		return uuid.Nil, errors.New("members: tenant id is required")
	}
	if len(roles) == 0 {
		roles = []string{"staff"}
	}
	body := map[string]any{"email": email, "roles": roles, "service": "ticketing"}
	path := fmt.Sprintf("/api/v1/s2s/tenants/%s/members", tenantID)
	resp, err := c.client.Post(ctx, path, body, map[string]string{"X-API-Key": c.apiKey})
	if err != nil {
		return uuid.Nil, fmt.Errorf("members: add: %w", err)
	}
	if !resp.IsSuccess() {
		c.logger.Warn("auth-api refused the member add", zap.Int("status", resp.StatusCode))
		return uuid.Nil, fmt.Errorf("members: add: status %d", resp.StatusCode)
	}
	var member memberResponse
	if err := resp.DecodeJSON(&member); err != nil {
		return uuid.Nil, fmt.Errorf("members: decode: %w", err)
	}
	id, err := uuid.Parse(member.UserID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("members: invalid user_id in response: %w", err)
	}
	return id, nil
}
