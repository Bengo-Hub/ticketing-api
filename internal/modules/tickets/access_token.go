package tickets

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Ticket access tokens let a customer who proved their email once (with an auth-api one-use
// code) read and reply to one ticket for a while without a new code each time. A token is
// scoped to one tenant, one ticket number and the requester email, and is only issued when the
// verified email is that ticket's requester. It is stateless: an HMAC-SHA256 signature over a
// small JSON claim, so every replica can check it without Redis.

// TicketAccessTTL is how long a ticket access token works.
const TicketAccessTTL = 30 * time.Minute

// TicketAccessHeader carries the token on the public read and comment routes.
const TicketAccessHeader = "X-Ticket-Access-Token"

// ticketAccessKeyLabel separates this key from every other use of the service key.
const ticketAccessKeyLabel = "ticketing-api/public-ticket-access/v1"

var (
	errAccessDisabled = errors.New("ticket access tokens are not configured")
	errAccessInvalid  = errors.New("invalid ticket access token")
	errAccessExpired  = errors.New("ticket access token expired")
)

// ticketAccessClaims is the signed body of a token.
type ticketAccessClaims struct {
	Tenant string `json:"t"`
	Ticket string `json:"n"`
	Email  string `json:"e"`
	Expiry int64  `json:"x"`
}

// ticketAccessSigner issues and checks ticket access tokens.
type ticketAccessSigner struct {
	key []byte
	now func() time.Time
}

// newTicketAccessSigner derives the signing key from a service secret (INTERNAL_SERVICE_KEY)
// through HMAC with a fixed label, so the raw secret never signs anything itself. An empty
// secret gives a nil signer: no tokens are issued and none are accepted.
func newTicketAccessSigner(secret string) *ticketAccessSigner {
	if strings.TrimSpace(secret) == "" {
		return nil
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ticketAccessKeyLabel))
	return &ticketAccessSigner{key: m.Sum(nil), now: time.Now}
}

func (s *ticketAccessSigner) sign(body string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Issue returns a token for one ticket and email, and when it expires.
func (s *ticketAccessSigner) Issue(tenantID uuid.UUID, ticketNumber, email string) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, errAccessDisabled
	}
	exp := s.now().Add(TicketAccessTTL).UTC().Truncate(time.Second)
	raw, err := json.Marshal(ticketAccessClaims{
		Tenant: tenantID.String(),
		Ticket: ticketNumber,
		Email:  normalizeEmail(email),
		Expiry: exp.Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + s.sign(body), exp, nil
}

// Verify checks the signature and expiry and returns the claims. The caller still checks the
// scope (tenant, ticket number, requester email) against the ticket it is about to serve.
func (s *ticketAccessSigner) Verify(token string) (ticketAccessClaims, error) {
	if s == nil {
		return ticketAccessClaims{}, errAccessDisabled
	}
	body, sig, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || body == "" || sig == "" {
		return ticketAccessClaims{}, errAccessInvalid
	}
	if !hmac.Equal([]byte(sig), []byte(s.sign(body))) {
		return ticketAccessClaims{}, errAccessInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return ticketAccessClaims{}, errAccessInvalid
	}
	var c ticketAccessClaims
	if err := json.Unmarshal(raw, &c); err != nil || c.Tenant == "" || c.Ticket == "" || c.Email == "" {
		return ticketAccessClaims{}, errAccessInvalid
	}
	if !s.now().Before(time.Unix(c.Expiry, 0)) {
		return ticketAccessClaims{}, errAccessExpired
	}
	return c, nil
}

// allows reports whether the claims cover this ticket: same tenant, same ticket number, and
// the token's email is still the ticket's requester.
func (c ticketAccessClaims) allows(tenantID uuid.UUID, t *Ticket) bool {
	return t != nil && c.Tenant == tenantID.String() && c.Ticket == t.TicketNumber && canReplyAsRequester(t, c.Email)
}
