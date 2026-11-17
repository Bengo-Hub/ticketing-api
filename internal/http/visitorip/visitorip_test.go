package visitorip

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
)

// A placeholder, never a real key: long enough to pass MinKeyLength.
var testKey = strings.Repeat("example-only-", 4)

// seen runs one request through TrustedRealIP and Middleware(key) and reports the client IP the
// limiters would key on, plus the headers that reached the handler.
func seen(t *testing.T, key string, set func(r *http.Request)) (string, http.Header) {
	t.Helper()
	var ip string
	var hdr http.Header
	h := ratelimit.TrustedRealIP(Middleware(key)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ip = ratelimit.ClientIP(r)
		hdr = r.Header.Clone()
	})))
	r := httptest.NewRequest(http.MethodGet, "/api/v1/acme/kb/articles/reset-password", nil)
	// The ingress in front of ticketing-api sets X-Real-IP to the caller: here, the UI server.
	r.Header.Set("X-Real-IP", "10.0.0.7")
	set(r)
	h.ServeHTTP(httptest.NewRecorder(), r)
	return ip, hdr
}

func TestVisitorIPHonouredWithTheKey(t *testing.T) {
	ip, hdr := seen(t, testKey, func(r *http.Request) {
		r.Header.Set(IPHeader, "203.0.113.9")
		r.Header.Set(KeyHeader, testKey)
	})
	if ip != "203.0.113.9" {
		t.Errorf("client IP = %q, want the visitor 203.0.113.9", ip)
	}
	if hdr.Get(IPHeader) != "" || hdr.Get(KeyHeader) != "" {
		t.Error("the forwarding headers must not reach the handler")
	}
}

func TestVisitorIPv6Honoured(t *testing.T) {
	ip, _ := seen(t, testKey, func(r *http.Request) {
		r.Header.Set(IPHeader, " 2001:db8::1 ")
		r.Header.Set(KeyHeader, testKey)
	})
	if ip != "2001:db8::1" {
		t.Errorf("client IP = %q, want 2001:db8::1", ip)
	}
}

func TestVisitorIPRefused(t *testing.T) {
	cases := []struct {
		name string
		key  string
		set  func(r *http.Request)
	}{
		{"no key header (a browser on the open internet)", testKey, func(r *http.Request) {
			r.Header.Set(IPHeader, "203.0.113.9")
		}},
		{"wrong key", testKey, func(r *http.Request) {
			r.Header.Set(IPHeader, "203.0.113.9")
			r.Header.Set(KeyHeader, strings.Repeat("wrong-", 8))
		}},
		{"key with a prefix of the right one", testKey, func(r *http.Request) {
			r.Header.Set(IPHeader, "203.0.113.9")
			r.Header.Set(KeyHeader, testKey[:MinKeyLength])
		}},
		{"forwarding off (no key configured)", "", func(r *http.Request) {
			r.Header.Set(IPHeader, "203.0.113.9")
			r.Header.Set(KeyHeader, "")
		}},
		{"configured key too short", "short", func(r *http.Request) {
			r.Header.Set(IPHeader, "203.0.113.9")
			r.Header.Set(KeyHeader, "short")
		}},
		{"right key, not an IP", testKey, func(r *http.Request) {
			r.Header.Set(IPHeader, "203.0.113.9, 198.51.100.1")
			r.Header.Set(KeyHeader, testKey)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ip, hdr := seen(t, c.key, c.set)
			if ip != "10.0.0.7" {
				t.Errorf("client IP = %q, want the ingress address 10.0.0.7", ip)
			}
			if hdr.Get(IPHeader) != "" || hdr.Get(KeyHeader) != "" {
				t.Error("the forwarding headers must be stripped even when refused")
			}
		})
	}
}

// A refused header must not leak into the per-IP limiter key either.
func TestRefusedHeaderDoesNotChangeLimiterKey(t *testing.T) {
	var key string
	h := ratelimit.TrustedRealIP(Middleware(testKey)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		key = ratelimit.IPKey(r)
	})))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "198.51.100.20:5555"
	r.Header.Set(IPHeader, "203.0.113.9")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if key != "ip:198.51.100.20" || strings.Contains(key, "203.0.113.9") {
		t.Errorf("limiter key = %q, want the peer address", key)
	}
}
