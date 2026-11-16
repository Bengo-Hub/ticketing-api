package tickets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/rbac"
	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// memPresence is an in-memory PresenceStore.
type memPresence struct{ entries map[uuid.UUID]PresenceEntry }

func (m *memPresence) Touch(_ context.Context, _, _ uuid.UUID, e PresenceEntry, _ time.Duration) (bool, error) {
	prev, ok := m.entries[e.UserID]
	m.entries[e.UserID] = e
	return !ok || prev.State != e.State, nil
}

func (m *memPresence) Remove(_ context.Context, _, _, user uuid.UUID) (bool, error) {
	_, ok := m.entries[user]
	delete(m.entries, user)
	return ok, nil
}

func (m *memPresence) List(context.Context, uuid.UUID, uuid.UUID) ([]PresenceEntry, error) {
	out := []PresenceEntry{}
	for _, e := range m.entries {
		out = append(out, e)
	}
	return out, nil
}

// testTenant is the caller's tenant; the access middleware refuses uuid.Nil.
var testTenant = uuid.MustParse("6f1c2b8e-0d3a-4c55-9e21-7a4b9c0d1e2f")

type recordedSSE struct{ types []string }

func (r *recordedSSE) Publish(_ uuid.UUID, eventType string, _ any) {
	r.types = append(r.types, eventType)
}

// routerAs mounts the ticket routes behind a fixed caller with the given ticketing role.
func routerAs(h *Handler, tenant, user uuid.UUID, role string) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			a := &access.Access{TenantID: tenant, AuthUserID: user, Name: "Jane", Roles: []string{role},
				Perms: rbac.PermissionsForRoles([]string{role})}
			next.ServeHTTP(w, req.WithContext(access.With(req.Context(), a)))
		})
	})
	h.RegisterRoutes(r)
	return r
}

func TestPresenceIsStaffOnly(t *testing.T) {
	repo := newWorkRepo()
	customer := uuid.New()
	ticket := repo.addTicket("TKT-000001", StatusOpen)
	ticket.CustomerID = customer
	store := &memPresence{entries: map[uuid.UUID]PresenceEntry{}}
	sse := &recordedSSE{}
	h := NewHandler(newWorkService(repo), zap.NewNop(), WithPresence(NewPresence(store, sse)))
	path := "/tickets/" + ticket.ID.String() + "/presence"

	// The customer owns the ticket and still may neither announce nor read presence.
	cust := routerAs(h, testTenant, customer, rbac.RoleCustomer)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := httptest.NewRecorder()
		cust.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(`{"state":"viewing"}`)))
		if rec.Code != http.StatusForbidden {
			t.Errorf("customer %s presence: %d", method, rec.Code)
		}
	}
	// Nor read views or macros.
	for _, p := range []string{"/views", "/macros", "/tickets/tags", "/tickets/" + ticket.ID.String() + "/links"} {
		rec := httptest.NewRecorder()
		cust.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("customer GET %s: %d", p, rec.Code)
		}
	}

	agent := routerAs(h, testTenant, uuid.New(), rbac.RoleAgent)
	rec := httptest.NewRecorder()
	agent.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"state":"typing"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"typing"`) {
		t.Fatalf("agent presence: %d %s", rec.Code, rec.Body)
	}
	if len(sse.types) != 1 || sse.types[0] != "presence.changed" {
		t.Fatalf("broadcast: %v", sse.types)
	}
	// A refresh in the same state is not broadcast again.
	agent.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"state":"typing"}`)))
	if len(sse.types) != 1 {
		t.Fatalf("refresh broadcast: %v", sse.types)
	}
	rec = httptest.NewRecorder()
	agent.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"state":"dancing"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown state: %d", rec.Code)
	}
}

func TestPresenceWithoutRedisIsUnavailable(t *testing.T) {
	repo := newWorkRepo()
	ticket := repo.addTicket("TKT-000001", StatusOpen)
	h := NewHandler(newWorkService(repo), zap.NewNop(), WithPresence(NewPresence(nil, nil)))
	rec := httptest.NewRecorder()
	routerAs(h, testTenant, uuid.New(), rbac.RoleAgent).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/tickets/"+ticket.ID.String()+"/presence", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no redis: %d", rec.Code)
	}
}
