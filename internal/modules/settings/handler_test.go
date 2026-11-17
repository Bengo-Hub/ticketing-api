package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

func channelsRequest(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/acme/settings/channels", strings.NewReader(body))
	return r.WithContext(access.With(r.Context(), &access.Access{TenantID: tenantA, TenantSlug: "acme"}))
}

// GET returns email.last_received_at as null until the first forwarded email; PUT cannot set it.
func TestChannelsEmailReceiptReadOnly(t *testing.T) {
	store := newMemStore()
	h := NewHandler(NewService(store), func(slug string) string { return slug + "@support.example.com" }, zap.NewNop())

	w := httptest.NewRecorder()
	h.GetChannels(w, channelsRequest(http.MethodGet, ""))
	if !strings.Contains(w.Body.String(), `"last_received_at":null`) || !strings.Contains(w.Body.String(), `"forwarding_address":"acme@support.example.com"`) {
		t.Fatalf("get = %s", w.Body)
	}

	w = httptest.NewRecorder()
	h.PutChannels(w, channelsRequest(http.MethodPut,
		`{"whatsapp":{"auto_ticket":true},"email":{"last_received_at":"2026-01-01T00:00:00Z"}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("put status %d: %s", w.Code, w.Body)
	}
	var got channelsView
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.WhatsApp.AutoTicket || got.Email.LastReceivedAt != nil {
		t.Fatalf("put = %+v", got)
	}

	if err := h.service.EmailReceived(context.Background(), tenantA); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.GetChannels(w, channelsRequest(http.MethodGet, ""))
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Email.LastReceivedAt == nil || !got.WhatsApp.AutoTicket {
		t.Fatalf("after a receipt: %+v", got)
	}
}
