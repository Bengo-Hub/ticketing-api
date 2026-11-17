package settings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

func aiSettingsRequest(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/acme/settings/ai", strings.NewReader(body))
	return r.WithContext(access.With(r.Context(), &access.Access{TenantID: tenantA, TenantSlug: "acme"}))
}

// ai.auto_classify is off until the tenant turns it on, and saving it leaves the channels alone.
func TestAIAutoClassifyOffByDefault(t *testing.T) {
	store := newMemStore()
	svc := NewService(store)
	h := NewHandler(svc, nil, zap.NewNop())

	w := httptest.NewRecorder()
	h.GetAI(w, aiSettingsRequest(http.MethodGet, ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"auto_classify":false`) {
		t.Fatalf("get = %d %s", w.Code, w.Body)
	}
	if on, err := svc.AutoClassify(context.Background(), tenantA); err != nil || on {
		t.Fatalf("default auto_classify = %v, %v", on, err)
	}

	if err := svc.SetWhatsApp(context.Background(), tenantA, WhatsAppChannel{AutoTicket: true}); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.PutAI(w, aiSettingsRequest(http.MethodPut, `{"auto_classify":true}`))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"auto_classify":true`) {
		t.Fatalf("put = %d %s", w.Code, w.Body)
	}
	if on, _ := svc.AutoClassify(context.Background(), tenantA); !on {
		t.Fatal("auto_classify not saved")
	}
	if c, _ := svc.Channels(context.Background(), tenantA); !c.WhatsApp.AutoTicket {
		t.Fatal("saving ai settings changed the channels section")
	}
}
