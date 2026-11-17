// Package visitorip lets ticketing-ui's server name the visitor it is reading for.
//
// The public help center pages read articles on the UI server, so without this every visitor's
// read reaches ticketing-api from the UI server's address and shares one rate limit bucket (the
// global 300 a minute and the 120 a minute public KB read limit). A busy article or one crawler
// could then answer 429 for everybody.
//
// The UI server sends the visitor's address in IPHeader together with the shared key in
// KeyHeader. Only when the key matches is the address used: it then replaces X-Real-IP and the
// peer address that ratelimit.TrustedRealIP set, so ratelimit.ClientIP, every IP keyed limit and
// the request log see the visitor. A request without the key, or with a wrong one, keeps the
// address the ingress established; a visitor's browser can send IPHeader but never knows the key.
// Both headers are removed before the request goes further either way.
package visitorip

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

const (
	// IPHeader carries the visitor's address as the UI server's own ingress saw it.
	IPHeader = "X-Visitor-IP"
	// KeyHeader carries the shared key (VISITOR_IP_FORWARD_KEY) that makes IPHeader trusted.
	KeyHeader = "X-Visitor-IP-Key"
	// MinKeyLength is the shortest key the middleware accepts; a shorter one disables it.
	MinKeyLength = 32
)

// Middleware honours IPHeader only on requests that carry key in KeyHeader. Mount it right after
// ratelimit.TrustedRealIP and before any limiter. An empty key, or one shorter than
// MinKeyLength, turns forwarding off: the headers are still stripped and nothing is trusted.
func Middleware(key string) func(http.Handler) http.Handler {
	enabled := len(key) >= MinKeyLength
	want := sha256.Sum256([]byte(key))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := strings.TrimSpace(r.Header.Get(IPHeader))
			got := r.Header.Get(KeyHeader)
			r.Header.Del(IPHeader)
			r.Header.Del(KeyHeader)
			if enabled && got != "" && net.ParseIP(ip) != nil && keyMatches(want, got) {
				r.Header.Set("X-Real-IP", ip)
				r.RemoteAddr = net.JoinHostPort(ip, "0")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// keyMatches compares digests so the comparison takes the same time whatever the length of got.
func keyMatches(want [sha256.Size]byte, got string) bool {
	g := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(want[:], g[:]) == 1
}
