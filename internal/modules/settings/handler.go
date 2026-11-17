package settings

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// Handler serves the tenant settings routes.
type Handler struct {
	service *Service
	// forwardingAddress is the address the tenant forwards its support mail to.
	forwardingAddress func(slug string) string
	logger            *zap.Logger
}

// NewHandler creates the settings handler. forwardingAddress may be nil (no email intake).
func NewHandler(service *Service, forwardingAddress func(slug string) string, logger *zap.Logger) *Handler {
	return &Handler{service: service, forwardingAddress: forwardingAddress, logger: logger.Named("settings-handler")}
}

// RegisterRoutes mounts the settings routes on the protected tenant router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/settings/channels", func(c chi.Router) {
		c.With(access.RequirePermission("ticketing.settings.manage")).Get("/", h.GetChannels)
		c.With(access.RequirePermission("ticketing.settings.manage")).Put("/", h.PutChannels)
	})
	r.Route("/settings/ai", func(c chi.Router) {
		c.With(access.RequirePermission("ticketing.settings.manage")).Get("/", h.GetAI)
		c.With(access.RequirePermission("ticketing.settings.manage")).Put("/", h.PutAI)
	})
}

// channelsView is the stored settings plus the email intake address, which is derived, never stored.
type channelsView struct {
	WhatsApp WhatsAppChannel `json:"whatsapp"`
	Email    emailView       `json:"email"`
}

type emailView struct {
	// ForwardingAddress is where the tenant auto-forwards its support mailbox.
	ForwardingAddress string `json:"forwarding_address"`
	// LastReceivedAt is when the last forwarded email arrived, null until the first one: the
	// forwarding verify step shows the address as working once it is set.
	LastReceivedAt *time.Time `json:"last_received_at"`
}

// channelsInput is what a client may change. The email receipt is written by the ingest only, so
// an email key in the body is ignored.
type channelsInput struct {
	WhatsApp WhatsAppChannel `json:"whatsapp"`
}

// GetChannels returns the tenant's channel settings.
// @Summary      Channel settings
// @Description  WhatsApp auto-ticketing, the email forwarding address and when the last forwarded email arrived
// @Tags         settings
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Success      200 {object} channelsView
// @Security     BearerAuth
// @Router       /{tenant}/settings/channels [get]
func (h *Handler) GetChannels(w http.ResponseWriter, r *http.Request) {
	a := access.From(r.Context())
	if a == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	c, err := h.service.Channels(r.Context(), a.TenantID)
	if err != nil {
		h.logger.Error("read channel settings", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to read settings")
		return
	}
	respondJSON(w, http.StatusOK, h.view(r, a, c))
}

// PutChannels saves the tenant's channel settings.
// @Summary      Save channel settings
// @Description  Turns WhatsApp auto-ticketing on or off for the tenant. email.last_received_at is read only.
// @Tags         settings
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body channelsInput true "Channel settings"
// @Success      200 {object} channelsView
// @Failure      400 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/settings/channels [put]
func (h *Handler) PutChannels(w http.ResponseWriter, r *http.Request) {
	a := access.From(r.Context())
	if a == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in channelsInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.SetWhatsApp(r.Context(), a.TenantID, in.WhatsApp); err != nil {
		h.logger.Error("save channel settings", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	c, err := h.service.Channels(r.Context(), a.TenantID)
	if err != nil {
		h.logger.Error("read channel settings", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to read settings")
		return
	}
	respondJSON(w, http.StatusOK, h.view(r, a, c))
}

func (h *Handler) view(r *http.Request, a *access.Access, c Channels) channelsView {
	v := channelsView{WhatsApp: c.WhatsApp, Email: emailView{LastReceivedAt: c.Email.LastReceivedAt}}
	slug := a.TenantSlug
	if slug == "" {
		slug = chi.URLParam(r, "tenant")
	}
	if h.forwardingAddress != nil && slug != "" {
		v.Email.ForwardingAddress = h.forwardingAddress(slug)
	}
	return v
}

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
