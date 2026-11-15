package tickets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func accessTicket(number, requester string) *Ticket {
	return &Ticket{
		ID:           uuid.New(),
		TicketNumber: number,
		Subject:      "Card declined twice",
		Status:       StatusOpen,
		Metadata:     map[string]any{MetaRequesterEmail: requester},
	}
}

func TestTicketAccessTokenIssueAndScope(t *testing.T) {
	s := newTicketAccessSigner("service-key")
	tenant := uuid.New()
	tk := accessTicket("TKT-000042", "jane@example.com")

	token, exp, err := s.Issue(tenant, tk.TicketNumber, "Jane@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(exp); d < TicketAccessTTL-time.Minute || d > TicketAccessTTL {
		t.Fatalf("expiry %v is not about %v away", exp, TicketAccessTTL)
	}
	c, err := s.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if !c.allows(tenant, tk) {
		t.Fatal("the token must cover its own ticket")
	}
	if c.allows(uuid.New(), tk) {
		t.Fatal("another tenant must not be covered")
	}
	if c.allows(tenant, accessTicket("TKT-000043", "jane@example.com")) {
		t.Fatal("another ticket must not be covered")
	}
	if c.allows(tenant, accessTicket("TKT-000042", "someone@example.com")) {
		t.Fatal("a ticket whose requester is now someone else must not be covered")
	}
}

func TestTicketAccessTokenExpiry(t *testing.T) {
	s := newTicketAccessSigner("service-key")
	start := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return start }
	token, _, err := s.Issue(uuid.New(), "TKT-1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return start.Add(TicketAccessTTL - time.Second) }
	if _, err := s.Verify(token); err != nil {
		t.Fatalf("still valid just before expiry: %v", err)
	}
	s.now = func() time.Time { return start.Add(TicketAccessTTL) }
	if _, err := s.Verify(token); err != errAccessExpired {
		t.Fatalf("want expired, got %v", err)
	}
}

func TestTicketAccessTokenRejectsForgery(t *testing.T) {
	s := newTicketAccessSigner("service-key")
	token, _, _ := s.Issue(uuid.New(), "TKT-1", "a@example.com")
	body, sig, _ := strings.Cut(token, ".")

	other := newTicketAccessSigner("another-key")
	if _, err := other.Verify(token); err == nil {
		t.Fatal("a token signed with another key must fail")
	}
	forged, _, _ := other.Issue(uuid.New(), "TKT-1", "a@example.com")
	forgedBody, _, _ := strings.Cut(forged, ".")
	for name, tok := range map[string]string{
		"swapped body": forgedBody + "." + sig,
		"bad sig":      body + ".AAAA",
		"no sig":       body,
		"empty":        "",
	} {
		if _, err := s.Verify(tok); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	var none *ticketAccessSigner
	if newTicketAccessSigner("  ") != nil {
		t.Fatal("an empty secret must give no signer")
	}
	if _, _, err := none.Issue(uuid.New(), "TKT-1", "a@example.com"); err != errAccessDisabled {
		t.Fatalf("issue without a key: %v", err)
	}
	if _, err := none.Verify(token); err != errAccessDisabled {
		t.Fatalf("verify without a key: %v", err)
	}
}

// ── Handler: the public read proves the email ────────────────────────────────

type byNumberRepo struct {
	Repository
	tickets  map[string]*Ticket
	comments int
}

func (r *byNumberRepo) GetTicketByNumber(_ context.Context, _ uuid.UUID, n string) (*Ticket, error) {
	t, ok := r.tickets[n]
	if !ok {
		return nil, ErrTicketNotFound
	}
	cp := *t
	return &cp, nil
}

// fakeOTP maps one-use tokens to verified emails and forgets each token once used.
type fakeOTP map[string]string

func (f fakeOTP) Validate(_ context.Context, token string) (string, error) {
	email, ok := f[token]
	if !ok {
		return "", ErrTicketNotFound
	}
	delete(f, token)
	return email, nil
}

func publicRouter(t *testing.T, otp fakeOTP, tickets ...*Ticket) (http.Handler, uuid.UUID) {
	h, tenant, _ := publicRouterWithRepo(t, otp, tickets...)
	return h, tenant
}

func publicRouterWithRepo(t *testing.T, otp fakeOTP, tickets ...*Ticket) (http.Handler, uuid.UUID, *byNumberRepo) {
	t.Helper()
	tenant := uuid.New()
	repo := &byNumberRepo{tickets: map[string]*Ticket{}}
	for _, tk := range tickets {
		repo.tickets[tk.TicketNumber] = tk
	}
	h := NewPublicHandler(NewService(repo, zap.NewNop()), otp, nil, zap.NewNop())
	h.SetTenantLookup(fakeTenants{"acme": tenant.String()})
	h.SetAccessKey("service-key")
	r := chi.NewRouter()
	r.Route("/{tenant}", h.RegisterPublicRoutes)
	return r, tenant, repo
}

func (r *byNumberRepo) CreateComment(context.Context, uuid.UUID, *TicketComment) error {
	r.comments++
	return nil
}

func call(h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	return callWithBody(h, method, path, headers, "")
}

func callWithBody(h http.Handler, method, path string, headers map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPublicTicketReadNeedsAccessToken(t *testing.T) {
	otp := fakeOTP{"otp-jane": "jane@example.com", "otp-mallory": "mallory@example.com", "otp-jane-2": "jane@example.com"}
	h, _ := publicRouter(t, otp,
		accessTicket("TKT-000042", "jane@example.com"),
		accessTicket("TKT-000099", "bob@example.com"))

	// No token, or a made-up one: 401 and nothing about the ticket.
	for _, hdr := range []map[string]string{nil, {TicketAccessHeader: "nope.nope"}} {
		rec := call(h, http.MethodGet, "/acme/public/tickets/TKT-000042", hdr)
		if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "Card declined") ||
			strings.Contains(rec.Body.String(), "open") {
			t.Fatalf("read without access: %d %s", rec.Code, rec.Body.String())
		}
	}

	// A verified email that is not the requester gets the same 404 as a missing ticket.
	if rec := call(h, http.MethodPost, "/acme/public/tickets/TKT-000042/access", map[string]string{"X-OTP-Token": "otp-mallory"}); rec.Code != http.StatusNotFound {
		t.Fatalf("wrong email: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/acme/public/tickets/TKT-404/access", map[string]string{"X-OTP-Token": "otp-jane-2"}); rec.Code != http.StatusNotFound {
		t.Fatalf("missing ticket: %d", rec.Code)
	}

	// The requester gets a token, and the token reads that ticket only.
	rec := call(h, http.MethodPost, "/acme/public/tickets/TKT-000042/access", map[string]string{"X-OTP-Token": "otp-jane"})
	if rec.Code != http.StatusOK {
		t.Fatalf("access: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		AccessToken string         `json:"access_token"`
		Ticket      map[string]any `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.AccessToken == "" || got.Ticket["subject"] != "Card declined twice" {
		t.Fatalf("access body: %v %s", err, rec.Body.String())
	}
	if rec := call(h, http.MethodPost, "/acme/public/tickets/TKT-000042/access", map[string]string{"X-OTP-Token": "otp-jane"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a spent OTP token must not issue again: %d", rec.Code)
	}
	auth := map[string]string{TicketAccessHeader: got.AccessToken}
	for i := 0; i < 2; i++ {
		rec = call(h, http.MethodGet, "/acme/public/tickets/TKT-000042", auth)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Card declined twice") {
			t.Fatalf("read %d with access: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if rec := call(h, http.MethodGet, "/acme/public/tickets/TKT-000099", auth); rec.Code != http.StatusNotFound {
		t.Fatalf("another ticket with this token: %d", rec.Code)
	}
}

// A comment with a bad access token is refused before the body is read; the plain OTP path
// still answers as before for a missing header.
func TestPublicCommentAccessHeaders(t *testing.T) {
	h, _ := publicRouter(t, fakeOTP{}, accessTicket("TKT-000042", "jane@example.com"))
	if rec := call(h, http.MethodPost, "/acme/public/tickets/TKT-000042/comments", map[string]string{TicketAccessHeader: "x.y"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad access token: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/acme/public/tickets/TKT-000042/comments", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/acme/public/tickets/TKT-000042/comments", map[string]string{"X-OTP-Token": "unknown"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown OTP token: %d", rec.Code)
	}
}

// One access token carries several replies; it does not reach another ticket, and the OTP
// path keeps working for the chat widget and the MarketFlow support form.
func TestPublicCommentWithAccessToken(t *testing.T) {
	otp := fakeOTP{"otp-jane": "jane@example.com", "otp-jane-2": "jane@example.com"}
	h, _, repo := publicRouterWithRepo(t, otp,
		accessTicket("TKT-000042", "jane@example.com"),
		accessTicket("TKT-000099", "bob@example.com"))

	rec := call(h, http.MethodPost, "/acme/public/tickets/TKT-000042/access", map[string]string{"X-OTP-Token": "otp-jane"})
	var got struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.AccessToken == "" {
		t.Fatalf("access: %d %s", rec.Code, rec.Body.String())
	}
	auth := map[string]string{TicketAccessHeader: got.AccessToken}
	for i := 0; i < 2; i++ {
		if rec := callWithBody(h, http.MethodPost, "/acme/public/tickets/TKT-000042/comments", auth, `{"content":"Any news?"}`); rec.Code != http.StatusCreated {
			t.Fatalf("reply %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if rec := callWithBody(h, http.MethodPost, "/acme/public/tickets/TKT-000099/comments", auth, `{"content":"hi"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("reply on another ticket: %d", rec.Code)
	}
	if rec := callWithBody(h, http.MethodPost, "/acme/public/tickets/TKT-000042/comments", map[string]string{"X-OTP-Token": "otp-jane-2"}, `{"content":"Still there?"}`); rec.Code != http.StatusCreated {
		t.Fatalf("reply with an OTP token: %d %s", rec.Code, rec.Body.String())
	}
	if repo.comments != 3 {
		t.Fatalf("want 3 comments stored, got %d", repo.comments)
	}
}
