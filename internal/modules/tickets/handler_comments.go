package tickets

import (
	"encoding/json"
	"net/http"
	"strings"

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
	t, ok := h.visibleTicket(w, r, a, ticketID)
	if !ok {
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
	respondJSON(w, http.StatusCreated, h.service.WithAuthorName(r.Context(), t, comment))
}

// ListComments lists a ticket's comments a keyset page at a time, oldest first, or newest first
// with order=desc; an own-only caller never sees internal notes.
// @Summary      List comments
// @Tags         tickets
// @Produce      json
// @Param        tenant   path  string true  "Tenant slug"
// @Param        ticketId path  string true  "Ticket UUID"
// @Param        cursor   query string false "next_cursor of the previous page"
// @Param        limit    query int    false "Page size (default 25, max 100)"
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
	t, ok := h.visibleTicket(w, r, a, ticketID)
	if !ok {
		return
	}
	page, err := ParsePageParams(r)
	if err != nil {
		h.handleError(w, err)
		return
	}
	newest := r.URL.Query().Get("order") == "desc"
	res, err := h.service.PageTicketComments(r.Context(), t, CommentQuery{Page: page, PublicOnly: ownOnly(a), Newest: newest})
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, res)
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
