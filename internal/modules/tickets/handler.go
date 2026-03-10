package tickets

import (
	"encoding/json"
	"net/http"
	"strconv"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Handler handles HTTP requests for ticket operations.
type Handler struct {
	service *Service
	logger  *zap.Logger
}

// NewHandler creates a new ticket handler.
func NewHandler(service *Service, logger *zap.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger.Named("tickets-handler"),
	}
}

// RegisterRoutes registers the ticket routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	// Tickets
	r.Route("/tickets", func(tickets chi.Router) {
		tickets.Post("/", h.CreateTicket)
		tickets.Get("/", h.ListTickets)
		tickets.Get("/stats", h.GetTicketStats)
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

// CreateTicket creates a new ticket.
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

// ListTickets lists tickets with filtering.
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

	filter := TicketFilter{
		TenantID: *tenantID,
		Limit:    50,
	}

	// Parse query parameters
	if v := r.URL.Query().Get("status"); v != "" {
		status := TicketStatus(v)
		filter.Status = &status
	}
	if v := r.URL.Query().Get("priority"); v != "" {
		priority := TicketPriority(v)
		filter.Priority = &priority
	}
	if v := r.URL.Query().Get("customerId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.CustomerID = &id
		}
	}
	if v := r.URL.Query().Get("agentId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.AssignedAgentID = &id
		}
	}
	if v := r.URL.Query().Get("teamId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.AssignedTeamID = &id
		}
	}
	if v := r.URL.Query().Get("categoryId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.CategoryID = &id
		}
	}
	if v := r.URL.Query().Get("orderId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.OrderID = &id
		}
	}
	if v := r.URL.Query().Get("channel"); v != "" {
		channel := TicketChannel(v)
		filter.Channel = &channel
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if offset, err := strconv.Atoi(v); err == nil {
			filter.Offset = offset
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err := strconv.Atoi(v); err == nil && limit > 0 && limit <= 100 {
			filter.Limit = limit
		}
	}

	tickets, total, err := h.service.ListTickets(r.Context(), *tenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"tickets": tickets,
		"total":   total,
	})
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

// AssignTicket assigns a ticket to an agent/team.
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

// Comment handlers

// AddComment adds a comment to a ticket.
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

// CreateCategory creates a new category.
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

// ListCategories lists categories with filtering.
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

	filter := CategoryFilter{
		TenantID: *tenantID,
		Limit:    100,
	}

	if v := r.URL.Query().Get("parentId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.ParentID = &id
		}
	}
	if v := r.URL.Query().Get("active"); v != "" {
		isActive := v == "true"
		filter.IsActive = &isActive
	}

	categories, total, err := h.service.ListCategories(r.Context(), *tenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"categories": categories,
		"total":      total,
	})
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

// Helper functions

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
