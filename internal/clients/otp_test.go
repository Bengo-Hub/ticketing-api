package clients

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestOTPClientValidate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/s2s/otp/validate" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "svc-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("token") == "good token" {
			_, _ = w.Write([]byte(`{"valid":true,"email":"visitor@example.com"}`))
			return
		}
		_, _ = w.Write([]byte(`{"valid":false}`))
	}))
	defer srv.Close()

	c := NewOTPClient(srv.URL+"/", "svc-key", zap.NewNop())
	email, err := c.Validate(context.Background(), "good token")
	if err != nil || email != "visitor@example.com" {
		t.Fatalf("good token: email %q, err %v", email, err)
	}
	if _, err := c.Validate(context.Background(), "used"); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("used token: want ErrOTPInvalid, got %v", err)
	}

	noKey := NewOTPClient(srv.URL, "", zap.NewNop())
	if _, err := noKey.Validate(context.Background(), "good token"); err == nil || errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("missing key must be a config error, not an invalid token: %v", err)
	}
}
