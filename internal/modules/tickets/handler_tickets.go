package tickets

import (
	"encoding/json"
	"net/http"
	"strings"

	pagination "github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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
		respondJSON(w, http.StatusOK, h.service.WithSLALabels(r.Context(), a.TenantID, t))
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
	respondJSON(w, http.StatusOK, h.service.WithSLALabels(r.Context(), a.TenantID, t))
}

// ListTickets lists tickets a keyset page at a time, newest first unless sort=created_asc, or
// nearest SLA target first with sort=sla_due (running clocks before paused ones, tickets with no
// target last in each group). A cursor only continues the order that issued it. An own-only
// caller sees their tickets.
// @Summary      List tickets
// @Tags         tickets
// @Produce      json
// @Param        tenant     path  string true  "Tenant slug"
// @Param        cursor     query string false "next_cursor of the previous page"
// @Param        limit      query int    false "Page size (default 25, max 100)"
// @Param        search     query string false "Ticket number, subject, description or requester"
// @Param        tags       query string false "Comma-separated tags; a ticket must carry all"
// @Param        channel    query string false "web, email, api, chat, whatsapp, phone or internal"
// @Param        sort       query string false "created_desc (default), created_asc or sla_due"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/tickets [get]
func (h *Handler) ListTickets(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	page, err := ParsePageParams(r)
	if err != nil {
		h.handleError(w, err)
		return
	}
	q := r.URL.Query()
	filter := TicketFilter{
		TenantID: a.TenantID, Page: page, Sort: q.Get("sort"),
		Search: pagination.SearchTerm(r), Tags: ParseTagFilter(q.Get("tags")),
	}
	// status takes one status or a comma list (open,in_progress), so a queue tab is one query.
	if v := q.Get("status"); v != "" {
		for _, part := range strings.Split(v, ",") {
			if s := TicketStatus(strings.TrimSpace(part)); s != "" {
				filter.Statuses = append(filter.Statuses, s)
			}
		}
		if len(filter.Statuses) == 1 {
			filter.Status, filter.Statuses = &filter.Statuses[0], nil
		}
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
		ch := TicketChannel(strings.ToLower(strings.TrimSpace(v)))
		if !ch.Valid() {
			respondError(w, http.StatusBadRequest, "channel must be one of web, email, api, chat, whatsapp, phone, internal")
			return
		}
		filter.Channel = &ch
	}
	if ownOnly(a) {
		self := a.AuthUserID
		filter.CustomerID = &self
	}
	res, err := h.service.ListTickets(r.Context(), a.TenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// maxTagList bounds GET /tickets/tags.
const maxTagList = 100

// ListTags lists the tenant's tags in use with their counts, most used first.
// @Summary      List ticket tags
// @Tags         tickets
// @Produce      json
// @Param        tenant path  string true  "Tenant slug"
// @Param        search query string false "Tag prefix"
// @Param        limit  query int    false "At most this many tags (default 50, max 100)"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/tickets/tags [get]
func (h *Handler) ListTags(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 50, maxTagList)
	tags, err := h.service.ListTags(r.Context(), a.TenantID, pagination.SearchTerm(r), limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": tags})
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
