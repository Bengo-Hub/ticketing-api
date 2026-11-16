package sla

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	pagination "github.com/Bengo-Hub/pagination"
	"github.com/bengobox/ticketing-service/internal/platform/access"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Reporter runs the SLA report (ReportRepository).
type Reporter interface {
	Report(ctx context.Context, f ReportFilter) (*Report, error)
}

// Handler handles HTTP requests for SLA policies, business calendars and the SLA report.
type Handler struct {
	service *Service
	reports Reporter
	logger  *zap.Logger
}

// NewHandler creates a new SLA handler.
func NewHandler(service *Service, reports Reporter, logger *zap.Logger) *Handler {
	return &Handler{
		service: service,
		reports: reports,
		logger:  logger.Named("sla-handler"),
	}
}

// RegisterRoutes registers SLA routes on the given router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/sla", func(sla chi.Router) {
		sla.Route("/policies", func(policies chi.Router) {
			policies.With(access.RequirePermission("ticketing.sla.add")).Post("/", h.CreatePolicy)
			policies.With(access.RequirePermission("ticketing.sla.view")).Get("/", h.ListPolicies)
			policies.With(access.RequirePermission("ticketing.sla.view")).Get("/{policyId}", h.GetPolicy)
			policies.With(access.RequirePermission("ticketing.sla.change")).Put("/{policyId}", h.UpdatePolicy)
			policies.With(access.RequirePermission("ticketing.sla.delete")).Delete("/{policyId}", h.DeletePolicy)
		})
		sla.Route("/calendars", func(cals chi.Router) {
			cals.With(access.RequirePermission("ticketing.sla.add")).Post("/", h.CreateCalendar)
			cals.With(access.RequirePermission("ticketing.sla.view")).Get("/", h.ListCalendars)
			cals.With(access.RequirePermission("ticketing.sla.view")).Get("/{calendarId}", h.GetCalendar)
			cals.With(access.RequirePermission("ticketing.sla.change")).Put("/{calendarId}", h.UpdateCalendar)
			cals.With(access.RequirePermission("ticketing.sla.delete")).Delete("/{calendarId}", h.DeleteCalendar)
		})
		sla.With(access.RequirePermission("ticketing.analytics.view")).Get("/reports", h.GetReport)
	})
}

// CreatePolicy creates a new SLA policy.
// @Summary      Create SLA policy
// @Description  Create a new SLA policy for the authenticated tenant
// @Tags         sla
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreatePolicyRequest true "Policy payload"
// @Success      201 {object} Policy
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/sla/policies [post]
func (h *Handler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	var req CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = *tenantID

	policy, err := h.service.CreatePolicy(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, policy)
}

// GetPolicy retrieves an SLA policy by ID.
// @Summary      Get SLA policy
// @Description  Retrieve an SLA policy by its UUID
// @Tags         sla
// @Produce      json
// @Param        tenant   path string true "Tenant slug"
// @Param        policyId path string true "Policy UUID"
// @Success      200 {object} Policy
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/sla/policies/{policyId} [get]
func (h *Handler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	policyID, err := uuid.Parse(chi.URLParam(r, "policyId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid policy ID")
		return
	}

	policy, err := h.service.GetPolicy(r.Context(), *tenantID, policyID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, policy)
}

// ListPolicies lists SLA policies with filtering and pagination.
// @Summary      List SLA policies
// @Description  List SLA policies for the authenticated tenant
// @Tags         sla
// @Produce      json
// @Param        tenant    path  string false "Tenant slug"
// @Param        priority  query string false "Filter by priority"
// @Param        is_active query bool   false "Filter by active status"
// @Param        page      query int    false "Page number"
// @Param        limit     query int    false "Results per page"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/sla/policies [get]
func (h *Handler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	p := pagination.Parse(r)
	filter := PolicyFilter{
		TenantID: *tenantID,
		Offset:   p.Offset,
		Limit:    p.Limit,
	}

	if prio := r.URL.Query().Get("priority"); prio != "" {
		pr := Priority(prio)
		filter.Priority = &pr
	}
	if active := r.URL.Query().Get("is_active"); active == "true" {
		t := true
		filter.IsActive = &t
	} else if active == "false" {
		f := false
		filter.IsActive = &f
	}

	policies, total, err := h.service.ListPolicies(r.Context(), filter)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(policies, total, p))
}

func (h *Handler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	policyID, err := uuid.Parse(chi.URLParam(r, "policyId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid policy ID")
		return
	}

	var req UpdatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.UpdatePolicy(r.Context(), *tenantID, policyID, &req); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}

	policyID, err := uuid.Parse(chi.URLParam(r, "policyId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid policy ID")
		return
	}

	if err := h.service.DeletePolicy(r.Context(), *tenantID, policyID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPolicyNotFound):
		respondError(w, http.StatusNotFound, "sla policy not found")
	case errors.Is(err, ErrPolicyInUse):
		respondError(w, http.StatusConflict, "sla policy is in use")
	case errors.Is(err, ErrInvalidPolicy), errors.Is(err, ErrInvalidCalendar), errors.Is(err, ErrInvalidReport):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrCalendarNotFound):
		respondError(w, http.StatusNotFound, "business calendar not found")
	case errors.Is(err, ErrCalendarInUse):
		respondError(w, http.StatusConflict, "business calendar is used by an sla policy")
	case errors.Is(err, ErrCalendarExists):
		respondError(w, http.StatusConflict, "a business calendar with this name already exists")
	default:
		h.logger.Error("sla handler error", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "internal server error")
	}
}

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
