package settings

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// SectionAI is the settings section of the AI features.
const SectionAI = "ai"

const keyAutoClassify = "auto_classify"

// AISettings are a tenant's AI settings.
type AISettings struct {
	// AutoClassify classifies every new ticket after it is saved, spending one AI credit each.
	// Off by default, so no tenant spends credits it did not choose to.
	AutoClassify bool `json:"auto_classify"`
}

// AI returns the tenant's AI settings, the defaults (all off) when it never saved any.
func (s *Service) AI(ctx context.Context, tenantID uuid.UUID) (AISettings, error) {
	var a AISettings
	if _, err := s.store.Section(ctx, tenantID, SectionAI, &a); err != nil {
		return AISettings{}, err
	}
	return a, nil
}

// AutoClassify reports whether the tenant opted in to classifying new tickets.
func (s *Service) AutoClassify(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	a, err := s.AI(ctx, tenantID)
	return a.AutoClassify, err
}

// SetAI saves the tenant's AI settings and leaves every other section as it is.
func (s *Service) SetAI(ctx context.Context, tenantID uuid.UUID, a AISettings) error {
	return s.store.PutSectionKey(ctx, tenantID, SectionAI, keyAutoClassify, a.AutoClassify)
}

// GetAI returns the tenant's AI settings.
// @Summary      AI settings
// @Description  Whether new tickets are classified by AI after they are saved (ai.auto_classify, off by default)
// @Tags         settings
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Success      200 {object} AISettings
// @Security     BearerAuth
// @Router       /{tenant}/settings/ai [get]
func (h *Handler) GetAI(w http.ResponseWriter, r *http.Request) {
	a := access.From(r.Context())
	if a == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	out, err := h.service.AI(r.Context(), a.TenantID)
	if err != nil {
		h.logger.Error("read ai settings", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to read settings")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// PutAI saves the tenant's AI settings.
// @Summary      Save AI settings
// @Description  Turns auto-classify of new tickets on or off. Each auto-classified ticket spends one AI credit.
// @Tags         settings
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body AISettings true "AI settings"
// @Success      200 {object} AISettings
// @Failure      400 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/settings/ai [put]
func (h *Handler) PutAI(w http.ResponseWriter, r *http.Request) {
	a := access.From(r.Context())
	if a == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in AISettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.SetAI(r.Context(), a.TenantID, in); err != nil {
		h.logger.Error("save ai settings", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	out, err := h.service.AI(r.Context(), a.TenantID)
	if err != nil {
		h.logger.Error("read ai settings", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to read settings")
		return
	}
	respondJSON(w, http.StatusOK, out)
}
