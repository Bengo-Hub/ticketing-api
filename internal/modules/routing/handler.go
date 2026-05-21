package routing

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

// Handler handles HTTP requests for routing rule management.
type Handler struct {
	service *Service
	logger  *zap.Logger
}

// NewHandler creates a new routing handler.
func NewHandler(service *Service, logger *zap.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger.Named("routing-handler"),
	}
}

// RegisterRoutes registers routing rule routes on the given router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/routing/rules", func(rules chi.Router) {
		rules.Post("/", h.CreateRule)
		rules.Get("/", h.ListRules)
		rules.Get("/{ruleId}", h.GetRule)
		rules.Put("/{ruleId}", h.UpdateRule)
		rules.Delete("/{ruleId}", h.DeleteRule)
	})
}

// CreateRule creates a new routing rule.
// @Summary      Create routing rule
// @Description  Create a new ticket auto-routing rule for the authenticated tenant
// @Tags         routing
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreateRuleRequest true "Rule payload"
// @Success      201 {object} RoutingRule
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/routing/rules [post]
func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
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

	var req CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = *tenantID

	rule, err := h.service.CreateRule(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, rule)
}

// GetRule retrieves a routing rule by ID.
// @Summary      Get routing rule
// @Description  Get a routing rule by ID
// @Tags         routing
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        ruleId path string true "Rule ID"
// @Success      200 {object} RoutingRule
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/routing/rules/{ruleId} [get]
func (h *Handler) GetRule(w http.ResponseWriter, r *http.Request) {
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
	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	rule, err := h.service.GetRule(r.Context(), *tenantID, ruleID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, rule)
}

// ListRules lists routing rules for the tenant.
// @Summary      List routing rules
// @Description  List all routing rules for the authenticated tenant
// @Tags         routing
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Success      200 {array} RoutingRule
// @Security     BearerAuth
// @Router       /{tenant}/routing/rules [get]
func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
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
	rules, total, err := h.service.ListRules(r.Context(), RuleFilter{
		TenantID: *tenantID,
		Offset:   p.Offset,
		Limit:    p.Limit,
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(rules, total, p))
}

// UpdateRule updates a routing rule.
// @Summary      Update routing rule
// @Description  Update a routing rule
// @Tags         routing
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        ruleId path string true "Rule ID"
// @Param        body   body UpdateRuleRequest true "Update payload"
// @Success      200 {object} RoutingRule
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/routing/rules/{ruleId} [put]
func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
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
	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	var req UpdateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	rule, err := h.service.UpdateRule(r.Context(), *tenantID, ruleID, &req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, rule)
}

// DeleteRule deletes a routing rule.
// @Summary      Delete routing rule
// @Description  Delete a routing rule by ID
// @Tags         routing
// @Param        tenant path string true "Tenant slug"
// @Param        ruleId path string true "Rule ID"
// @Success      204
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/routing/rules/{ruleId} [delete]
func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
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
	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	if err := h.service.DeleteRule(r.Context(), *tenantID, ruleID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrRuleNotFound):
		respondError(w, http.StatusNotFound, "routing rule not found")
	case errors.Is(err, ErrInvalidRule):
		respondError(w, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error("routing handler error", zap.Error(err))
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
