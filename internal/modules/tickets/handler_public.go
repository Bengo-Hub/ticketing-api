package tickets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"github.com/bengobox/ticketing-service/internal/platform/tenantcache"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// otpValidator validates OTP tokens and returns the verified email.
type otpValidator interface {
	Validate(ctx context.Context, token string) (email string, err error)
}

// rateLimiter enforces per-key request limits (shared-ratelimit's Limiter: GCRA in Redis,
// exact across replicas, per-pod fallback while Redis is down).
type rateLimiter interface {
	Allow(ctx context.Context, key string, opts ratelimit.Options, cost int) (bool, time.Duration)
}

// PublicHandler handles unauthenticated public ticket endpoints.
type PublicHandler struct {
	service *Service
	otp     otpValidator
	limiter rateLimiter
	tenants tenantcache.Lookup
	access  *ticketAccessSigner
	logger  *zap.Logger
}

// SetTenantLookup wires the slug lookup (tenantcache.Resolver: Redis, then auth-api).
func (h *PublicHandler) SetTenantLookup(l tenantcache.Lookup) { h.tenants = l }

// SetAccessKey wires the secret ticket access tokens are signed with (INTERNAL_SERVICE_KEY;
// the signing key is derived from it). Without one, the public read refuses every request.
func (h *PublicHandler) SetAccessKey(secret string) { h.access = newTicketAccessSigner(secret) }

// NewPublicHandler creates a handler for the public ticket endpoints.
func NewPublicHandler(service *Service, otp otpValidator, limiter rateLimiter, logger *zap.Logger) *PublicHandler {
	return &PublicHandler{
		service: service,
		otp:     otp,
		limiter: limiter,
		logger:  logger.Named("public-tickets-handler"),
	}
}

// RegisterPublicRoutes mounts public routes on a tenant-scoped router. They take no JWT: create
// takes an auth-api one-use OTP token, access trades one for a ticket access token, the read
// takes a ticket access token, and a comment takes either.
func (h *PublicHandler) RegisterPublicRoutes(r chi.Router) {
	r.Route("/public", func(pub chi.Router) {
		pub.Post("/tickets", h.CreatePublicTicket)
		pub.Get("/tickets/{ticketNumber}", h.GetPublicTicket)
		pub.Post("/tickets/{ticketNumber}/access", h.RequestTicketAccess)
		pub.Post("/tickets/{ticketNumber}/comments", h.AddPublicComment)
		// CSAT survey: the single-use token from the survey email is the only credential.
		pub.Post("/csat/check", h.CheckCSAT)
		pub.Post("/csat", h.SubmitCSAT)
	})
}

// CreatePublicTicket accepts an unauthenticated ticket submission.
// Requires X-OTP-Token header validated against auth-api.
func (h *PublicHandler) CreatePublicTicket(w http.ResponseWriter, r *http.Request) {
	tenantSlug := chi.URLParam(r, "tenant")
	tenantID, err := h.resolveTenantID(r, tenantSlug)
	if err != nil {
		h.tenantError(w, err)
		return
	}

	// The body is checked before the token is traded: a token works once, so a typo must not burn it.
	var req CreatePublicTicketRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = tenantID

	req.Subject, req.Description = strings.TrimSpace(req.Subject), strings.TrimSpace(req.Description)
	if req.Subject == "" || req.Description == "" {
		respondError(w, http.StatusBadRequest, "subject and description are required")
		return
	}
	if len([]rune(req.Subject)) > 200 || len([]rune(req.Description)) > 5000 || len([]rune(req.CustomerName)) > 120 {
		respondError(w, http.StatusBadRequest, "subject, description or name is too long")
		return
	}
	// Validate OTP token → get verified email
	otpToken := r.Header.Get("X-OTP-Token")
	if otpToken == "" {
		respondError(w, http.StatusUnauthorized, "X-OTP-Token header required")
		return
	}

	email, err := h.otp.Validate(r.Context(), otpToken)
	if err != nil {
		h.logger.Warn("otp validation failed", zap.Error(err))
		respondError(w, http.StatusUnauthorized, "invalid or expired OTP token")
		return
	}

	// Rate limit by IP (5 per hour) and by verified email (10 per day). The old limiter reset
	// the TTL on every hit, so a client that kept trying never got out of the window.
	ip := ratelimit.ClientIP(r)
	if h.limiter != nil {
		if ok, _ := h.limiter.Allow(r.Context(), tenantSlug+":"+ip, ratelimit.Options{Name: "public-ticket-ip", Limit: 5, Window: time.Hour}, 1); !ok {
			respondError(w, http.StatusTooManyRequests, "rate limit exceeded: too many tickets from this IP")
			return
		}
		if ok, _ := h.limiter.Allow(r.Context(), tenantSlug+":"+strings.ToLower(email), ratelimit.Options{Name: "public-ticket-email", Limit: 10, Window: 24 * time.Hour}, 1); !ok {
			respondError(w, http.StatusTooManyRequests, "rate limit exceeded: too many tickets from this email")
			return
		}
	}

	req.CustomerEmail = email

	if req.Channel == "" {
		req.Channel = ChannelWeb
	}

	// Create ticket with a synthetic customer UUID derived from email
	customerID := emailToUUID(email)
	createReq := &CreateTicketRequest{
		TenantID:    tenantID,
		CustomerID:  customerID,
		CategoryID:  req.CategoryID,
		Subject:     req.Subject,
		Description: req.Description,
		Priority:    PriorityNormal,
		Channel:     req.Channel,
		OrderID:     req.OrderID,
		Metadata:    map[string]any{"source": "public_form"},
	}
	// Stored as requester_* like every other create path (the verified email, never the body's).
	createReq.RequesterName = strings.TrimSpace(req.CustomerName)
	createReq.RequesterEmail = email

	ticket, err := h.service.CreateTicket(r.Context(), createReq)
	if err != nil {
		h.logger.Error("failed to create public ticket", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to create ticket")
		return
	}

	respondJSON(w, http.StatusCreated, PublicTicketResponse{
		TicketID:     ticket.ID,
		TicketNumber: ticket.TicketNumber,
		Status:       ticket.Status,
	})
}

// publicTicketView is the slim view a verified requester sees: no internal fields.
func publicTicketView(t *Ticket) map[string]any {
	return map[string]any{
		"ticket_number": t.TicketNumber,
		"status":        t.Status,
		"subject":       t.Subject,
		"channel":       t.Channel,
		"created_at":    t.CreatedAt,
		"updated_at":    t.UpdatedAt,
	}
}

// RequestTicketAccess trades a one-use OTP token for a ticket access token
// (TicketAccessTTL) scoped to this tenant, this ticket and the verified email. It is issued
// only when that email is the ticket's requester; a missing ticket and a different email get
// the same 404, so the route never confirms that someone else's ticket exists.
// POST /{tenant}/public/tickets/{ticketNumber}/access  (X-OTP-Token)
func (h *PublicHandler) RequestTicketAccess(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r, chi.URLParam(r, "tenant"))
	if err != nil {
		h.tenantError(w, err)
		return
	}
	if h.access == nil {
		respondError(w, http.StatusServiceUnavailable, "ticket access is not available")
		return
	}
	otpToken := r.Header.Get("X-OTP-Token")
	if otpToken == "" {
		respondError(w, http.StatusUnauthorized, "X-OTP-Token header required")
		return
	}
	email, err := h.otp.Validate(r.Context(), otpToken)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "invalid or expired OTP token")
		return
	}

	ticket, err := h.service.GetTicketByNumber(r.Context(), tenantID, chi.URLParam(r, "ticketNumber"))
	if err != nil && !errors.Is(err, ErrTicketNotFound) {
		respondError(w, http.StatusInternalServerError, "failed to get ticket")
		return
	}
	if err != nil || !canReplyAsRequester(ticket, email) {
		respondError(w, http.StatusNotFound, "ticket not found")
		return
	}

	token, expires, err := h.access.Issue(tenantID, ticket.TicketNumber, email)
	if err != nil {
		h.logger.Error("failed to issue ticket access token", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to issue access")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"expires_at":   expires,
		"ticket":       publicTicketView(ticket),
	})
}

// GetPublicTicket returns a ticket's slim view to its verified requester. It needs a ticket
// access token for this tenant and ticket; without one it answers 401 and shows nothing, and
// a token for another ticket, or whose email is no longer the requester, gets a 404.
func (h *PublicHandler) GetPublicTicket(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r, chi.URLParam(r, "tenant"))
	if err != nil {
		h.tenantError(w, err)
		return
	}
	claims, ok := h.accessClaims(w, r)
	if !ok {
		return
	}
	ticket, ok := h.ticketForClaims(w, r, tenantID, claims)
	if !ok {
		return
	}
	respondJSON(w, http.StatusOK, publicTicketView(ticket))
}

// accessClaims reads and checks the ticket access token header, answering 401 when it is
// missing, forged or expired.
func (h *PublicHandler) accessClaims(w http.ResponseWriter, r *http.Request) (ticketAccessClaims, bool) {
	token := r.Header.Get(TicketAccessHeader)
	if token == "" {
		respondError(w, http.StatusUnauthorized, TicketAccessHeader+" header required")
		return ticketAccessClaims{}, false
	}
	claims, err := h.access.Verify(token)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "invalid or expired ticket access token")
		return ticketAccessClaims{}, false
	}
	return claims, true
}

// ticketForClaims loads the route's ticket and checks it is the one the claims cover. A
// missing ticket and an out-of-scope one both answer 404.
func (h *PublicHandler) ticketForClaims(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, claims ticketAccessClaims) (*Ticket, bool) {
	ticket, err := h.service.GetTicketByNumber(r.Context(), tenantID, chi.URLParam(r, "ticketNumber"))
	if err != nil && !errors.Is(err, ErrTicketNotFound) {
		respondError(w, http.StatusInternalServerError, "failed to get ticket")
		return nil, false
	}
	if err != nil || !claims.allows(tenantID, ticket) {
		respondError(w, http.StatusNotFound, "ticket not found")
		return nil, false
	}
	return ticket, true
}

// AddPublicComment lets a customer reply to their ticket. It takes a ticket access token
// (TicketAccessHeader, reusable until it expires) or, as before, a one-use X-OTP-Token, which
// the chat widget and the MarketFlow support form still send.
func (h *PublicHandler) AddPublicComment(w http.ResponseWriter, r *http.Request) {
	tenantSlug := chi.URLParam(r, "tenant")
	tenantID, err := h.resolveTenantID(r, tenantSlug)
	if err != nil {
		h.tenantError(w, err)
		return
	}

	var (
		email  string
		ticket *Ticket
	)
	if r.Header.Get(TicketAccessHeader) != "" {
		claims, ok := h.accessClaims(w, r)
		if !ok {
			return
		}
		if ticket, ok = h.ticketForClaims(w, r, tenantID, claims); !ok {
			return
		}
		email = claims.Email
	} else {
		otpToken := r.Header.Get("X-OTP-Token")
		if otpToken == "" {
			respondError(w, http.StatusUnauthorized, "X-OTP-Token or "+TicketAccessHeader+" header required")
			return
		}
		email, err = h.otp.Validate(r.Context(), otpToken)
		if err != nil {
			respondError(w, http.StatusUnauthorized, "invalid or expired OTP token")
			return
		}

		ticketNumber := chi.URLParam(r, "ticketNumber")
		ticket, err = h.service.GetTicketByNumber(r.Context(), tenantID, ticketNumber)
		if err != nil {
			if errors.Is(err, ErrTicketNotFound) {
				respondError(w, http.StatusNotFound, "ticket not found")
				return
			}
			respondError(w, http.StatusInternalServerError, "failed to get ticket")
			return
		}

		// Only the requester replies here: the verified email must be the ticket's requester email.
		// A ticket with no requester email (a phone-only S2S ticket) takes no public replies.
		if !canReplyAsRequester(ticket, email) {
			respondError(w, http.StatusForbidden, "not authorized for this ticket")
			return
		}
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Content == "" {
		respondError(w, http.StatusBadRequest, "content is required")
		return
	}

	comment, err := h.service.AddComment(r.Context(), &AddCommentRequest{
		TenantID:    tenantID,
		TicketID:    ticket.ID,
		AuthorID:    emailToUUID(email),
		AuthorType:  AuthorCustomer,
		CommentType: CommentPublic,
		Content:     body.Content,
	})
	if err != nil {
		h.logger.Error("failed to add public comment", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to add comment")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"comment_id": comment.ID,
		"created_at": comment.CreatedAt,
	})
}

// resolveTenantID looks up the tenant UUID from the JWT claims already in context,
// or falls back to parsing the tenant slug from the URL.
// Public endpoints don't have JWT claims, so we parse the slug from the X-Tenant-ID header
// or derive it from context. For now, we use a deterministic UUID from the slug.
// resolveTenantID turns the route's tenant slug into auth-api's tenant id. It used to derive
// uuid5(DNS, slug), which matches no real tenant (auth-api ids are random), so public tickets
// were stored where no agent could see them. An unknown slug is now refused.
func (h *PublicHandler) resolveTenantID(r *http.Request, slug string) (uuid.UUID, error) {
	if slug == "" {
		return uuid.Nil, errors.New("tenant slug required")
	}
	id, ok := tenantcache.PublicTenantID(r.Context(), h.tenants, slug)
	if !ok {
		return uuid.Nil, errUnknownTenant
	}
	return id, nil
}

var errUnknownTenant = errors.New("unknown tenant")

func (h *PublicHandler) tenantError(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnknownTenant) {
		respondError(w, http.StatusNotFound, "tenant not found")
		return
	}
	respondError(w, http.StatusBadRequest, "invalid tenant")
}

// canReplyAsRequester reports whether a verified email is the ticket's requester (requester_email,
// or the legacy customer_email of older public tickets).
func canReplyAsRequester(t *Ticket, verifiedEmail string) bool {
	want := RequesterOf(t).Email
	return want != "" && strings.EqualFold(want, strings.TrimSpace(verifiedEmail))
}

// emailToUUID derives a deterministic UUID from an email address.
// Used as the customer_id for public (non-SSO) ticket submissions.
func emailToUUID(email string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.ToLower(email)))
}

// realIP extracts the real client IP, respecting X-Forwarded-For.
