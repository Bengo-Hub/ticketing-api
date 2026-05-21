package identity

import (
	"encoding/json"
	"net/http"

	pagination "github.com/Bengo-Hub/pagination"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Handler handles HTTP requests for identity operations.
type Handler struct {
	service *Service
	logger  *zap.Logger
}

// NewHandler creates a new identity handler.
func NewHandler(service *Service, logger *zap.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger.Named("identity-handler"),
	}
}

// RegisterRoutes registers the identity routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/teams", func(teams chi.Router) {
		teams.Post("/", h.CreateTeam)
		teams.Get("/", h.ListTeams)
		teams.Get("/{teamId}", h.GetTeam)
		teams.Put("/{teamId}", h.UpdateTeam)
		teams.Delete("/{teamId}", h.DeleteTeam)
	})

	r.Route("/agents", func(agents chi.Router) {
		agents.Post("/", h.CreateAgent)
		agents.Get("/", h.ListAgents)
		agents.Get("/available", h.GetAvailableAgents)
		agents.Get("/{agentId}", h.GetAgent)
		agents.Get("/user/{userId}", h.GetAgentByUserID)
		agents.Put("/{agentId}", h.UpdateAgent)
		agents.Put("/{agentId}/status", h.UpdateAgentStatus)
		agents.Delete("/{agentId}", h.DeleteAgent)
	})
}

// Team handlers

// CreateTeam creates a new support team.
// @Summary      Create team
// @Description  Create a new support team for the authenticated tenant
// @Tags         identity
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreateTeamRequest true "Team payload"
// @Success      201 {object} Team
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/teams [post]
func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
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

	var req CreateTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = *tenantID

	team, err := h.service.CreateTeam(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, team)
}

// GetTeam retrieves a team by ID.
func (h *Handler) GetTeam(w http.ResponseWriter, r *http.Request) {
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

	teamID, err := uuid.Parse(chi.URLParam(r, "teamId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid team ID")
		return
	}

	team, err := h.service.GetTeam(r.Context(), *tenantID, teamID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, team)
}

// ListTeams lists teams with pagination.
// @Summary      List teams
// @Description  List support teams for the authenticated tenant
// @Tags         identity
// @Produce      json
// @Param        tenant path  string false "Tenant slug"
// @Param        active query bool   false "Filter by active status"
// @Param        page   query int    false "Page number"
// @Param        limit  query int    false "Results per page"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/teams [get]
func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
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
	filter := TeamFilter{TenantID: *tenantID, Limit: p.Limit, Offset: p.Offset}

	if v := r.URL.Query().Get("active"); v != "" {
		isActive := v == "true"
		filter.IsActive = &isActive
	}

	teams, total, err := h.service.ListTeams(r.Context(), *tenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, pagination.NewResponse(teams, total, p))
}

// UpdateTeam updates a team.
func (h *Handler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
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

	teamID, err := uuid.Parse(chi.URLParam(r, "teamId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid team ID")
		return
	}

	var req UpdateTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.UpdateTeam(r.Context(), *tenantID, teamID, &req); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteTeam soft-deletes a team.
func (h *Handler) DeleteTeam(w http.ResponseWriter, r *http.Request) {
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

	teamID, err := uuid.Parse(chi.URLParam(r, "teamId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid team ID")
		return
	}

	if err := h.service.DeleteTeam(r.Context(), *tenantID, teamID); err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Agent handlers

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

// ListAgents lists agents with pagination.
// @Summary      List agents
// @Description  List support agents for the authenticated tenant
// @Tags         identity
// @Produce      json
// @Param        tenant path  string false "Tenant slug"
// @Param        teamId query string false "Filter by team UUID"
// @Param        status query string false "Filter by agent status"
// @Param        role   query string false "Filter by agent role"
// @Param        active query bool   false "Filter by active status"
// @Param        page   query int    false "Page number"
// @Param        limit  query int    false "Results per page"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/agents [get]
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
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
	filter := AgentFilter{TenantID: *tenantID, Limit: p.Limit, Offset: p.Offset}

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

	agents, total, err := h.service.ListAgents(r.Context(), *tenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, pagination.NewResponse(agents, total, p))
}

// UpdateAgent updates an agent.
func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
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

	agentID, err := uuid.Parse(chi.URLParam(r, "agentId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid agent ID")
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

// GetAvailableAgents returns agents available for assignment.
func (h *Handler) GetAvailableAgents(w http.ResponseWriter, r *http.Request) {
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

	var teamID *uuid.UUID
	if v := r.URL.Query().Get("teamId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			teamID = &id
		}
	}

	agents, err := h.service.GetAvailableAgents(r.Context(), *tenantID, teamID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"agents": agents,
		"total":  len(agents),
	})
}

// Helper functions

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	switch err {
	case ErrTeamNotFound, ErrAgentNotFound:
		respondError(w, http.StatusNotFound, err.Error())
	case ErrTeamExists, ErrAgentExists:
		respondError(w, http.StatusConflict, err.Error())
	case ErrAgentInactive:
		respondError(w, http.StatusForbidden, err.Error())
	case ErrAgentUnavailable, ErrAgentAtCapacity:
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
