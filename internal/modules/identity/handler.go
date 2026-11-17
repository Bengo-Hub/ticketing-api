package identity

import (
	"encoding/json"
	"net/http"

	"github.com/bengobox/ticketing-service/internal/platform/access"
	"github.com/go-chi/chi/v5"
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
		teams.With(access.RequirePermission("ticketing.teams.add")).Post("/", h.CreateTeam)
		teams.With(access.RequirePermission("ticketing.teams.view")).Get("/", h.ListTeams)
		teams.With(access.RequirePermission("ticketing.teams.view")).Get("/{teamId}", h.GetTeam)
		teams.With(access.RequirePermission("ticketing.teams.change")).Put("/{teamId}", h.UpdateTeam)
		teams.With(access.RequirePermission("ticketing.teams.delete")).Delete("/{teamId}", h.DeleteTeam)
	})

	r.Route("/agents", func(agents chi.Router) {
		agents.With(access.RequirePermission("ticketing.agents.add")).Post("/", h.CreateAgent)
		agents.With(access.RequirePermission("ticketing.agents.view")).Get("/", h.ListAgents)
		agents.With(access.RequirePermission("ticketing.agents.view")).Get("/available", h.GetAvailableAgents)
		agents.With(access.RequirePermission("ticketing.agents.view")).Get("/{agentId}", h.GetAgent)
		agents.With(access.RequirePermission("ticketing.agents.view")).Get("/user/{userId}", h.GetAgentByUserID)
		agents.With(access.RequirePermission("ticketing.agents.change")).Put("/{agentId}", h.UpdateAgent)
		// An agent sets their own availability; changing another agent's needs agents.change.
		agents.With(access.RequireAnyPermission("ticketing.agents.change", "ticketing.agents.change_own")).
			Put("/{agentId}/status", h.UpdateAgentStatus)
		agents.With(access.RequirePermission("ticketing.agents.delete")).Delete("/{agentId}", h.DeleteAgent)
	})
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	switch err {
	case ErrInvalidCursor:
		respondError(w, http.StatusBadRequest, err.Error())
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
