package rbac

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// MemberDirectory adds a person to a tenant in auth-api (the S2S member add), which resolves or
// creates the SSO account by email. auth-api owns identity; ticketing never creates accounts.
type MemberDirectory interface {
	AddMember(ctx context.Context, tenantID uuid.UUID, email string, roles []string) (uuid.UUID, error)
}

// Handler serves the caller's ticketing profile, role administration and user administration.
type Handler struct {
	svc     *Service
	members MemberDirectory
	log     *zap.Logger
}

// NewHandler creates the RBAC handler. members may be nil, in which case adding a user fails.
func NewHandler(svc *Service, members MemberDirectory, log *zap.Logger) *Handler {
	return &Handler{svc: svc, members: members, log: log.Named("rbac-handler")}
}

// RegisterRoutes mounts the routes under the protected /{tenant} group. auth/me needs only a
// signed-in caller in the tenant. Reads need ticketing.rbac.view and changes ticketing.rbac.manage;
// on top of that, editing roles, removing a role and adding a user need an organisation admin
// (admin tier), as in treasury-api.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/ticketing", func(t chi.Router) {
		t.Get("/auth/me", h.Me)

		t.Group(func(view chi.Router) {
			view.Use(access.RequirePermission(Code("rbac", "view")))
			view.Get("/roles", h.ListRoles)
			view.Get("/roles/{id}/permissions", h.GetRolePermissions)
			view.Get("/permissions", h.ListPermissions)
			view.Get("/rbac/audit-logs", h.ListAuditLogs)
			view.Get("/users", h.ListUsers)
			view.Get("/users/{id}", h.GetUser)
			view.Get("/users/{id}/roles", h.GetUserRoles)
		})

		t.Group(func(manage chi.Router) {
			manage.Use(access.RequirePermission(ManagePermission))
			manage.Post("/roles", h.CreateRole)
			manage.Post("/roles/customize", h.CustomizeRole)
			manage.Post("/roles/{id}/clone", h.CloneRole)
			manage.Patch("/roles/{id}", h.UpdateRole)
			manage.Delete("/roles/{id}", h.DeleteRole)
			manage.Put("/roles/{id}/permissions", h.SetRolePermissions)
			manage.Post("/users", h.CreateUser)
			manage.Post("/users/{id}/roles", h.AssignUserRole)
			manage.Delete("/users/{id}/roles/{role_id}", h.RevokeUserRole)
		})
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

// rbacCaller is the tenant and person behind an administration request.
type rbacCaller struct {
	access *access.Access
	tenant uuid.UUID
	userID uuid.UUID
	// guard is the caller's id for the self-lockout check; nil when they pass permission checks anyway.
	guard *uuid.UUID
}

// caller resolves the acting tenant (the token's, or ?tenantId= for a platform owner) and the
// person. A service key has no person and is refused, as in treasury-api.
func (h *Handler) caller(w http.ResponseWriter, r *http.Request) (rbacCaller, bool) {
	a := access.From(r.Context())
	if a == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return rbacCaller{}, false
	}
	if a.TenantID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return rbacCaller{}, false
	}
	if a.AuthUserID == uuid.Nil {
		respondError(w, http.StatusUnauthorized, "invalid user ID")
		return rbacCaller{}, false
	}
	c := rbacCaller{access: a, tenant: a.TenantID, userID: a.AuthUserID}
	if !a.Bypass {
		id := a.AuthUserID
		c.guard = &id
	}
	return c, true
}

func respondError(w http.ResponseWriter, status int, msg string) {
	access.WriteJSON(w, status, map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(into); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func pathUUID(w http.ResponseWriter, r *http.Request, param string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid "+param)
		return uuid.Nil, false
	}
	return id, true
}

// respondRBACError gives a refused action its own status and message; anything else is logged.
func (h *Handler) respondRBACError(w http.ResponseWriter, action string, err error) {
	switch {
	case errors.Is(err, ErrRoleNotFound), errors.Is(err, ErrUserNotFound), errors.Is(err, ErrAssignmentMissing):
		respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrRoleCodeTaken), errors.Is(err, ErrRoleInUse), errors.Is(err, ErrSystemRole), errors.Is(err, ErrSelfLockout):
		respondError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidRole), errors.Is(err, ErrUnknownPermission), errors.Is(err, ErrInvalidExpiry):
		respondError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		h.log.Error("role management failed", zap.String("action", action), zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not "+action)
	}
}

// audit records a change. A failed write is logged and never undoes or blocks the change.
func (h *Handler) audit(r *http.Request, c rbacCaller, action, resourceType, resourceID string, changes map[string]any) {
	tenant := c.tenant
	e := AuditEntry{
		TenantID: &tenant, UserID: c.userID, UserEmail: c.access.Email,
		Action: action, ResourceType: resourceType, ResourceID: resourceID,
	}
	if changes != nil {
		if b, err := json.Marshal(changes); err == nil {
			s := string(b)
			e.Changes = &s
		}
	}
	if ip := clientIP(r); ip != "" {
		e.IPAddress = &ip
	}
	if err := h.svc.Audit(r.Context(), e); err != nil {
		h.log.Warn("audit log write failed", zap.String("action", action), zap.String("resource_id", resourceID), zap.Error(err))
	}
}

// clientIP is the address ratelimit.TrustedRealIP left on the request.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if len(host) > 45 {
		return ""
	}
	return host
}

// adminEquivalentRoles are the admin-tier role names with spaces, hyphens and underscores
// removed, so super_admin, super-admin, super admin and SuperAdmin all match.
var adminEquivalentRoles = map[string]struct{}{
	"admin": {}, "administrator": {}, "superuser": {}, "superusers": {},
	"superadmin": {}, "owner": {}, "tenantadmin": {}, "systemadmin": {},
}

func adminRoleKey(role string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '-', '_':
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(role)))
}

// adminEquivalentRole reports a role that makes its holder an admin of the organisation. Only a
// platform owner may give one through ticketing.
func adminEquivalentRole(role string) bool {
	_, ok := adminEquivalentRoles[adminRoleKey(role)]
	return ok
}

const errAdminRoleRefused = "admin, owner and superuser roles can only be given by a platform owner"

// isAdminTier reports a caller who is an admin of the organisation or a platform owner. Holding
// ticketing.rbac.manage is not enough.
func isAdminTier(claims *authclient.Claims) bool {
	return claims != nil && (claims.IsPlatformOwner || claims.IsAdmin() || claims.IsSuperuser())
}

// requireAdminTier answers 403 unless the caller is admin tier or holds ticketing's own
// ticketing_admin role, so a tenant can delegate ticketing administration without giving
// tenant-wide admin (owner decision 2026-11-17). Role editing needs it on top of the rbac.manage
// gate, so a supervisor cannot widen a role and then hand it out.
func requireAdminTier(w http.ResponseWriter, c rbacCaller, msg string) bool {
	if !isAdminTier(c.access.Claims) && !c.access.HasRole(RoleAdmin) {
		respondError(w, http.StatusForbidden, msg)
		return false
	}
	return true
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
