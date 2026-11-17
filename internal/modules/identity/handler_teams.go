package identity

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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
	tenantID, ok := requestTenant(w, r)
	if !ok {
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
	tenantID, ok := requestTenant(w, r)
	if !ok {
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

// ListTeams lists teams a keyset page at a time, newest first.
// @Summary      List teams
// @Description  List support teams for the authenticated tenant, newest first, as {data, next_cursor, has_more}
// @Tags         identity
// @Produce      json
// @Param        tenant path  string false "Tenant slug"
// @Param        active query bool   false "Filter by active status"
// @Param        cursor query string false "next_cursor of the previous page"
// @Param        limit  query int    false "Page size (default 25, max 100)"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/teams [get]
func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	after, limit, err := parsePage(r)
	if err != nil {
		h.handleError(w, err)
		return
	}
	filter := TeamFilter{TenantID: *tenantID, After: after, Limit: limit}

	if v := r.URL.Query().Get("active"); v != "" {
		isActive := v == "true"
		filter.IsActive = &isActive
	}

	page, err := h.service.ListTeams(r.Context(), *tenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, page)
}

// UpdateTeam updates a team.
func (h *Handler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
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
	tenantID, ok := requestTenant(w, r)
	if !ok {
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
