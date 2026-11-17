package clients

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// aiCreditsService is the subscriptions-api metered service of the prepaid AI credit wallet.
const aiCreditsService = "ai_credits"

// CreditDecision is subscriptions-api's answer for an AI credit check or spend. Exempt is set for
// the tenants subscriptions-api never charges for AI (only the platform owner, codevertex); those
// are allowed and nothing is written. Reason explains a refusal (insufficient_credits,
// no_subscription, service_not_activated). Replayed means the spend key was already used, so
// nothing was charged again.
type CreditDecision struct {
	Allowed       bool   `json:"allowed"`
	Exempt        bool   `json:"exempt"`
	Reason        string `json:"reason"`
	Replayed      bool   `json:"replayed"`
	BundleBalance int64  `json:"bundle_balance"`
	QuotaBalance  int64  `json:"quota_balance"`
}

// CreditsClient reads and spends the tenant's prepaid AI credits in subscriptions-api through its
// S2S credit routes (X-API-Key), the same ledger marketflow-ai and notifications-api use:
//
//   - GET  /api/v1/s2s/credits/ai_credits/check?tenant_id=&units=  (nothing is spent)
//   - POST /api/v1/s2s/credits/ai_credits/consume {tenant_id, units, key, description}
//
// subscriptions-api decides the exemption (its credits Coverage), so no local exemption check
// exists here. A spend is idempotent on its key, which is also sent as Idempotency-Key so the
// shared client may retry it safely.
type CreditsClient struct {
	apiKey string
	client *serviceclient.Client
}

// NewCreditsClient creates the client. apiKey is the shared INTERNAL_SERVICE_KEY.
func NewCreditsClient(baseURL, apiKey string, logger *zap.Logger) *CreditsClient {
	cfg := serviceclient.DefaultConfig(strings.TrimSuffix(baseURL, "/"), "subscriptions-api", logger.Named("credits-client"))
	cfg.Timeout = 5 * time.Second
	cfg.MaxElapsedTime = 10 * time.Second
	return &CreditsClient{apiKey: apiKey, client: serviceclient.New(cfg)}
}

func (c *CreditsClient) ready() error {
	if c.apiKey == "" {
		return errors.New("credits: INTERNAL_SERVICE_KEY is not configured")
	}
	return nil
}

// Check reports whether units would fit the tenant's AI credits now, spending nothing.
func (c *CreditsClient) Check(ctx context.Context, tenantID uuid.UUID, units int64) (CreditDecision, error) {
	if err := c.ready(); err != nil {
		return CreditDecision{}, err
	}
	q := url.Values{"tenant_id": {tenantID.String()}, "units": {fmt.Sprint(units)}}
	resp, err := c.client.Get(ctx, "/api/v1/s2s/credits/"+aiCreditsService+"/check?"+q.Encode(),
		map[string]string{"X-API-Key": c.apiKey})
	if err != nil {
		return CreditDecision{}, fmt.Errorf("credits: check: %w", err)
	}
	if !resp.IsSuccess() {
		return CreditDecision{}, fmt.Errorf("credits: check: status %d", resp.StatusCode)
	}
	var d CreditDecision
	if err := resp.DecodeJSON(&d); err != nil {
		return CreditDecision{}, fmt.Errorf("credits: check decode: %w", err)
	}
	return d, nil
}

// Consume spends units under key. A repeat of the same key charges nothing and answers Replayed.
func (c *CreditsClient) Consume(ctx context.Context, tenantID uuid.UUID, units int64, key, description string) (CreditDecision, error) {
	if err := c.ready(); err != nil {
		return CreditDecision{}, err
	}
	if key == "" {
		return CreditDecision{}, errors.New("credits: a spend key is required")
	}
	body := map[string]any{"tenant_id": tenantID.String(), "units": units, "key": key, "description": description}
	resp, err := c.client.Post(ctx, "/api/v1/s2s/credits/"+aiCreditsService+"/consume", body,
		map[string]string{"X-API-Key": c.apiKey, "Idempotency-Key": key})
	if err != nil {
		return CreditDecision{}, fmt.Errorf("credits: consume: %w", err)
	}
	if !resp.IsSuccess() {
		return CreditDecision{}, fmt.Errorf("credits: consume: status %d", resp.StatusCode)
	}
	var d CreditDecision
	if err := resp.DecodeJSON(&d); err != nil {
		return CreditDecision{}, fmt.Errorf("credits: consume decode: %w", err)
	}
	return d, nil
}
