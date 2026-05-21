package sla

import (
	"encoding/json"
	"errors"
	"net/http"

	pagination "github.com/Bengo-Hub/pagination"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Handler handles HTTP requests for SLA policy management.
type Handler struct {
	service *Service
	logger  *zap.Logger
}

// NewHandler creates a new SLA handler.
func NewHandler(service *Service, logger *zap.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger.Named("sla-handler"),
	}
}

// RegisterRoutes registers SLA routes on the given router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/sla", func(sla chi.Router) {
		sla.Route("/policies", func(policies chi.Router) {
			policies.Post("/", h.CreatePolicy)
			policies.Get("/", h.ListPolicies)
			policies.Get("/{policyId}", h.GetPolicy)
			policies.Put("/{policyId}", h.UpdatePolicy)
			policies.Delete("/{policyId}", h.DeletePolicy)
		})
	})
}

func (h *Handler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) GetPolicy(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) ListPolicies(w http.ResponseWriter, r *http.Request) {
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
	case errors.Is(err, ErrInvalidPolicy):
		respondError(w, http.StatusBadRequest, err.Error())
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
