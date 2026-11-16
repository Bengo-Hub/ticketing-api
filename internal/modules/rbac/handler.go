package rbac

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// Handler serves the caller's ticketing profile and role administration.
type Handler struct {
	svc *Service
	log *zap.Logger
}

// NewHandler creates the RBAC handler.
func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log.Named("rbac-handler")}
}

// RegisterRoutes mounts the routes under the protected /{tenant} group. auth/me needs only a
// signed-in caller in the tenant; role administration needs the rbac permissions.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/ticketing", func(t chi.Router) {
		t.Get("/auth/me", h.Me)
		t.With(access.RequirePermission(Code("rbac", "view"))).Get("/roles", h.ListRoles)
		t.With(access.RequirePermission(Code("rbac", "view"))).Get("/users", h.ListUsers)
		t.With(access.RequirePermission(Code("rbac", "manage"))).Put("/users/{userId}/roles", h.SetUserRoles)
	})
}

// Me is the auth/me response: the caller's identity and ticketing access in the tenant.
type Me struct {
	UserID          string   `json:"user_id"`
	Email           string   `json:"email"`
	Name            string   `json:"name"`
	TenantID        string   `json:"tenant_id"`
	TenantSlug      string   `json:"tenant_slug"`
	GlobalRoles     []string `json:"global_roles"`
	ServiceRole     string   `json:"service_role"`
	Permissions     []string `json:"permissions"`
	AgentID         *string  `json:"agent_id"`
	IsPlatformOwner bool     `json:"is_platform_owner"`
}

// BuildMe turns resolved access into the auth/me response.
func BuildMe(a *access.Access, name string) Me {
	me := Me{
		Email:           a.Email,
		Name:            name,
		TenantID:        a.TenantID.String(),
		TenantSlug:      a.TenantSlug,
		GlobalRoles:     nonNil(a.GlobalRoles),
		ServiceRole:     strongestRole(a.Roles),
		Permissions:     nonNil(a.Perms),
		IsPlatformOwner: a.IsPlatformOwner,
	}
	if a.AuthUserID != uuid.Nil {
		me.UserID = a.AuthUserID.String()
	}
	if me.ServiceRole == "" && a.Bypass {
		me.ServiceRole = RoleAdmin
	}
	if a.AgentID != nil {
		id := a.AgentID.String()
		me.AgentID = &id
	}
	return me
}

// Me godoc
// @Summary      Ticketing profile
// @Description  The caller's ticketing role, permissions and agent profile in the tenant
// @Tags         rbac
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Success      200 {object} rbac.Me
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/ticketing/auth/me [get]
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	a := access.From(r.Context())
	if a == nil {
		access.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	name := ""
	if a.AuthUserID != uuid.Nil {
		if u, err := h.svc.FindUser(r.Context(), a.TenantID, a.AuthUserID); err == nil && u != nil {
			name = u.Name
		}
	}
	access.WriteJSON(w, http.StatusOK, BuildMe(a, name))
}

// ListRoles returns the tenant's roles with their permissions.
func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	a := access.From(r.Context())
	roles, err := h.svc.ListRoles(r.Context(), a.TenantID)
	if err != nil {
		h.fail(w, err)
		return
	}
	access.WriteJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

// ListUsers returns the tenant's ticketing users with their roles.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	a := access.From(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	users, err := h.svc.ListUsers(r.Context(), a.TenantID, limit)
	if err != nil {
		h.fail(w, err)
		return
	}
	access.WriteJSON(w, http.StatusOK, map[string]any{"users": users})
}

// SetUserRoles replaces a ticketing user's roles. Body: {"roles": ["agent"]}.
func (h *Handler) SetUserRoles(w http.ResponseWriter, r *http.Request) {
	a := access.From(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		access.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	var body struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		access.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	u, err := h.svc.SetUserRoles(r.Context(), a.TenantID, userID, a.AuthUserID, body.Roles)
	if err != nil {
		h.fail(w, err)
		return
	}
	access.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnknownRole):
		access.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrUserNotFound):
		access.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		h.log.Error("rbac request failed", zap.Error(err))
		access.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

func strongestRole(roles []string) string {
	best := ""
	for _, r := range roles {
		if rolePriority[r] > rolePriority[best] {
			best = r
		}
	}
	if best == "" && len(roles) > 0 {
		return roles[0]
	}
	return best
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
