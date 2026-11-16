package tickets

import (
	"net/http"

	"github.com/google/uuid"
)

// Agent actions on tickets: merge, links, bulk update, macros and collision presence. Every
// route here needs the tenant-wide ticket permissions, which customers never hold.

// MergeTicket merges the URL ticket into into_ticket_id.
// @Summary      Merge ticket
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant   path string       true "Tenant slug"
// @Param        ticketId path string       true "Ticket to merge (it closes)"
// @Param        body     body MergeRequest true "Target ticket"
// @Success      200 {object} Ticket
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/merge [post]
func (h *Handler) MergeTicket(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	var req MergeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.IntoTicketID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "into_ticket_id is required")
		return
	}
	target, err := h.service.MergeTicket(r.Context(), a.TenantID, ticketID, req.IntoTicketID, a.AuthUserID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, target)
}

// ListLinks lists the ticket's parent, child, related and duplicate links.
// @Summary      List ticket links
// @Tags         tickets
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/links [get]
func (h *Handler) ListLinks(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	links, err := h.service.ListLinks(r.Context(), a.TenantID, ticketID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": links})
}

// CreateLink links the URL ticket to another (kind parent, child, related or duplicate).
// @Summary      Link tickets
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant   path string            true "Tenant slug"
// @Param        ticketId path string            true "Ticket UUID"
// @Param        body     body CreateLinkRequest true "Link"
// @Success      201 {object} TicketLink
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/links [post]
func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	var req CreateLinkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	link, err := h.service.LinkTickets(r.Context(), a.TenantID, ticketID, a.AuthUserID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, link)
}

// DeleteLink removes one of the ticket's links.
func (h *Handler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	linkID, ok := uuidParam(w, r, "linkId", "link")
	if !ok {
		return
	}
	if err := h.service.UnlinkTickets(r.Context(), a.TenantID, ticketID, linkID); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// BulkUpdate changes up to 100 tickets in one transaction and reports each ticket's outcome.
// @Summary      Bulk update tickets
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant path string            true "Tenant slug"
// @Param        body   body BulkUpdateRequest true "Tickets and changes"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/tickets/bulk [put]
func (h *Handler) BulkUpdate(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	var req BulkUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	results, err := h.service.BulkUpdate(r.Context(), a.TenantID, &req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	updated := 0
	for _, res := range results {
		if res.OK {
			updated++
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"results": results, "updated": updated, "failed": len(results) - updated})
}

// ApplyMacro applies a macro to the ticket: its field changes and its reply, in one transaction.
// @Summary      Apply macro
// @Tags         macros
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Param        macroId  path string true "Macro UUID"
// @Success      200 {object} MacroApplied
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/macros/{macroId}/apply [post]
func (h *Handler) ApplyMacro(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	macroID, ok := uuidParam(w, r, "macroId", "macro")
	if !ok {
		return
	}
	res, err := h.service.ApplyMacro(r.Context(), a.TenantID, ticketID, macroID, a.AuthUserID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// presenceRequest is the body of POST /tickets/{id}/presence.
type presenceRequest struct {
	State PresenceState `json:"state"`
}

// SetPresence records that the caller is viewing or typing on the ticket for about 30 seconds.
// @Summary      Set presence
// @Tags         tickets
// @Accept       json
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/presence [post]
func (h *Handler) SetPresence(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	var req presenceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, ok := h.visibleTicket(w, r, a, ticketID); !ok {
		return
	}
	entry := PresenceEntry{UserID: a.AuthUserID, AgentID: a.AgentID, Name: firstNonBlank(a.Name, a.Email), State: req.State}
	if err := h.presence.Set(r.Context(), a.TenantID, ticketID, entry); err != nil {
		h.handleError(w, err)
		return
	}
	h.writePresence(w, r, a.TenantID, ticketID)
}

// ListPresence returns who is viewing or typing on the ticket.
// @Summary      Get presence
// @Tags         tickets
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        ticketId path string true "Ticket UUID"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/presence [get]
func (h *Handler) ListPresence(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	h.writePresence(w, r, a.TenantID, ticketID)
}

// LeavePresence forgets the caller on the ticket (they closed it).
func (h *Handler) LeavePresence(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return
	}
	entry := PresenceEntry{UserID: a.AuthUserID, AgentID: a.AgentID, Name: firstNonBlank(a.Name, a.Email)}
	if err := h.presence.Leave(r.Context(), a.TenantID, ticketID, entry); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "left"})
}

func (h *Handler) writePresence(w http.ResponseWriter, r *http.Request, tenantID, ticketID uuid.UUID) {
	entries, err := h.presence.List(r.Context(), tenantID, ticketID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ticket_id": ticketID, "data": entries})
}
