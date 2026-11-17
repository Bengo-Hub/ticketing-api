package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestCreditsCheck(t *testing.T) {
	tenant := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/s2s/credits/ai_credits/check" ||
			r.URL.Query().Get("tenant_id") != tenant.String() || r.URL.Query().Get("units") != "1" || r.Header.Get("X-API-Key") != "k" {
			t.Errorf("%s %s %v", r.Method, r.URL, r.Header)
		}
		_, _ = w.Write([]byte(`{"allowed":false,"reason":"insufficient_credits","bundle_balance":0,"quota_balance":0}`))
	}))
	defer srv.Close()
	d, err := NewCreditsClient(srv.URL, "k", zap.NewNop()).Check(context.Background(), tenant, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Reason != "insufficient_credits" {
		t.Errorf("decision %+v", d)
	}
}

// A spend carries its key in the body and as Idempotency-Key, so the shared client may retry it.
func TestCreditsConsumeRetriesUnderItsKey(t *testing.T) {
	tenant := uuid.New()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/api/v1/s2s/credits/ai_credits/consume" || r.Header.Get("Idempotency-Key") != "key-1" ||
			body["key"] != "key-1" || body["units"] != float64(1) || body["tenant_id"] != tenant.String() || body["description"] != "d" {
			t.Errorf("%s %v %v", r.URL.Path, r.Header, body)
		}
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"allowed":true,"replayed":true}`))
	}))
	defer srv.Close()
	d, err := NewCreditsClient(srv.URL, "k", zap.NewNop()).Consume(context.Background(), tenant, 1, "key-1", "d")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed || !d.Replayed || atomic.LoadInt32(&hits) != 2 {
		t.Errorf("decision %+v after %d calls", d, hits)
	}
	if _, err := NewCreditsClient(srv.URL, "k", zap.NewNop()).Consume(context.Background(), tenant, 1, "", "d"); err == nil {
		t.Error("a spend without a key must fail")
	}
	if _, err := NewCreditsClient(srv.URL, "", zap.NewNop()).Check(context.Background(), tenant, 1); err == nil {
		t.Error("a missing service key must fail")
	}
}
