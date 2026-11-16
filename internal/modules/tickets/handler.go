package tickets

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	pagination "github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// Permission codes the ticket routes check.
const (
	permTicketsAdd       = "ticketing.tickets.add"
	permTicketsView      = "ticketing.tickets.view"
	permTicketsViewOwn   = "ticketing.tickets.view_own"
	permTicketsChange    = "ticketing.tickets.change"
	permTicketsChangeOwn = "ticketing.tickets.change_own"
	permTicketsDelete    = "ticketing.tickets.delete"
	permCommentsAdd      = "ticketing.comments.add"
	permCommentsChange   = "ticketing.comments.change"
	permCommentsChgOwn   = "ticketing.comments.change_own"
	permCommentsDelete   = "ticketing.comments.delete"
	permCommentsDelOwn   = "ticketing.comments.delete_own"
	permAttachmentsView  = "ticketing.attachments.view"
	permAttachViewOwn    = "ticketing.attachments.view_own"
	permCategoriesAdd    = "ticketing.categories.add"
	permCategoriesView   = "ticketing.categories.view"
	permCategoriesChange = "ticketing.categories.change"
	permCategoriesDelete = "ticketing.categories.delete"
	permAnalyticsView    = "ticketing.analytics.view"
)

// Handler handles HTTP requests for ticket operations.
type Handler struct {
	service    *Service
	internal   internalCreator
	logger     *zap.Logger
	serviceKey string // INTERNAL_SERVICE_KEY for S2S calls
}

// NewHandler creates a new ticket handler.
func NewHandler(service *Service, logger *zap.Logger, opts ...func(*Handler)) *Handler {
	h := &Handler{service: service, internal: service, logger: logger.Named("tickets-handler")}
	for _, o := range opts {
		o(h)
	}
	return h
}

// WithServiceKey sets the internal service key for S2S endpoint validation.
func WithServiceKey(key string) func(*Handler) {
	return func(h *Handler) { h.serviceKey = key }
}

// RegisterInternalRoutes mounts the S2S routes (X-API-Key and X-Tenant-ID, no user token).
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.Post("/internal/tickets", h.InternalCreateTicket)
	r.Get("/internal/tickets/{ticketNumber}", h.InternalGetTicket)
}

// RegisterRoutes registers the ticket routes under the protected /{tenant} group.
func (h *Handler) RegisterRoutes(r chi.Router) {
	viewAny := access.RequireAnyPermission(permTicketsView, permTicketsViewOwn)
	r.Route("/tickets", func(t chi.Router) {
		t.With(access.RequirePermission(permTicketsAdd)).Post("/", h.CreateTicket)
		t.With(viewAny).Get("/", h.ListTickets)
		t.With(access.RequirePermission(permAnalyticsView)).Get("/stats", h.GetTicketStats)
		t.With(access.RequirePermission(permAnalyticsView)).Get("/analytics", h.GetAnalytics)
		t.With(access.RequirePermission(permTicketsAdd)).Post("/classify", h.ClassifyTicket)
		t.With(viewAny).Get("/number/{ticketNumber}", h.GetTicketByNumber)
		t.With(viewAny).Get("/{ticketId}", h.GetTicket)
		t.With(access.RequireAnyPermission(permTicketsChange, permTicketsChangeOwn)).Put("/{ticketId}", h.UpdateTicket)
		t.With(access.RequireAnyPermission(permTicketsChange, permTicketsChangeOwn)).Put("/{ticketId}/status", h.UpdateTicketStatus)
		t.With(access.RequirePermission(permTicketsChange)).Put("/{ticketId}/assign", h.AssignTicket)
		t.With(access.RequirePermission(permTicketsDelete)).Delete("/{ticketId}", h.DeleteTicket)

		t.With(access.RequirePermission(permCommentsAdd)).Post("/{ticketId}/comments", h.AddComment)
		t.With(viewAny).Get("/{ticketId}/comments", h.ListComments)
		t.With(access.RequireAnyPermission(permCommentsChange, permCommentsChgOwn)).Put("/{ticketId}/comments/{commentId}", h.UpdateComment)
		t.With(access.RequireAnyPermission(permCommentsDelete, permCommentsDelOwn)).Delete("/{ticketId}/comments/{commentId}", h.DeleteComment)

		t.With(access.RequireAnyPermission(permAttachmentsView, permAttachViewOwn)).Get("/{ticketId}/attachments", h.ListAttachments)
	})

	r.Route("/categories", func(c chi.Router) {
		c.With(access.RequirePermission(permCategoriesAdd)).Post("/", h.CreateCategory)
		c.With(access.RequirePermission(permCategoriesView)).Get("/", h.ListCategories)
		c.With(access.RequirePermission(permCategoriesView)).Get("/{categoryId}", h.GetCategory)
		c.With(access.RequirePermission(permCategoriesChange)).Put("/{categoryId}", h.UpdateCategory)
		c.With(access.RequirePermission(permCategoriesDelete)).Delete("/{categoryId}", h.DeleteCategory)
	})
}

// caller returns the request's resolved access; the protected group always sets it.
func caller(w http.ResponseWriter, r *http.Request) (*access.Access, bool) {
	a := access.From(r.Context())
	if a == nil || a.TenantID == uuid.Nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return a, true
}

// ownOnly reports whether the caller sees only their own tickets (a customer): they lack the
// tenant-wide view.
func ownOnly(a *access.Access) bool { return !a.Has(permTicketsView) }

// visibleTicket loads a ticket the caller may see. A ticket outside an own-only caller's reach is
// not found, so its existence is not revealed.
func (h *Handler) visibleTicket(w http.ResponseWriter, r *http.Request, a *access.Access, ticketID uuid.UUID) (*Ticket, bool) {
	t, err := h.service.GetTicket(r.Context(), a.TenantID, ticketID)
	if err == nil && ownOnly(a) && t.CustomerID != a.AuthUserID {
		err = ErrTicketNotFound
	}
	if err != nil {
		h.handleError(w, err)
		return nil, false
	}
	return t, true
}

func ticketIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ticket ID")
		return uuid.Nil, false
	}
	return id, true
}

// CreateTicket creates a new support ticket.
// @Summary      Create ticket
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreateTicketRequest true "Ticket payload"
// @Success      201 {object} Ticket
// @Security     BearerAuth
// @Router       /{tenant}/tickets [post]
func (h *Handler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	var req CreateTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = a.TenantID
	if ownOnly(a) {
		// A customer raises tickets for themselves only and cannot route them.
		req.CustomerID = a.AuthUserID
		req.AssignedAgentID, req.AssignedTeamID = nil, nil
	} else if req.CustomerID == uuid.Nil {
		req.CustomerID = a.AuthUserID
	}
	defaultRequesterToCaller(&req, a.AuthUserID.String(), a.Email)
	ticket, err := h.service.CreateTicket(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, ticket)
}

// defaultRequesterToCaller makes a signed-in user the requester of a ticket they raise for
// themselves (no customer_id, or their own) when the body names no requester email. An agent
// raising a ticket for someone else names the requester (requester_email/requester_phone).
func defaultRequesterToCaller(req *CreateTicketRequest, subject, email string) {
	if strings.TrimSpace(req.RequesterEmail) != "" || metaText(req.Metadata, MetaRequesterEmail, metaLegacyCustomerEmail) != "" {
		return
	}
	self := req.CustomerID == uuid.Nil
	if !self {
		if uid, err := uuid.Parse(subject); err == nil && uid == req.CustomerID {
			self = true
		}
	}
	if self {
		req.RequesterEmail = email
	}
}

// GetTicket retrieves a ticket by ID.
// @Summary      Get ticket
// @Tags         tickets
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Success      200 {object} Ticket
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId} [get]
func (h *Handler) GetTicket(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	if t, ok := h.visibleTicket(w, r, a, ticketID); ok {
		respondJSON(w, http.StatusOK, t)
	}
}

// GetTicketByNumber retrieves a ticket by its number.
func (h *Handler) GetTicketByNumber(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	t, err := h.service.GetTicketByNumber(r.Context(), a.TenantID, chi.URLParam(r, "ticketNumber"))
	if err == nil && ownOnly(a) && t.CustomerID != a.AuthUserID {
		err = ErrTicketNotFound
	}
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// ListTickets lists tickets with filtering and pagination. An own-only caller sees their tickets.
// @Summary      List tickets
// @Tags         tickets
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/tickets [get]
func (h *Handler) ListTickets(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	filter := TicketFilter{TenantID: a.TenantID, Limit: p.Limit, Offset: p.Offset}
	q := r.URL.Query()
	if v := q.Get("status"); v != "" {
		s := TicketStatus(v)
		filter.Status = &s
	}
	if v := q.Get("priority"); v != "" {
		pr := TicketPriority(v)
		filter.Priority = &pr
	}
	parseID := func(key string) *uuid.UUID {
		if id, err := uuid.Parse(q.Get(key)); err == nil {
			return &id
		}
		return nil
	}
	filter.CustomerID = parseID("customerId")
	filter.AssignedAgentID = parseID("agentId")
	filter.AssignedTeamID = parseID("teamId")
	filter.CategoryID = parseID("categoryId")
	filter.OrderID = parseID("orderId")
	if v := q.Get("channel"); v != "" {
		ch := TicketChannel(v)
		filter.Channel = &ch
	}
	if ownOnly(a) {
		self := a.AuthUserID
		filter.CustomerID = &self
	}
	rows, total, err := h.service.ListTickets(r.Context(), a.TenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(rows, total, p))
}

// UpdateTicket updates a ticket. An own-only caller may change only the subject and category.
func (h *Handler) UpdateTicket(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	var req UpdateTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !a.Has(permTicketsChange) {
		if _, ok := h.visibleTicket(w, r, a, ticketID); !ok {
			return
		}
		req = UpdateTicketRequest{Subject: req.Subject, CategoryID: req.CategoryID}
	}
	if err := h.service.UpdateTicket(r.Context(), a.TenantID, ticketID, &req); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// UpdateTicketStatus updates a ticket's status. An own-only caller may only close or reopen.
// @Summary      Update ticket status
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Success      200 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/status [put]
func (h *Handler) UpdateTicketStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Status TicketStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !a.Has(permTicketsChange) {
		if req.Status != StatusClosed && req.Status != StatusOpen {
			respondError(w, http.StatusForbidden, "you can only close or reopen your own ticket")
			return
		}
		if _, ok := h.visibleTicket(w, r, a, ticketID); !ok {
			return
		}
	}
	if err := h.service.UpdateTicketStatus(r.Context(), a.TenantID, ticketID, req.Status); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// AssignTicket assigns a ticket to an agent or team.
// @Summary      Assign ticket
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Param        body     body AssignTicketRequest true "Assignment payload"
// @Success      200 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/assign [put]
func (h *Handler) AssignTicket(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	var req AssignTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.AssignTicket(r.Context(), a.TenantID, ticketID, &req); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
}

// DeleteTicket soft-deletes a ticket.
func (h *Handler) DeleteTicket(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteTicket(r.Context(), a.TenantID, ticketID); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetTicketStats returns ticket counts by status.
func (h *Handler) GetTicketStats(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	stats, err := h.service.GetTicketStats(r.Context(), a.TenantID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"stats": stats})
}

// GetAnalytics returns ticket counts by status and priority plus the creation and resolution trend.
// @Summary      Get analytics
// @Tags         tickets
// @Produce      json
// @Param        tenant path  string true "Tenant slug"
// @Param        days   query int    false "Days in the trend (default 30, max 365)"
// @Success      200 {object} tickets.AnalyticsResponse
// @Security     BearerAuth
// @Router       /{tenant}/tickets/analytics [get]
func (h *Handler) GetAnalytics(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	days := 30
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n > 0 {
		days = n
	}
	analytics, err := h.service.GetAnalytics(r.Context(), a.TenantID, days)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, analytics)
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTicketNotFound), errors.Is(err, ErrCommentNotFound),
		errors.Is(err, ErrAttachmentNotFound), errors.Is(err, ErrCategoryNotFound):
		respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalidTransition):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrCategoryExists), errors.Is(err, ErrTicketAlreadyClosed), errors.Is(err, ErrTicketCancelled),
		errors.Is(err, ErrCategoryHasChildren), errors.Is(err, ErrCategoryHasTickets), errors.Is(err, ErrDuplicateSourceRef):
		respondError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrAgentNotAvailable):
		respondError(w, http.StatusServiceUnavailable, err.Error())
	default:
		h.logger.Error("internal error", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "internal error")
	}
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
