package kb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"github.com/bengobox/ticketing-service/internal/http/visitorip"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func post(h http.Handler, path string) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	return rec.Code
}

// The cached server read does not count; the browser's view call counts once; a plain read still
// counts, as before.
func TestPublicArticleViewCounting(t *testing.T) {
	f, repo := newFixture(1)
	h := publicRouter(f, repo)
	a := f.visible[0]

	if code := getJSON(t, h, "/acme/kb/articles/"+a.Slug+"?count_view=false", nil); code != http.StatusOK {
		t.Fatalf("uncounted read: %d", code)
	}
	if code := post(h, "/acme/kb/articles/"+a.ID.String()+"/view"); code != http.StatusNoContent {
		t.Fatalf("view: %d, want 204", code)
	}
	if n := repo.views.Load(); n != 1 {
		t.Fatalf("views = %d after one uncounted read and one view call, want 1", n)
	}

	// A plain read counts on its own goroutine.
	if code := getJSON(t, h, "/acme/kb/articles/"+a.Slug, nil); code != http.StatusOK {
		t.Fatalf("counted read: %d", code)
	}
	deadline := time.Now().Add(time.Second)
	for repo.views.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := repo.views.Load(); n != 2 {
		t.Errorf("views = %d after a plain read, want 2", n)
	}
}

// A view of anything the public routes would not show is not found and not counted.
func TestPublicArticleViewRefusesHiddenArticles(t *testing.T) {
	f, repo := newFixture(1)
	h := publicRouter(f, repo)
	for _, hidden := range f.hidden {
		if code := post(h, "/acme/kb/articles/"+hidden.ID.String()+"/view"); code != http.StatusNotFound {
			t.Errorf("view of %s: %d, want 404", hidden.Slug, code)
		}
	}
	if code := post(h, "/acme/kb/articles/not-a-uuid/view"); code != http.StatusBadRequest {
		t.Errorf("bad id: %d, want 400", code)
	}
	if code := post(h, "/nobody/kb/articles/"+f.visible[0].ID.String()+"/view"); code != http.StatusNotFound {
		t.Errorf("unknown tenant: %d, want 404", code)
	}
	if n := repo.views.Load(); n != 0 {
		t.Errorf("views = %d, want 0", n)
	}
}

type keyLimiter struct{ keys []string }

func (k *keyLimiter) Allow(_ context.Context, key string, _ ratelimit.Options, _ int) (bool, time.Duration) {
	k.keys = append(k.keys, key)
	return true, 0
}

// Reads the UI server makes for a visitor are limited per visitor only when they carry the
// forwarding key; any other X-Visitor-IP is ignored and the ingress address is the key.
func TestPublicReadLimitKeyFollowsTrustedVisitorIP(t *testing.T) {
	forwardKey := strings.Repeat("example-only-", 4)
	f, repo := newFixture(1)
	h := NewHandler(NewService(repo, zap.NewNop()), zap.NewNop())
	h.SetTenantLookup(fakeTenants{"acme": f.tenant.String()})
	lim := &keyLimiter{}
	h.SetLimiter(lim)
	r := chi.NewRouter()
	r.Use(ratelimit.TrustedRealIP)
	r.Use(visitorip.Middleware(forwardKey))
	r.Route("/{tenant}", h.RegisterPublicRoutes)

	read := func(key string) {
		req := httptest.NewRequest(http.MethodGet, "/acme/kb/articles/visible-a?count_view=false", nil)
		req.Header.Set("X-Real-IP", "10.0.0.7") // the UI server, as the ingress saw it
		req.Header.Set(visitorip.IPHeader, "203.0.113.9")
		if key != "" {
			req.Header.Set(visitorip.KeyHeader, key)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("read: %d", rec.Code)
		}
	}
	read(forwardKey)
	read("")
	read(strings.Repeat("wrong-", 8))

	want := []string{"acme:203.0.113.9", "acme:10.0.0.7", "acme:10.0.0.7"}
	if strings.Join(lim.keys, ",") != strings.Join(want, ",") {
		t.Errorf("limiter keys = %v, want %v", lim.keys, want)
	}
}
