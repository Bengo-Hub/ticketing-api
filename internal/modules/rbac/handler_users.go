package rbac

import (
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/Bengo-Hub/pagination"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// CreateUserRequest adds a person to the tenant. Roles are the tenant membership roles sent to
// auth-api; empty means auth's default (staff). tenant_slug is accepted for treasury parity and
// ignored: the tenant is the one the request acts in.
type CreateUserRequest struct {
	Email      string   `json:"email"`
	TenantSlug string   `json:"tenant_slug"`
	Roles      []string `json:"roles,omitempty"`
}

// CreateUser adds a person through auth-api's S2S member add (which finds or creates the SSO
// account by email) and records them as a ticketing user. Ticketing stores no password and never
// creates an account itself.
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok || !requireAdminTier(w, c, "only an admin of this organisation can add users") {
		return
	}
	var req CreateUserRequest
	if !decodeBody(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if _, err := mail.ParseAddress(email); err != nil || email == "" {
		respondError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	// The S2S member add trusts the roles we send, so admin-equivalent ones are refused here for
	// everyone. Admins are made in auth-ui.
	roles := make([]string, 0, len(req.Roles))
	for _, role := range req.Roles {
		canonical := strings.ToLower(strings.TrimSpace(role))
		if canonical == "" {
			continue
		}
		if adminEquivalentRole(canonical) {
			respondError(w, http.StatusForbidden, errAdminRoleRefused)
			return
		}
		roles = append(roles, canonical)
	}
	if h.members == nil {
		h.log.Error("member directory not configured; cannot add users")
		respondError(w, http.StatusInternalServerError, "failed to sync user")
		return
	}
	authUserID, err := h.members.AddMember(r.Context(), c.tenant, email, roles)
	if err != nil {
		h.log.Error("failed to add member in auth-api", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to sync user")
		return
	}
	user, err := h.svc.AddMember(r.Context(), c.tenant, authUserID, email)
	if err != nil {
		h.log.Error("failed to record ticketing user", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to sync user")
		return
	}
	h.audit(r, c, "create", "user", user.ID.String(), map[string]any{"email": user.Email})
	access.WriteJSON(w, http.StatusCreated, map[string]any{
		"user_id":   user.ID,
		"email":     user.Email,
		"tenant_id": user.TenantID,
		"created":   true,
	})
}

// GetUser reads one person by ticketing user id or auth-api user id.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	user, err := h.svc.GetUser(r.Context(), c.tenant, userID)
	if err != nil {
		h.respondRBACError(w, "read the user", err)
		return
	}
	access.WriteJSON(w, http.StatusOK, user)
}

// ListUsers pages the tenant's people with the roles they hold or held. ?search= matches part of
// the email or name; ?limit= and ?page= (or ?offset=) page it.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	pg := pagination.Parse(r)
	users, total, err := h.svc.ListUsersWithRoles(r.Context(), c.tenant, strings.TrimSpace(r.URL.Query().Get("search")), pg.Limit, pg.Offset)
	if err != nil {
		h.respondRBACError(w, "list users", err)
		return
	}
	access.WriteJSON(w, http.StatusOK, pagination.NewResponse(users, total, pg))
}

// GetUserRoles lists the roles a person holds now.
func (h *Handler) GetUserRoles(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	roles, err := h.svc.GetUserRoles(r.Context(), c.tenant, userID)
	if err != nil {
		h.respondRBACError(w, "get user roles", err)
		return
	}
	access.WriteJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

// AuditLogResponse is one audit log entry, in treasury-api's shape.
type AuditLogResponse struct {
	ID           string  `json:"id"`
	TenantID     *string `json:"tenant_id,omitempty"`
	UserID       string  `json:"user_id"`
	UserEmail    string  `json:"user_email"`
	Action       string  `json:"action"`
	ResourceType string  `json:"resource_type"`
	ResourceID   string  `json:"resource_id"`
	Changes      *string `json:"changes,omitempty"`
	IPAddress    *string `json:"ip_address,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// eat is East Africa Time; the from and to days of the audit list are EAT days.
var eat = time.FixedZone("EAT", 3*60*60)

// parseListDay reads a YYYY-MM-DD day and returns its start and the start of the next day in EAT.
func parseListDay(s string) (time.Time, time.Time, bool) {
	if s == "" {
		return time.Time{}, time.Time{}, false
	}
	day, err := time.ParseInLocation("2006-01-02", s, eat)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	return day, day.AddDate(0, 0, 1), true
}

// auditFilters reads the query string. The resource types are always role and user.
func auditFilters(r *http.Request) AuditFilters {
	q := r.URL.Query()
	f := AuditFilters{
		ResourceTypes: []string{"role", "user"},
		ResourceType:  q.Get("resource_type"),
		Action:        q.Get("action"),
	}
	if id, err := uuid.Parse(q.Get("user_id")); err == nil {
		f.UserID = &id
	}
	if start, _, ok := parseListDay(q.Get("from")); ok {
		f.From = &start
	}
	if _, end, ok := parseListDay(q.Get("to")); ok {
		f.To = &end
	}
	return f
}

// ListAuditLogs shows who changed which roles and people in this tenant, newest first.
// ?resource_type=&user_id=&action=&from=&to=&limit=&page=
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	pg := pagination.Parse(r)
	entries, total, err := h.svc.ListAudit(r.Context(), c.tenant, auditFilters(r), pg.Limit, pg.Offset)
	if err != nil {
		h.respondRBACError(w, "list audit logs", err)
		return
	}
	items := make([]AuditLogResponse, len(entries))
	for i, e := range entries {
		items[i] = AuditLogResponse{
			ID: e.ID.String(), UserID: e.UserID.String(), UserEmail: e.UserEmail, Action: e.Action,
			ResourceType: e.ResourceType, ResourceID: e.ResourceID, Changes: e.Changes, IPAddress: e.IPAddress,
			CreatedAt: e.CreatedAt.Format(time.RFC3339),
		}
		if e.TenantID != nil {
			tid := e.TenantID.String()
			items[i].TenantID = &tid
		}
	}
	access.WriteJSON(w, http.StatusOK, pagination.NewResponse(items, total, pg))
}
