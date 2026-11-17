package identity

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/bengobox/ticketing-service/internal/platform/access"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// CreateAgent creates a new support agent.
// @Summary      Create agent
// @Description  Create a new support agent for the authenticated tenant
// @Tags         identity
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreateAgentRequest true "Agent payload"
// @Success      201 {object} Agent
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/agents [post]
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = *tenantID

	agent, err := h.service.CreateAgent(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, agent)
}

// GetAgent retrieves an agent by ID.
func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	agentID, err := uuid.Parse(chi.URLParam(r, "agentId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid agent ID")
		return
	}

	agent, err := h.service.GetAgent(r.Context(), *tenantID, agentID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, agent)
}

// GetAgentByUserID retrieves an agent by user ID.
func (h *Handler) GetAgentByUserID(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	agent, err := h.service.GetAgentByUserID(r.Context(), *tenantID, userID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, agent)
}

// ListAgents lists agents a keyset page at a time, newest first.
// @Summary      List agents
// @Description  List support agents for the authenticated tenant, newest first, as {data, next_cursor, has_more}
// @Tags         identity
// @Produce      json
// @Param        tenant path  string false "Tenant slug"
// @Param        teamId query string false "Filter by team UUID"
// @Param        status query string false "Filter by agent status"
// @Param        role   query string false "Filter by agent role"
// @Param        active query bool   false "Filter by active status"
// @Param        cursor query string false "next_cursor of the previous page"
// @Param        limit  query int    false "Page size (default 25, max 100)"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/agents [get]
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	after, limit, err := parsePage(r)
	if err != nil {
		h.handleError(w, err)
		return
	}
	filter := AgentFilter{TenantID: *tenantID, After: after, Limit: limit}

	q := r.URL.Query()
	if v := q.Get("teamId"); v != "" {
		if teamID, err := uuid.Parse(v); err == nil {
			filter.TeamID = &teamID
		}
	}
	if v := q.Get("status"); v != "" {
		s := AgentStatus(v)
		filter.Status = &s
	}
	if v := q.Get("role"); v != "" {
		ro := AgentRole(v)
		filter.Role = &ro
	}
	if v := q.Get("active"); v != "" {
		isActive := v == "true"
		filter.IsActive = &isActive
	}

	page, err := h.service.ListAgents(r.Context(), *tenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, page)
}

// UpdateAgent updates an agent.
func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	agentID, err := uuid.Parse(chi.URLParam(r, "agentId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid agent ID")
		return
	}

	var req UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.UpdateAgent(r.Context(), *tenantID, agentID, &req); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// UpdateAgentStatus updates an agent's status.
func (h *Handler) UpdateAgentStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	agentID, err := uuid.Parse(chi.URLParam(r, "agentId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid agent ID")
		return
	}

	if a := access.From(r.Context()); a == nil || (!a.Has("ticketing.agents.change") && (a.AgentID == nil || *a.AgentID != agentID)) {
		respondError(w, http.StatusForbidden, "you can only set your own availability")
		return
	}

	var req struct {
		Status AgentStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.UpdateAgentStatus(r.Context(), *tenantID, agentID, req.Status); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteAgent deactivates an agent.
func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	agentID, err := uuid.Parse(chi.URLParam(r, "agentId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid agent ID")
		return
	}

	if err := h.service.DeleteAgent(r.Context(), *tenantID, agentID); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

// Bounds of GET /agents/available.
const (
	defaultAvailableAgents = 50
	maxAvailableAgents     = 200
)

// GetAvailableAgents returns agents available for assignment, least loaded first, at most
// ?limit= (default 50, max 200).
// @Summary      Available agents
// @Tags         identity
// @Produce      json
// @Param        tenant path  string false "Tenant slug"
// @Param        teamId query string false "Filter by team UUID"
// @Param        limit  query int    false "At most this many agents (default 50, max 200)"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/agents/available [get]
func (h *Handler) GetAvailableAgents(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	var teamID *uuid.UUID
	if v := r.URL.Query().Get("teamId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			teamID = &id
		}
	}
	limit := defaultAvailableAgents
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, maxAvailableAgents)
	}

	agents, err := h.service.GetAvailableAgents(r.Context(), *tenantID, teamID, limit)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": agents})
}
