package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMembersClientAddMember(t *testing.T) {
	tenant, user := uuid.New(), uuid.New()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/s2s/tenants/"+tenant.String()+"/members" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "svc-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_id":"` + user.String() + `","tenant_id":"` + tenant.String() + `"}`))
	}))
	defer srv.Close()

	c := NewMembersClient(srv.URL+"/", "svc-key", zap.NewNop())
	id, err := c.AddMember(context.Background(), tenant, "new@acme.co.ke", nil)
	if err != nil || id != user {
		t.Fatalf("id %s, err %v", id, err)
	}
	if got["email"] != "new@acme.co.ke" || got["service"] != "ticketing" {
		t.Errorf("body %v", got)
	}
	if roles, _ := got["roles"].([]any); len(roles) != 1 || roles[0] != "staff" {
		t.Errorf("empty roles must default to staff: %v", got["roles"])
	}

	if _, err := NewMembersClient(srv.URL, "", zap.NewNop()).AddMember(context.Background(), tenant, "x@y.z", nil); err == nil {
		t.Error("a missing service key must fail before calling auth-api")
	}
	if _, err := NewMembersClient(srv.URL, "wrong", zap.NewNop()).AddMember(context.Background(), tenant, "x@y.z", nil); err == nil {
		t.Error("a refused call must fail")
	}
}
