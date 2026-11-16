package tickets

import (
	"net/http"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// Saved views and macros. Every route needs tickets.view (staff); a shared view is changed only
// with tickets.manage, and macros are written with kb.manage.

func viewActor(a *access.Access) ViewActor {
	return ViewActor{UserID: a.AuthUserID, Manage: a.Has(permTicketsManage)}
}

// ListViews lists the caller's views and the tenant's shared views.
// @Summary      List saved views
// @Tags         views
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/views [get]
func (h *Handler) ListViews(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	views, err := h.service.ListViews(r.Context(), a.TenantID, a.AuthUserID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": views})
}

// CreateView saves a view; shared=true needs tickets.manage.
// @Summary      Create saved view
// @Tags         views
// @Accept       json
// @Produce      json
// @Param        tenant path string    true "Tenant slug"
// @Param        body   body ViewInput true "View"
// @Success      201 {object} TicketView
// @Security     BearerAuth
// @Router       /{tenant}/views [post]
func (h *Handler) CreateView(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	var in ViewInput
	if !decodeJSON(w, r, &in) {
		return
	}
	v, err := h.service.CreateView(r.Context(), a.TenantID, viewActor(a), in)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, v)
}

// GetView returns one of the caller's views or a shared view.
func (h *Handler) GetView(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "viewId", "view")
	if !ok {
		return
	}
	v, err := h.service.GetView(r.Context(), a.TenantID, id, a.AuthUserID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, v)
}

// UpdateView replaces a view the caller may edit.
func (h *Handler) UpdateView(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "viewId", "view")
	if !ok {
		return
	}
	var in ViewInput
	if !decodeJSON(w, r, &in) {
		return
	}
	v, err := h.service.UpdateView(r.Context(), a.TenantID, id, viewActor(a), in)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, v)
}

// DeleteView removes a view the caller may edit.
func (h *Handler) DeleteView(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "viewId", "view")
	if !ok {
		return
	}
	if err := h.service.DeleteView(r.Context(), a.TenantID, id, viewActor(a)); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ListMacros lists the tenant's macros by name; ?active=true leaves out inactive ones.
// @Summary      List macros
// @Tags         macros
// @Produce      json
// @Param        tenant path  string true  "Tenant slug"
// @Param        active query bool   false "Only active macros"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/macros [get]
func (h *Handler) ListMacros(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	macros, err := h.service.ListMacros(r.Context(), a.TenantID, r.URL.Query().Get("active") == "true")
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": macros})
}

// GetMacro returns one macro.
func (h *Handler) GetMacro(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "macroId", "macro")
	if !ok {
		return
	}
	m, err := h.service.GetMacro(r.Context(), a.TenantID, id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, m)
}

// CreateMacro stores a macro.
// @Summary      Create macro
// @Tags         macros
// @Accept       json
// @Produce      json
// @Param        tenant path string     true "Tenant slug"
// @Param        body   body MacroInput true "Macro"
// @Success      201 {object} Macro
// @Security     BearerAuth
// @Router       /{tenant}/macros [post]
func (h *Handler) CreateMacro(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	var in MacroInput
	if !decodeJSON(w, r, &in) {
		return
	}
	m, err := h.service.CreateMacro(r.Context(), a.TenantID, a.AuthUserID, in)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

// UpdateMacro replaces a macro.
func (h *Handler) UpdateMacro(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "macroId", "macro")
	if !ok {
		return
	}
	var in MacroInput
	if !decodeJSON(w, r, &in) {
		return
	}
	m, err := h.service.UpdateMacro(r.Context(), a.TenantID, id, in)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, m)
}

// DeleteMacro removes a macro.
func (h *Handler) DeleteMacro(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "macroId", "macro")
	if !ok {
		return
	}
	if err := h.service.DeleteMacro(r.Context(), a.TenantID, id); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
