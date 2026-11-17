package rbac

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

const errRolesAdminOnly = "only an admin of this organisation can change roles"

// ListRoles lists the tenant's roles with member and permission counts.
func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	roles, err := h.svc.ListRoleViews(r.Context(), c.tenant)
	if err != nil {
		h.respondRBACError(w, "list roles", err)
		return
	}
	access.WriteJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

// roleRequest is the body of a new, customized or cloned role.
type roleRequest struct {
	RoleCode        string   `json:"role_code"`
	Name            string   `json:"name"`
	Description     *string  `json:"description"`
	PermissionCodes []string `json:"permission_codes"`
}

func (req roleRequest) input() RoleInput {
	return RoleInput{Code: req.RoleCode, Name: req.Name, Description: req.Description, PermissionCodes: req.PermissionCodes}
}

// CreateRole adds a role of the tenant's own.
func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok || !requireAdminTier(w, c, errRolesAdminOnly) {
		return
	}
	var req roleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	role, err := h.svc.CreateCustomRole(r.Context(), c.tenant, req.input())
	if err != nil {
		h.respondRBACError(w, "create the role", err)
		return
	}
	h.audit(r, c, "role.created", "role", role.ID.String(), map[string]any{
		"role_code": role.RoleCode, "name": role.Name, "permission_codes": req.PermissionCodes,
	})
	access.WriteJSON(w, http.StatusCreated, role)
}

// CustomizeRole makes a system role the tenant's own, so its edits survive the seeder.
func (h *Handler) CustomizeRole(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok || !requireAdminTier(w, c, errRolesAdminOnly) {
		return
	}
	var req roleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	role, err := h.svc.CustomizeRole(r.Context(), c.tenant, req.RoleCode)
	if err != nil {
		h.respondRBACError(w, "customise the role", err)
		return
	}
	h.audit(r, c, "role.customized", "role", role.ID.String(), map[string]any{"role_code": role.RoleCode})
	access.WriteJSON(w, http.StatusOK, role)
}

// CloneRole starts a new role from an existing one.
func (h *Handler) CloneRole(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok || !requireAdminTier(w, c, errRolesAdminOnly) {
		return
	}
	sourceID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req roleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	role, err := h.svc.CloneRole(r.Context(), c.tenant, sourceID, req.input())
	if err != nil {
		h.respondRBACError(w, "copy the role", err)
		return
	}
	h.audit(r, c, "role.cloned", "role", role.ID.String(), map[string]any{
		"role_code": role.RoleCode, "name": role.Name, "source_role_id": sourceID,
	})
	access.WriteJSON(w, http.StatusCreated, role)
}

// UpdateRole renames a role or changes its description.
func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok || !requireAdminTier(w, c, errRolesAdminOnly) {
		return
	}
	roleID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	role, err := h.svc.UpdateRoleDetails(r.Context(), c.tenant, roleID, req.Name, req.Description)
	if err != nil {
		h.respondRBACError(w, "update the role", err)
		return
	}
	h.audit(r, c, "role.updated", "role", role.ID.String(), map[string]any{"name": req.Name, "description": req.Description})
	access.WriteJSON(w, http.StatusOK, role)
}

// DeleteRole removes one of the tenant's own roles.
func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok || !requireAdminTier(w, c, errRolesAdminOnly) {
		return
	}
	roleID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	role, err := h.svc.DeleteRole(r.Context(), c.tenant, roleID)
	if err != nil {
		h.respondRBACError(w, "delete the role", err)
		return
	}
	h.audit(r, c, "role.deleted", "role", role.ID.String(), map[string]any{"role_code": role.RoleCode, "name": role.Name})
	w.WriteHeader(http.StatusNoContent)
}

// GetRolePermissions reads a role's current permissions.
func (h *Handler) GetRolePermissions(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	roleID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	codes, err := h.svc.RolePermissionCodes(r.Context(), c.tenant, roleID)
	if err != nil {
		h.respondRBACError(w, "read the role", err)
		return
	}
	access.WriteJSON(w, http.StatusOK, map[string]any{"permission_codes": codes})
}

// SetRolePermissions replaces a role's permissions and answers with the role, which stops being a
// system role the first time a system role is edited.
func (h *Handler) SetRolePermissions(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok || !requireAdminTier(w, c, errRolesAdminOnly) {
		return
	}
	roleID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		PermissionCodes []string `json:"permission_codes"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	// An absent list is a broken client; saving it would strip the role of everything.
	if req.PermissionCodes == nil {
		respondError(w, http.StatusUnprocessableEntity, "permission_codes is required")
		return
	}
	role, before, err := h.svc.SetRolePermissions(r.Context(), c.tenant, c.guard, roleID, req.PermissionCodes)
	if err != nil {
		h.respondRBACError(w, "save the role", err)
		return
	}
	h.audit(r, c, "role.permissions_updated", "role", role.ID.String(), map[string]any{
		"role_code": role.RoleCode, "before": before, "after": req.PermissionCodes,
	})
	access.WriteJSON(w, http.StatusOK, role)
}

// ListPermissions lists the permission catalogue.
func (h *Handler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := h.svc.ListPermissions(r.Context())
	if err != nil {
		h.respondRBACError(w, "list permissions", err)
		return
	}
	access.WriteJSON(w, http.StatusOK, map[string]any{"permissions": perms})
}

// assignRoleRequest is the body of a grant or renewal.
type assignRoleRequest struct {
	RoleID    uuid.UUID  `json:"role_id"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// AssignUserRole grants a role to a person or renews the one they have.
func (h *Handler) AssignUserRole(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req assignRoleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if status, msg := h.checkGrant(r, c, req.RoleID); status != 0 {
		respondError(w, status, msg)
		return
	}
	role, err := h.svc.AssignRole(r.Context(), c.tenant, userID, req.RoleID, c.userID, req.ExpiresAt)
	if err != nil {
		h.respondRBACError(w, "assign the role", err)
		return
	}
	h.audit(r, c, "role.assigned", "user", userID.String(), map[string]any{
		"role_id": role.ID, "role_code": role.RoleCode, "expires_at": req.ExpiresAt,
	})
	access.WriteJSON(w, http.StatusCreated, role)
}

// checkGrant refuses a grant the caller may not make: an admin-equivalent role unless they are a
// platform owner, and any role carrying a permission they do not hold themselves unless they are
// admin tier. It answers 0 to go ahead.
func (h *Handler) checkGrant(r *http.Request, c rbacCaller, roleID uuid.UUID) (int, string) {
	claims := c.access.Claims
	if claims != nil && claims.IsPlatformOwner {
		return 0, ""
	}
	role, err := h.svc.visibleRole(r.Context(), c.tenant, roleID)
	if err != nil {
		if errors.Is(err, ErrRoleNotFound) {
			return http.StatusNotFound, ErrRoleNotFound.Error()
		}
		h.log.Error("could not read the role to assign", zap.Error(err))
		return http.StatusInternalServerError, "could not assign the role"
	}
	if adminEquivalentRole(role.RoleCode) {
		return http.StatusForbidden, errAdminRoleRefused
	}
	if isAdminTier(claims) {
		return 0, ""
	}
	codes, err := h.svc.repo.RolePermissionCodes(r.Context(), role.ID)
	if err != nil {
		h.log.Error("could not read the role's permissions", zap.Error(err))
		return http.StatusInternalServerError, "could not assign the role"
	}
	for _, code := range codes {
		if c.access.Has(code) || (claims != nil && claims.HasPermission(code)) {
			continue
		}
		return http.StatusForbidden, "you cannot give a role with permissions you do not hold"
	}
	return 0, ""
}

// RevokeUserRole ends a person's role. Admin tier only: a manager who could revoke any role could
// remove an admin's access, and sign-in does not give a revoked role back.
func (h *Handler) RevokeUserRole(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok || !requireAdminTier(w, c, errRolesAdminOnly) {
		return
	}
	userID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	roleID, ok := pathUUID(w, r, "role_id")
	if !ok {
		return
	}
	role, err := h.svc.RevokeRole(r.Context(), c.tenant, c.guard, userID, roleID)
	if err != nil {
		h.respondRBACError(w, "remove the role", err)
		return
	}
	h.audit(r, c, "role.revoked", "user", userID.String(), map[string]any{"role_id": role.ID, "role_code": role.RoleCode})
	w.WriteHeader(http.StatusNoContent)
}
