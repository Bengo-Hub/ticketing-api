package tickets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// otpValidator validates OTP tokens and returns the verified email.
type otpValidator interface {
	Validate(ctx context.Context, token string) (email string, err error)
}

// rateLimiter enforces per-key request limits.
type rateLimiter interface {
	Allow(ctx context.Context, key string, maxCount int, window time.Duration) (bool, error)
}

// PublicHandler handles unauthenticated public ticket endpoints.
type PublicHandler struct {
	service  *Service
	otp      otpValidator
	limiter  rateLimiter
	logger   *zap.Logger
}

// NewPublicHandler creates a handler for the public ticket endpoints.
func NewPublicHandler(service *Service, otp otpValidator, limiter rateLimiter, logger *zap.Logger) *PublicHandler {
	return &PublicHandler{
		service: service,
		otp:     otp,
		limiter: limiter,
		logger:  logger.Named("public-tickets-handler"),
	}
}

// RegisterPublicRoutes mounts public routes on a tenant-scoped router.
// These routes require NO JWT auth — OTP header is used instead.
func (h *PublicHandler) RegisterPublicRoutes(r chi.Router) {
	r.Route("/public", func(pub chi.Router) {
		pub.Post("/tickets", h.CreatePublicTicket)
		pub.Get("/tickets/{ticketNumber}", h.GetPublicTicket)
		pub.Post("/tickets/{ticketNumber}/comments", h.AddPublicComment)
	})
}

// CreatePublicTicket accepts an unauthenticated ticket submission.
// Requires X-OTP-Token header validated against auth-api.
func (h *PublicHandler) CreatePublicTicket(w http.ResponseWriter, r *http.Request) {
	tenantSlug := chi.URLParam(r, "tenant")
	tenantID, err := h.resolveTenantID(r, tenantSlug)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant")
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

	// Rate limit by IP (5 per hour)
	ip := realIP(r)
	if h.limiter != nil {
		ok, err := h.limiter.Allow(r.Context(), "rl:ip:"+tenantSlug+":"+ip, 5, time.Hour)
		if err != nil {
			h.logger.Warn("rate limit check failed", zap.Error(err))
		} else if !ok {
			respondError(w, http.StatusTooManyRequests, "rate limit exceeded: too many tickets from this IP")
			return
		}
		// Rate limit by email (10 per 24h)
		ok, err = h.limiter.Allow(r.Context(), "rl:email:"+tenantSlug+":"+email, 10, 24*time.Hour)
		if err != nil {
			h.logger.Warn("rate limit check failed", zap.Error(err))
		} else if !ok {
			respondError(w, http.StatusTooManyRequests, "rate limit exceeded: too many tickets from this email")
			return
		}
	}

	var req CreatePublicTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = tenantID
	req.CustomerEmail = email

	if req.Subject == "" || req.Description == "" {
		respondError(w, http.StatusBadRequest, "subject and description are required")
		return
	}
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
		Metadata:    map[string]any{"customer_email": email, "customer_name": req.CustomerName},
	}

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

// GetPublicTicket returns the status of a ticket by number without auth.
func (h *PublicHandler) GetPublicTicket(w http.ResponseWriter, r *http.Request) {
	tenantSlug := chi.URLParam(r, "tenant")
	tenantID, err := h.resolveTenantID(r, tenantSlug)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant")
		return
	}

	ticketNumber := chi.URLParam(r, "ticketNumber")
	ticket, err := h.service.GetTicketByNumber(r.Context(), tenantID, ticketNumber)
	if err != nil {
		if errors.Is(err, ErrTicketNotFound) {
			respondError(w, http.StatusNotFound, "ticket not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get ticket")
		return
	}

	// Return slim public view — no internal fields
	respondJSON(w, http.StatusOK, map[string]any{
		"ticket_number": ticket.TicketNumber,
		"status":        ticket.Status,
		"subject":       ticket.Subject,
		"channel":       ticket.Channel,
		"created_at":    ticket.CreatedAt,
		"updated_at":    ticket.UpdatedAt,
	})
}

// AddPublicComment lets a customer reply to their ticket via OTP.
func (h *PublicHandler) AddPublicComment(w http.ResponseWriter, r *http.Request) {
	tenantSlug := chi.URLParam(r, "tenant")
	tenantID, err := h.resolveTenantID(r, tenantSlug)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant")
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

	ticketNumber := chi.URLParam(r, "ticketNumber")
	ticket, err := h.service.GetTicketByNumber(r.Context(), tenantID, ticketNumber)
	if err != nil {
		if errors.Is(err, ErrTicketNotFound) {
			respondError(w, http.StatusNotFound, "ticket not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get ticket")
		return
	}

	// Verify the customer email matches what's in the ticket metadata
	if meta, ok := ticket.Metadata["customer_email"].(string); ok {
		if !strings.EqualFold(meta, email) {
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
func (h *PublicHandler) resolveTenantID(_ *http.Request, slug string) (uuid.UUID, error) {
	if slug == "" {
		return uuid.Nil, errors.New("tenant slug required")
	}
	// Derive a deterministic tenant UUID from the slug via UUIDv5 (DNS namespace).
	// This matches the convention used by auth-api when it provisions tenants.
	id := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(slug))
	return id, nil
}

// emailToUUID derives a deterministic UUID from an email address.
// Used as the customer_id for public (non-SSO) ticket submissions.
func emailToUUID(email string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.ToLower(email)))
}

// realIP extracts the real client IP, respecting X-Forwarded-For.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
