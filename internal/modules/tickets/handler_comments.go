package tickets

import (
	"encoding/json"
	"net/http"
	"strings"

	pagination "github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// AddComment adds a comment to a ticket. The author is the signed-in caller. Staff reply as
// agents and may post internal notes; an own-only caller (a customer) posts public comments on
// their own tickets.
// @Summary      Add comment
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Param        body     body AddCommentRequest true "Comment payload"
// @Success      201 {object} TicketComment
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/comments [post]
func (h *Handler) AddComment(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	var req AddCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		respondError(w, http.StatusBadRequest, "content is required")
		return
	}
	if _, ok := h.visibleTicket(w, r, a, ticketID); !ok {
		return
	}
	req.TenantID, req.TicketID, req.AuthorID = a.TenantID, ticketID, a.AuthUserID
	if ownOnly(a) {
		req.AuthorType, req.CommentType = AuthorCustomer, CommentPublic
	} else {
		req.AuthorType = AuthorAgent
		if req.CommentType == CommentSystem {
			respondError(w, http.StatusBadRequest, "comment_type must be public or internal")
			return
		}
	}
	comment, err := h.service.AddComment(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, comment)
}

// ListComments lists a ticket's comments; an own-only caller never sees internal notes.
// @Summary      List comments
// @Tags         tickets
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/comments [get]
func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.visibleTicket(w, r, a, ticketID); !ok {
		return
	}
	comments, err := h.service.ListComments(r.Context(), a.TenantID, ticketID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	if ownOnly(a) {
		public := comments[:0]
		for _, c := range comments {
			if c.CommentType != CommentInternal {
				public = append(public, c)
			}
		}
		comments = public
	}
	respondJSON(w, http.StatusOK, map[string]any{"comments": comments, "total": len(comments)})
}

// ownComment loads a comment on the URL's ticket that the caller may change or delete: any
// comment with the full permission, their own comment with the _own one.
func (h *Handler) ownComment(w http.ResponseWriter, r *http.Request, a *access.Access, fullPerm string) (uuid.UUID, bool) {
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return uuid.Nil, false
	}
	commentID, err := uuid.Parse(chi.URLParam(r, "commentId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid comment ID")
		return uuid.Nil, false
	}
	c, err := h.service.GetComment(r.Context(), a.TenantID, commentID)
	if err == nil && (c.TicketID != ticketID || (!a.Has(fullPerm) && c.AuthorID != a.AuthUserID)) {
		err = ErrCommentNotFound
	}
	if err != nil {
		h.handleError(w, err)
		return uuid.Nil, false
	}
	return commentID, true
}

// UpdateComment updates a comment.
func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	commentID, ok := h.ownComment(w, r, a, permCommentsChange)
	if !ok {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.UpdateComment(r.Context(), a.TenantID, commentID, req.Content); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteComment deletes a comment.
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	commentID, ok := h.ownComment(w, r, a, permCommentsDelete)
	if !ok {
		return
	}
	if err := h.service.DeleteComment(r.Context(), a.TenantID, commentID); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ListAttachments lists attachments for a ticket the caller may see.
func (h *Handler) ListAttachments(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.visibleTicket(w, r, a, ticketID); !ok {
		return
	}
	attachments, err := h.service.ListAttachments(r.Context(), a.TenantID, ticketID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"attachments": attachments, "total": len(attachments)})
}

// CreateCategory creates a new ticket category.
// @Summary      Create category
// @Tags         categories
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreateCategoryRequest true "Category payload"
// @Success      201 {object} TicketCategory
// @Security     BearerAuth
// @Router       /{tenant}/categories [post]
func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = a.TenantID
	category, err := h.service.CreateCategory(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, category)
}

func categoryIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "categoryId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid category ID")
		return uuid.Nil, false
	}
	return id, true
}

// GetCategory retrieves a category by ID.
func (h *Handler) GetCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := categoryIDParam(w, r)
	if !ok {
		return
	}
	category, err := h.service.GetCategory(r.Context(), a.TenantID, id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, category)
}

// ListCategories lists ticket categories with filtering and pagination.
// @Summary      List categories
// @Tags         categories
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/categories [get]
func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	filter := CategoryFilter{TenantID: a.TenantID, Limit: p.Limit, Offset: p.Offset}
	q := r.URL.Query()
	if id, err := uuid.Parse(q.Get("parentId")); err == nil {
		filter.ParentID = &id
	}
	if v := q.Get("active"); v != "" {
		isActive := v == "true"
		filter.IsActive = &isActive
	}
	categories, total, err := h.service.ListCategories(r.Context(), a.TenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(categories, total, p))
}

// UpdateCategory updates a category.
func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := categoryIDParam(w, r)
	if !ok {
		return
	}
	var req UpdateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.UpdateCategory(r.Context(), a.TenantID, id, &req); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteCategory deactivates a category.
func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := categoryIDParam(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteCategory(r.Context(), a.TenantID, id); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ClassifyTicket suggests a priority and a category hint for a subject and description from
// keyword heuristics.
func (h *Handler) ClassifyTicket(w http.ResponseWriter, r *http.Request) {
	if _, ok := caller(w, r); !ok {
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

// classifyTicketText applies keyword heuristics to suggest a priority and a category hint.
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
