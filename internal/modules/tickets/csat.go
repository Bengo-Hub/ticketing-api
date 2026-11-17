package tickets

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// CSAT surveys. Resolving a ticket whose requester has an email issues a single-use survey token:
// the token goes to the customer in the ticketing.ticket.csat_requested event, and only its
// SHA-256 stays on the ticket, in metadata csat. The public submit route checks the token against
// that hash, stores the score in csat_responses and marks the token used in the same transaction,
// so a token gives one response and stops working at its expiry. Resolving the ticket again issues
// a new token, which replaces the old one.

// EventTicketCSATRequested asks notifications-api to send the survey email (subject
// ticketing.ticket.csat_requested).
const EventTicketCSATRequested = "ticket.csat_requested"

// MetaCSAT is the ticket metadata key that holds the survey state:
// {token_hash, issued_at, expires_at[, used_at]}.
const MetaCSAT = "csat"

// DefaultCSATTokenTTL is how long a survey link works.
const DefaultCSATTokenTTL = 7 * 24 * time.Hour

// MaxCSATCommentRunes bounds the optional comment.
const MaxCSATCommentRunes = 2000

var (
	// ErrCSATTokenInvalid covers a malformed token, an unknown ticket and a token that is not
	// the ticket's current one, so the route never confirms which part was wrong.
	ErrCSATTokenInvalid = errors.New("tickets: invalid survey link")
	ErrCSATTokenUsed    = errors.New("tickets: this survey has already been answered")
	ErrCSATTokenExpired = errors.New("tickets: this survey link has expired")
	ErrCSATScore        = errors.New("tickets: score must be a whole number from 1 to 5")
	ErrCSATComment      = errors.New("tickets: comment is too long")
)

// CSATConfig sets the survey link and how long it works.
type CSATConfig struct {
	// SurveyURL is the public survey page; the link adds ?tenant=<tenant id>&token=<token>.
	// Empty sends no surveys.
	SurveyURL string
	// TTL is how long a link works; zero is DefaultCSATTokenTTL.
	TTL time.Duration
}

// SetCSAT wires the survey settings. Without a survey URL no survey is issued on resolve.
func (s *Service) SetCSAT(c CSATConfig) {
	if c.TTL <= 0 {
		c.TTL = DefaultCSATTokenTTL
	}
	s.csat = c
}

// CSATResponse is one stored rating.
type CSATResponse struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	TicketID    uuid.UUID
	Score       int
	Comment     string
	TokenHash   string
	SubmittedAt time.Time
}

// CSATSurvey is what the public survey page shows before the customer answers.
type CSATSurvey struct {
	TicketNumber string    `json:"ticket_number"`
	Subject      string    `json:"subject"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// csatTokenSecretBytes is the random part of a token.
const csatTokenSecretBytes = 24

// newCSATToken returns a token for the ticket and its hash. The token is the ticket id and 24
// random bytes, base64url without padding (54 characters), so the submit route finds the ticket
// without an index on the hash.
func newCSATToken(ticketID uuid.UUID) (token, hash string, err error) {
	raw := make([]byte, 16+csatTokenSecretBytes)
	copy(raw, ticketID[:])
	if _, err := rand.Read(raw[16:]); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, csatTokenHash(token), nil
}

// parseCSATToken returns the ticket id a token names and the token's hash.
func parseCSATToken(token string) (uuid.UUID, string, error) {
	token = strings.TrimSpace(token)
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 16+csatTokenSecretBytes {
		return uuid.Nil, "", ErrCSATTokenInvalid
	}
	id, err := uuid.FromBytes(raw[:16])
	if err != nil || id == uuid.Nil {
		return uuid.Nil, "", ErrCSATTokenInvalid
	}
	return id, csatTokenHash(token), nil
}

func csatTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// csatState is the survey state read from ticket metadata.
type csatState struct {
	TokenHash string
	ExpiresAt time.Time
	Used      bool
}

func readCSATState(meta map[string]any) (csatState, bool) {
	m, ok := meta[MetaCSAT].(map[string]any)
	if !ok {
		return csatState{}, false
	}
	st := csatState{TokenHash: metaText(m, "token_hash")}
	if exp, err := time.Parse(time.RFC3339, metaText(m, "expires_at")); err == nil {
		st.ExpiresAt = exp
	}
	st.Used = metaText(m, "used_at") != ""
	return st, st.TokenHash != "" && !st.ExpiresAt.IsZero()
}

// checkCSAT matches a token's hash against the ticket's survey state.
func checkCSAT(t *Ticket, hash string, now time.Time) error {
	st, ok := readCSATState(t.Metadata)
	if !ok || subtle.ConstantTimeCompare([]byte(st.TokenHash), []byte(hash)) != 1 {
		return ErrCSATTokenInvalid
	}
	if st.Used {
		return ErrCSATTokenUsed
	}
	if !now.Before(st.ExpiresAt) {
		return ErrCSATTokenExpired
	}
	return nil
}

// csatSurveyLink is the survey page link with the tenant id and token.
func csatSurveyLink(base string, tenantID uuid.UUID, token string) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	q := url.Values{}
	q.Set("tenant", tenantID.String())
	q.Set("token", token)
	return base + sep + q.Encode()
}

// csatTx issues the survey for a ticket being resolved: a new token whose hash replaces any
// earlier one in metadata, and the ticketing.ticket.csat_requested event carrying the link. A
// ticket with no requester email gets no survey (surveys go by email only), and nothing is
// issued while no survey URL is configured. It runs inside the resolve transaction (statusTx).
func (s *Service) csatTx(ctx context.Context, repo Repository, out *Outbox, t *Ticket, resolvedAt time.Time) error {
	if s.csat.SurveyURL == "" || RequesterOf(t).Email == "" {
		return nil
	}
	token, hash, err := newCSATToken(t.ID)
	if err != nil {
		return err
	}
	issued := resolvedAt.UTC().Truncate(time.Second)
	expires := issued.Add(s.csat.TTL)
	state := map[string]any{
		"token_hash": hash,
		"issued_at":  issued.Format(time.RFC3339),
		"expires_at": expires.Format(time.RFC3339),
	}
	if err := repo.MergeTicketMetadata(ctx, t.TenantID, t.ID, map[string]any{MetaCSAT: state}); err != nil {
		return err
	}
	link := csatSurveyLink(s.csat.SurveyURL, t.TenantID, token)
	out.Ticket(EventTicketCSATRequested, TicketCSATRequestedPayload(t, token, link, issued, expires))
	return nil
}

// TicketCSATRequestedPayload is ticketing.ticket.csat_requested: the ticket, its requester and the
// survey link. survey_channel is "email"; WhatsApp surveys are off by owner decision.
func TicketCSATRequestedPayload(t *Ticket, token, link string, resolvedAt, expiresAt time.Time) map[string]any {
	p := ticketEventPayload(t)
	p["status"] = string(StatusResolved)
	p["resolved_at"] = resolvedAt.UTC().Format(time.RFC3339)
	p["survey_channel"] = "email"
	p["csat_token"] = token
	p["csat_url"] = link
	p["csat_expires_at"] = expiresAt.UTC().Format(time.RFC3339)
	return p
}

// CheckCSAT returns what the survey page shows for a token that can still be answered.
func (s *Service) CheckCSAT(ctx context.Context, tenantID uuid.UUID, token string) (*CSATSurvey, error) {
	ticketID, hash, err := parseCSATToken(token)
	if err != nil {
		return nil, err
	}
	t, err := s.repo.GetTicket(ctx, tenantID, ticketID)
	if errors.Is(err, ErrTicketNotFound) {
		return nil, ErrCSATTokenInvalid
	}
	if err != nil {
		return nil, err
	}
	if err := checkCSAT(t, hash, s.now()); err != nil {
		return nil, err
	}
	st, _ := readCSATState(t.Metadata)
	return &CSATSurvey{TicketNumber: t.TicketNumber, Subject: t.Subject, ExpiresAt: st.ExpiresAt}, nil
}

// SubmitCSAT stores a rating for a token once. The token is consumed by a conditional update in
// the same transaction as the response row, so two submits racing for one token give one
// response; the loser gets ErrCSATTokenUsed.
func (s *Service) SubmitCSAT(ctx context.Context, tenantID uuid.UUID, token string, score int, comment string) error {
	if score < 1 || score > 5 {
		return ErrCSATScore
	}
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > MaxCSATCommentRunes {
		return ErrCSATComment
	}
	ticketID, hash, err := parseCSATToken(token)
	if err != nil {
		return err
	}
	now := s.now()
	return s.repo.InTx(ctx, tenantID, func(repo Repository, _ *Outbox) error {
		t, err := repo.GetTicket(ctx, tenantID, ticketID)
		if errors.Is(err, ErrTicketNotFound) {
			return ErrCSATTokenInvalid
		}
		if err != nil {
			return err
		}
		if err := checkCSAT(t, hash, now); err != nil {
			return err
		}
		consumed, err := repo.ConsumeCSATToken(ctx, tenantID, ticketID, hash, now)
		if err != nil {
			return err
		}
		if !consumed {
			return ErrCSATTokenUsed
		}
		return repo.CreateCSATResponse(ctx, &CSATResponse{
			ID:          uuid.New(),
			TenantID:    tenantID,
			TicketID:    ticketID,
			Score:       score,
			Comment:     comment,
			TokenHash:   hash,
			SubmittedAt: now,
		})
	})
}
