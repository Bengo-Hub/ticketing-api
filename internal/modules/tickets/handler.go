package tickets

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	httpware "github.com/Bengo-Hub/httpware"
	pagination "github.com/Bengo-Hub/pagination"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Handler handles HTTP requests for ticket operations.
type Handler struct {
	service    *Service
	logger     *zap.Logger
	serviceKey string // INTERNAL_SERVICE_KEY for S2S calls
}

// NewHandler creates a new ticket handler.
func NewHandler(service *Service, logger *zap.Logger, opts ...func(*Handler)) *Handler {
	h := &Handler{
		service: service,
		logger:  logger.Named("tickets-handler"),
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// WithServiceKey sets the internal service key for S2S endpoint validation.
func WithServiceKey(key string) func(*Handler) {
	return func(h *Handler) { h.serviceKey = key }
}

// RegisterRoutes registers the ticket routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	// Tickets
	r.Route("/tickets", func(tickets chi.Router) {
		tickets.Post("/", h.CreateTicket)
		tickets.Get("/", h.ListTickets)
		tickets.Get("/stats", h.GetTicketStats)
		tickets.Get("/analytics", h.GetAnalytics)
		tickets.Get("/{ticketId}", h.GetTicket)
		tickets.Get("/number/{ticketNumber}", h.GetTicketByNumber)
		tickets.Put("/{ticketId}", h.UpdateTicket)
		tickets.Put("/{ticketId}/status", h.UpdateTicketStatus)
		tickets.Put("/{ticketId}/assign", h.AssignTicket)
		tickets.Delete("/{ticketId}", h.DeleteTicket)

		// Comments
		tickets.Post("/{ticketId}/comments", h.AddComment)
		tickets.Get("/{ticketId}/comments", h.ListComments)
		tickets.Put("/{ticketId}/comments/{commentId}", h.UpdateComment)
		tickets.Delete("/{ticketId}/comments/{commentId}", h.DeleteComment)

		// Attachments
		tickets.Get("/{ticketId}/attachments", h.ListAttachments)
	})

	// AI routing helpers
	r.Post("/tickets/classify", h.ClassifyTicket)

	// Categories
	r.Route("/categories", func(categories chi.Router) {
		categories.Post("/", h.CreateCategory)
		categories.Get("/", h.ListCategories)
		categories.Get("/{categoryId}", h.GetCategory)
		categories.Put("/{categoryId}", h.UpdateCategory)
		categories.Delete("/{categoryId}", h.DeleteCategory)
	})
}

// Ticket handlers

// CreateTicket creates a new support ticket.
// @Summary      Create ticket
// @Description  Create a new support ticket for the authenticated tenant
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreateTicketRequest true "Ticket payload"
// @Success      201 {object} Ticket
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets [post]
func (h *Handler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	var req CreateTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = *tenantID

	ticket, err := h.service.CreateTicket(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, ticket)
}

// GetTicket retrieves a ticket by ID.
// @Summary      Get ticket
// @Description  Retrieve a support ticket by its UUID
// @Tags         tickets
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Success      200 {object} Ticket
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId} [get]
func (h *Handler) GetTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ticket ID")
		return
	}

	ticket, err := h.service.GetTicket(r.Context(), *tenantID, ticketID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, ticket)
}

// GetTicketByNumber retrieves a ticket by its number.
func (h *Handler) GetTicketByNumber(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	ticketNumber := chi.URLParam(r, "ticketNumber")
	if ticketNumber == "" {
		respondError(w, http.StatusBadRequest, "ticket number required")
		return
	}

	ticket, err := h.service.GetTicketByNumber(r.Context(), *tenantID, ticketNumber)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, ticket)
}

// ListTickets lists tickets with filtering and pagination.
// @Summary      List tickets
// @Description  List support tickets for the authenticated tenant with optional filters
// @Tags         tickets
// @Produce      json
// @Param        tenant     path   string false "Tenant slug"
// @Param        status     query  string false "Filter by status"
// @Param        priority   query  string false "Filter by priority"
// @Param        customerId query  string false "Filter by customer UUID"
// @Param        agentId    query  string false "Filter by assigned agent UUID"
// @Param        teamId     query  string false "Filter by assigned team UUID"
// @Param        categoryId query  string false "Filter by category UUID"
// @Param        orderId    query  string false "Filter by order UUID"
// @Param        channel    query  string false "Filter by channel"
// @Param        page       query  int    false "Page number"
// @Param        limit      query  int    false "Results per page"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets [get]
func (h *Handler) ListTickets(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	p := pagination.Parse(r)
	filter := TicketFilter{
		TenantID: *tenantID,
		Limit:    p.Limit,
		Offset:   p.Offset,
	}

	q := r.URL.Query()
	if v := q.Get("status"); v != "" {
		s := TicketStatus(v)
		filter.Status = &s
	}
	if v := q.Get("priority"); v != "" {
		pr := TicketPriority(v)
		filter.Priority = &pr
	}
	if v := q.Get("customerId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.CustomerID = &id
		}
	}
	if v := q.Get("agentId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.AssignedAgentID = &id
		}
	}
	if v := q.Get("teamId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.AssignedTeamID = &id
		}
	}
	if v := q.Get("categoryId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.CategoryID = &id
		}
	}
	if v := q.Get("orderId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.OrderID = &id
		}
	}
	if v := q.Get("channel"); v != "" {
		ch := TicketChannel(v)
		filter.Channel = &ch
	}

	tickets, total, err := h.service.ListTickets(r.Context(), *tenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, pagination.NewResponse(tickets, total, p))
}

// UpdateTicket updates a ticket.
func (h *Handler) UpdateTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ticket ID")
		return
	}

	var req UpdateTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.UpdateTicket(r.Context(), *tenantID, ticketID, &req); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// UpdateTicketStatus updates a ticket's status.
// @Summary      Update ticket status
// @Description  Update the status of a support ticket
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Param        body     body object true "Status payload" SchemaExample({"status":"open"})
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/status [put]
func (h *Handler) UpdateTicketStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ticket ID")
		return
	}

	var req struct {
		Status TicketStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.UpdateTicketStatus(r.Context(), *tenantID, ticketID, req.Status); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// AssignTicket assigns a ticket to an agent or team.
// @Summary      Assign ticket
// @Description  Assign a support ticket to an agent or team
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Param        body     body AssignTicketRequest true "Assignment payload"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/assign [put]
func (h *Handler) AssignTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ticket ID")
		return
	}

	var req AssignTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.AssignTicket(r.Context(), *tenantID, ticketID, &req); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
}

// DeleteTicket soft-deletes a ticket.
func (h *Handler) DeleteTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ticket ID")
		return
	}

	if err := h.service.DeleteTicket(r.Context(), *tenantID, ticketID); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetTicketStats returns ticket statistics.
func (h *Handler) GetTicketStats(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	stats, err := h.service.GetTicketStats(r.Context(), *tenantID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"stats": stats})
}

// GetAnalytics returns combined ticket analytics: counts by status/priority and daily trend.
// @Summary      Get analytics
// @Description  Returns ticket counts by status and priority, plus a creation/resolution trend
// @Tags         tickets
// @Produce      json
// @Param        tenant path  string true "Tenant slug"
// @Param        days   query int    false "Number of days to include in trend (default 30, max 365)"
// @Success      200 {object} tickets.AnalyticsResponse
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets/analytics [get]
func (h *Handler) GetAnalytics(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}

	analytics, err := h.service.GetAnalytics(r.Context(), *tenantID, days)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, analytics)
}

// Comment handlers

// AddComment adds a comment to a ticket.
// @Summary      Add comment
// @Description  Add a comment to a support ticket
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Param        body     body AddCommentRequest true "Comment payload"
// @Success      201 {object} TicketComment
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/comments [post]
func (h *Handler) AddComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ticket ID")
		return
	}

	var req AddCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = *tenantID
	req.TicketID = ticketID

	comment, err := h.service.AddComment(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, comment)
}

// ListComments lists comments for a ticket.
// @Summary      List comments
// @Description  List all comments for a support ticket
// @Tags         tickets
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/comments [get]
func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ticket ID")
		return
	}

	comments, err := h.service.ListComments(r.Context(), *tenantID, ticketID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"comments": comments,
		"total":    len(comments),
	})
}

// UpdateComment updates a comment.
func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	commentID, err := uuid.Parse(chi.URLParam(r, "commentId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid comment ID")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.UpdateComment(r.Context(), *tenantID, commentID, req.Content); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteComment deletes a comment.
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	commentID, err := uuid.Parse(chi.URLParam(r, "commentId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid comment ID")
		return
	}

	if err := h.service.DeleteComment(r.Context(), *tenantID, commentID); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ListAttachments lists attachments for a ticket.
func (h *Handler) ListAttachments(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	ticketID, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ticket ID")
		return
	}

	attachments, err := h.service.ListAttachments(r.Context(), *tenantID, ticketID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"attachments": attachments,
		"total":       len(attachments),
	})
}

// Category handlers

// CreateCategory creates a new ticket category.
// @Summary      Create category
// @Description  Create a new ticket category for the authenticated tenant
// @Tags         categories
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreateCategoryRequest true "Category payload"
// @Success      201 {object} TicketCategory
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/categories [post]
func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = *tenantID

	category, err := h.service.CreateCategory(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, category)
}

// GetCategory retrieves a category by ID.
func (h *Handler) GetCategory(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	categoryID, err := uuid.Parse(chi.URLParam(r, "categoryId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid category ID")
		return
	}

	category, err := h.service.GetCategory(r.Context(), *tenantID, categoryID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, category)
}

// ListCategories lists ticket categories with filtering and pagination.
// @Summary      List categories
// @Description  List ticket categories for the authenticated tenant
// @Tags         categories
// @Produce      json
// @Param        tenant   path  string false "Tenant slug"
// @Param        parentId query string false "Filter by parent category UUID"
// @Param        active   query bool   false "Filter by active status"
// @Param        page     query int    false "Page number"
// @Param        limit    query int    false "Results per page"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/categories [get]
func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	p := pagination.Parse(r)
	filter := CategoryFilter{
		TenantID: *tenantID,
		Limit:    p.Limit,
		Offset:   p.Offset,
	}

	q := r.URL.Query()
	if v := q.Get("parentId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.ParentID = &id
		}
	}
	if v := q.Get("active"); v != "" {
		isActive := v == "true"
		filter.IsActive = &isActive
	}

	categories, total, err := h.service.ListCategories(r.Context(), *tenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, pagination.NewResponse(categories, total, p))
}

// UpdateCategory updates a category.
func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	categoryID, err := uuid.Parse(chi.URLParam(r, "categoryId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid category ID")
		return
	}

	var req UpdateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.UpdateCategory(r.Context(), *tenantID, categoryID, &req); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteCategory deletes a category.
func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	categoryID, err := uuid.Parse(chi.URLParam(r, "categoryId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid category ID")
		return
	}

	if err := h.service.DeleteCategory(r.Context(), *tenantID, categoryID); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ClassifyTicket is an AI routing helper endpoint. It accepts a subject and
// description and returns a suggested priority and category hint based on
// keyword heuristics. A more sophisticated AI classifier can replace this
// logic later without changing the contract.
func (h *Handler) ClassifyTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, err := claims.TenantUUID()
	if err != nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return
	}

	var req struct {
		Subject     string `json:"subject"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Subject == "" && req.Description == "" {
		respondError(w, http.StatusBadRequest, "subject or description required")
		return
	}

	priority, categoryHint := classifyTicketText(req.Subject + " " + req.Description)

	respondJSON(w, http.StatusOK, map[string]any{
		"suggested_priority":      priority,
		"suggested_category_hint": categoryHint,
	})
}

// classifyTicketText applies simple keyword heuristics to suggest a priority
// and category hint for AI routing. Replace with an LLM call as needed.
func classifyTicketText(text string) (TicketPriority, string) {
	lower := strings.ToLower(text)

	priority := PriorityNormal
	switch {
	case containsAny(lower, "urgent", "critical", "down", "outage", "breach", "emergency"):
		priority = PriorityUrgent
	case containsAny(lower, "high", "asap", "immediately", "blocked"):
		priority = PriorityHigh
	case containsAny(lower, "low", "minor", "whenever", "no rush"):
		priority = PriorityLow
	}

	categoryHint := "general"
	switch {
	case containsAny(lower, "payment", "invoice", "billing", "charge", "refund"):
		categoryHint = "billing"
	case containsAny(lower, "login", "password", "access", "auth", "account"):
		categoryHint = "account"
	case containsAny(lower, "bug", "error", "crash", "broken", "not working"):
		categoryHint = "technical"
	case containsAny(lower, "feature", "request", "enhancement", "suggestion"):
		categoryHint = "feature_request"
	case containsAny(lower, "delivery", "shipping", "order", "tracking"):
		categoryHint = "order"
	}

	return priority, categoryHint
}

func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// Helper functions

// InternalCreateTicket is the S2S endpoint called by marketflow-ai agent and
// the Vera widget public tickets relay. Gated by X-API-Key (INTERNAL_SERVICE_KEY).
// Accepts a wider payload: title/description from the agent tool schema plus
// requester_name, requester_email stored in metadata.
func (h *Handler) InternalCreateTicket(w http.ResponseWriter, r *http.Request) {
	if !httpware.ValidServiceKey(r, h.serviceKey) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		TenantID       string `json:"tenant_id"`
		Title          string `json:"title"`
		Description    string `json:"description"`
		Priority       string `json:"priority"`
		Category       string `json:"category"`
		Source         string `json:"source"`
		Channel        string `json:"channel"`
		RequesterName  string `json:"requester_name"`
		RequesterEmail string `json:"requester_email"`
		RequesterPhone string `json:"requester_phone"`
		// ContactHash is the caller's keyed hash of the contact it verified. The caller proves a later
		// reader against it; ticketing never needs to read the contact itself.
		ContactHash string `json:"contact_hash"`
		SessionID   string `json:"session_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" || req.Description == "" {
		respondError(w, http.StatusBadRequest, "title and description are required")
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant_id")
		return
	}

	prio := TicketPriority(req.Priority)
	switch prio {
	case "":
		prio = PriorityNormal
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent, PriorityCritical:
	default:
		respondError(w, http.StatusBadRequest, "invalid priority")
		return
	}
	channel := TicketChannel(req.Channel)
	switch channel {
	case "":
		channel = ChannelWeb
	case ChannelWeb, ChannelEmail, ChannelAPI, ChannelChat, ChannelWhatsApp, ChannelPhone, ChannelInternal:
	default:
		respondError(w, http.StatusBadRequest, "invalid channel")
		return
	}

	meta := map[string]any{
		"source":          req.Source,
		"category":        req.Category,
		"requester_name":  req.RequesterName,
		"requester_email": req.RequesterEmail,
		"requester_phone": req.RequesterPhone,
		"session_id":      req.SessionID,
	}
	if req.ContactHash != "" {
		meta["contact_hash"] = req.ContactHash
	}

	domainReq := &CreateTicketRequest{
		TenantID:    tenantID,
		Subject:     req.Title,
		Description: req.Description,
		Priority:    prio,
		Channel:     channel,
		Metadata:    meta,
	}
	if req.ContactHash != "" {
		// One contact's tickets group under one synthetic customer, as the public routes do by email.
		domainReq.CustomerID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(req.ContactHash))
	}

	ticket, err := h.service.CreateTicket(r.Context(), domainReq)
	if err != nil {
		h.handleError(w, err)
		return
	}

	h.logger.Info("S2S ticket created",
		zap.String("tenant_id", req.TenantID),
		zap.String("ticket_number", ticket.TicketNumber),
		zap.String("source", req.Source),
	)
	respondJSON(w, http.StatusCreated, map[string]any{
		"id":            ticket.ID.String(),
		"ticket_number": ticket.TicketNumber,
		"status":        ticket.Status,
	})
}

// InternalGetTicket is the S2S read of one ticket by number for a service that verified the contact
// itself, gated by X-API-Key: the ticket, who may read it (contact_hash) and its public comments only.
// Internal notes and the requester's contact details never leave here.
func (h *Handler) InternalGetTicket(w http.ResponseWriter, r *http.Request) {
	if !httpware.ValidServiceKey(r, h.serviceKey) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tenantID, err := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant_id")
		return
	}
	ticket, err := h.service.GetTicketByNumber(r.Context(), tenantID, chi.URLParam(r, "ticketNumber"))
	if err != nil {
		h.handleError(w, err)
		return
	}
	comments, err := h.service.ListComments(r.Context(), tenantID, ticket.ID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, internalTicketView(ticket, comments))
}

// internalTicketView keeps only what a calling service may show its own user.
func internalTicketView(t *Ticket, comments []*TicketComment) map[string]any {
	public := make([]map[string]any, 0, len(comments))
	for _, c := range comments {
		if c.CommentType != CommentPublic {
			continue
		}
		public = append(public, map[string]any{"author_type": c.AuthorType, "content": c.Content, "created_at": c.CreatedAt})
	}
	return map[string]any{
		"id": t.ID.String(), "ticket_number": t.TicketNumber, "status": t.Status, "priority": t.Priority, "channel": t.Channel,
		"subject": t.Subject, "description": t.Description, "category": t.Metadata["category"], "contact_hash": t.Metadata["contact_hash"],
		"created_at": t.CreatedAt, "updated_at": t.UpdatedAt, "comments": public,
	}
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	switch err {
	case ErrTicketNotFound, ErrCommentNotFound, ErrAttachmentNotFound, ErrCategoryNotFound:
		respondError(w, http.StatusNotFound, err.Error())
	case ErrCategoryExists:
		respondError(w, http.StatusConflict, err.Error())
	case ErrInvalidTransition:
		respondError(w, http.StatusBadRequest, err.Error())
	case ErrTicketAlreadyClosed, ErrTicketCancelled:
		respondError(w, http.StatusConflict, err.Error())
	case ErrCategoryHasChildren, ErrCategoryHasTickets:
		respondError(w, http.StatusConflict, err.Error())
	case ErrAgentNotAvailable:
		respondError(w, http.StatusServiceUnavailable, err.Error())
	default:
		h.logger.Error("internal error", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "internal error")
	}
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
