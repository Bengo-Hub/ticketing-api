package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// OTPClient validates OTP tokens with auth-api.
type OTPClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewOTPClient creates a new OTP validator pointing at auth-api.
func NewOTPClient(authServiceURL string, logger *zap.Logger) *OTPClient {
	return &OTPClient{
		baseURL: authServiceURL,
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

// Validate calls auth-api to validate the OTP token.
// Returns the verified email address if valid, or an error.
func (c *OTPClient) Validate(ctx context.Context, token string) (email string, err error) {
	url := fmt.Sprintf("%s/api/v1/auth/otp/validate?token=%s", c.baseURL, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("otp validate: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("otp validate: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrOTPInvalid
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
