package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

// OTPClient validates OTP tokens with auth-api.
type OTPClient struct {
	baseURL    string
	serviceKey string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewOTPClient creates a new OTP validator pointing at auth-api. The token trade is an S2S
// route, so it carries the internal service key.
func NewOTPClient(authServiceURL, serviceKey string, logger *zap.Logger) *OTPClient {
	return &OTPClient{
		baseURL:    strings.TrimSuffix(authServiceURL, "/"),
		serviceKey: serviceKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger: logger.Named("otp-client"),
	}
}

type otpValidateResponse struct {
	Valid bool   `json:"valid"`
	Email string `json:"email"`
}

// Validate trades the widget's otp_token for the verified email address through auth-api's
// GET /api/v1/s2s/otp/validate. A token works once. Returns ErrOTPInvalid for an unknown,
// expired or used token.
func (c *OTPClient) Validate(ctx context.Context, token string) (email string, err error) {
	u := fmt.Sprintf("%s/api/v1/s2s/otp/validate?token=%s", c.baseURL, url.QueryEscape(token))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("otp validate: build request: %w", err)
	}
	if c.serviceKey != "" {
		req.Header.Set("X-API-Key", c.serviceKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("otp validate: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		c.logger.Error("otp validate refused by auth-api; check INTERNAL_SERVICE_KEY", zap.Int("status", resp.StatusCode))
		return "", fmt.Errorf("otp validate: auth-api refused the service key (%d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("otp validate: unexpected status %d", resp.StatusCode)
	}

	var body otpValidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("otp validate: decode: %w", err)
	}
	if !body.Valid {
		return "", ErrOTPInvalid
	}
	return body.Email, nil
}
