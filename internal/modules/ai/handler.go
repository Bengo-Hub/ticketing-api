package ai

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/tickets"
	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// Permission codes the AI routes check: the existing ticket and knowledge base permissions.
// Customers (own-only view) never reach a paid AI route.
const (
	permTicketsView = "ticketing.tickets.view"
	permTicketsAdd  = "ticketing.tickets.add"
	permCommentsAdd = "ticketing.comments.add"
	permKBView      = "ticketing.kb.view"
	permKBAdd       = "ticketing.kb.add"
)

// Handler serves the AI routes.
type Handler struct {
	service *Service
	logger  *zap.Logger
}

// NewHandler creates the AI handler.
func NewHandler(service *Service, logger *zap.Logger) *Handler {
	return &Handler{service: service, logger: logger.Named("ai-handler")}
}

// RegisterRoutes mounts the AI routes on the protected tenant router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	staff := access.RequirePermission(permTicketsView)
	r.Route("/ai", func(a chi.Router) {
		a.With(staff, access.RequirePermission(permTicketsAdd)).Post("/classify", h.Classify)
		a.Route("/tickets/{ticketId}", func(t chi.Router) {
			t.Use(staff)
			t.Post("/summary", h.Summarize)
			t.With(access.RequirePermission(permCommentsAdd)).Post("/reply", h.SuggestReply)
			t.With(access.RequirePermission(permKBView)).Post("/kb-suggestions", h.SuggestArticles)
			t.With(access.RequirePermission(permKBAdd)).Post("/kb-draft", h.DraftArticle)
		})
	})
}

// Classify suggests a category, priority and team for a draft ticket.
// @Summary      AI classify a draft ticket
// @Description  marketflow-ai classifies the draft; the label is matched to the tenant's categories and the team comes from its routing rules. Spends one AI credit (none for the platform owner). Send Idempotency-Key to make a retry free.
// @Tags         ai
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        Idempotency-Key header string false "Request key, 8 to 100 of A-Z a-z 0-9 . _ : -"
// @Param        body body ClassifyRequest true "Draft"
// @Success      200 {object} Classification
// @Failure      402 {object} map[string]string "ai_credits_exhausted"
// @Failure      503 {object} map[string]string "ai_unavailable or ai_credits_unknown"
// @Security     BearerAuth
// @Router       /{tenant}/ai/classify [post]
func (h *Handler) Classify(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	var req ClassifyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	out, err := h.service.Classify(r.Context(), a.TenantID, req, RequestKey(r.Header.Get("Idempotency-Key")))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// Summarize summarizes a ticket's thread for the agent.
// @Summary      AI summary of a ticket thread
// @Description  Internal notes are included (staff only). Spends one AI credit.
// @Tags         ai
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        ticketId path string true "Ticket ID"
// @Param        Idempotency-Key header string false "Request key"
// @Success      200 {object} Summary
// @Failure      402 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      503 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/ai/tickets/{ticketId}/summary [post]
func (h *Handler) Summarize(w http.ResponseWriter, r *http.Request) {
	t, ok := h.ticket(w, r)
	if !ok {
		return
	}
	out, err := h.service.Summarize(r.Context(), t, RequestKey(r.Header.Get("Idempotency-Key")))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// SuggestReply drafts a reply for the agent to edit. Nothing is sent.
// @Summary      AI suggested reply
// @Description  A draft reply to the customer's latest public message. It is only returned; the agent edits it and posts it as a comment. Spends one AI credit.
// @Tags         ai
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        ticketId path string true "Ticket ID"
// @Param        Idempotency-Key header string false "Request key"
// @Success      200 {object} ReplySuggestion
// @Failure      402 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      503 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/ai/tickets/{ticketId}/reply [post]
func (h *Handler) SuggestReply(w http.ResponseWriter, r *http.Request) {
	t, ok := h.ticket(w, r)
	if !ok {
		return
	}
	out, err := h.service.SuggestReply(r.Context(), t, RequestKey(r.Header.Get("Idempotency-Key")))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// SuggestArticles ranks the tenant's own published articles for a ticket.
// @Summary      AI knowledge base suggestions
// @Description  Up to 5 of the tenant's published articles, best first. With no published article the list is empty and nothing is spent; otherwise one AI credit.
// @Tags         ai
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        ticketId path string true "Ticket ID"
// @Param        Idempotency-Key header string false "Request key"
// @Success      200 {object} KBSuggestions
// @Failure      402 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      503 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/ai/tickets/{ticketId}/kb-suggestions [post]
func (h *Handler) SuggestArticles(w http.ResponseWriter, r *http.Request) {
	t, ok := h.ticket(w, r)
	if !ok {
		return
	}
	out, err := h.service.SuggestArticles(r.Context(), t, RequestKey(r.Header.Get("Idempotency-Key")))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// DraftArticle saves a draft article written from a resolved ticket.
// @Summary      AI article draft from a resolved ticket
// @Description  Saved as a private draft (status draft, is_public false), never published. The ticket must be resolved or closed. Spends one AI credit.
// @Tags         ai
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        ticketId path string true "Ticket ID"
// @Param        Idempotency-Key header string false "Request key"
// @Success      201 {object} KBDraft
// @Failure      402 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      409 {object} map[string]string "ticket_not_resolved"
// @Failure      503 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/ai/tickets/{ticketId}/kb-draft [post]
func (h *Handler) DraftArticle(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	t, ok := h.ticket(w, r)
	if !ok {
		return
	}
	out, err := h.service.DraftArticle(r.Context(), t, a.AuthUserID, RequestKey(r.Header.Get("Idempotency-Key")))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// ticket loads the route's ticket in the caller's tenant; another tenant's ticket is not found.
func (h *Handler) ticket(w http.ResponseWriter, r *http.Request) (*tickets.Ticket, bool) {
	a, ok := caller(w, r)
	if !ok {
		return nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid ticket ID")
		return nil, false
	}
	t, err := h.service.tickets.GetTicket(r.Context(), a.TenantID, id)
	if err != nil || t == nil || t.TenantID != a.TenantID {
		if err != nil && !errors.Is(err, tickets.ErrTicketNotFound) {
			h.logger.Error("ticket read for ai", zap.Error(err))
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to read the ticket")
			return nil, false
		}
		writeError(w, http.StatusNotFound, "ticket_not_found", "ticket not found")
		return nil, false
	}
	return t, true
}

// fail maps a service error to its HTTP answer. Every AI refusal is typed by code, so the UI hides
// the suggestion and the ticket action it decorates goes on.
func (h *Handler) fail(w http.ResponseWriter, err error) {
	var credit *CreditError
	switch {
	case errors.As(err, &credit):
		writeJSON(w, http.StatusPaymentRequired, map[string]string{
			"error": "Not enough AI credits. Top up AI credits to use AI suggestions.", "code": "ai_credits_exhausted", "reason": credit.Reason,
		})
	case errors.Is(err, ErrCreditsUnknown):
		writeError(w, http.StatusServiceUnavailable, "ai_credits_unknown", "AI credits could not be checked right now")
	case errors.Is(err, ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "AI is unavailable right now")
	case errors.Is(err, ErrEmptyDraft):
		writeError(w, http.StatusBadRequest, "invalid_request", "subject or description required")
	case errors.Is(err, ErrNotResolved):
		writeError(w, http.StatusConflict, "ticket_not_resolved", "an article can be drafted only from a resolved or closed ticket")
	case errors.Is(err, tickets.ErrTicketNotFound):
		writeError(w, http.StatusNotFound, "ticket_not_found", "ticket not found")
	default:
		h.logger.Error("ai request failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error", "AI request failed")
	}
}

func caller(w http.ResponseWriter, r *http.Request) (*access.Access, bool) {
	a := access.From(r.Context())
	if a == nil || a.TenantID == uuid.Nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return nil, false
	}
	return a, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
