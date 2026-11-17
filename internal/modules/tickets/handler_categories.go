package tickets

import (
	"encoding/json"
	"net/http"

	pagination "github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// CreateCategory creates a new ticket category.
// @Summary      Create category
// @Tags         categories
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreateCategoryRequest true "Category payload"
// @Success      201 {object} TicketCategory
// @Security     BearerAuth
// @Router       /{tenant}/categories [post]
func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = a.TenantID
	category, err := h.service.CreateCategory(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, category)
}

func categoryIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "categoryId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid category ID")
		return uuid.Nil, false
	}
	return id, true
}

// GetCategory retrieves a category by ID.
func (h *Handler) GetCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := categoryIDParam(w, r)
	if !ok {
		return
	}
	category, err := h.service.GetCategory(r.Context(), a.TenantID, id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, category)
}

// ListCategories lists ticket categories with filtering and pagination.
// @Summary      List categories
// @Tags         categories
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/categories [get]
func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	filter := CategoryFilter{TenantID: a.TenantID, Limit: p.Limit, Offset: p.Offset}
	q := r.URL.Query()
	if id, err := uuid.Parse(q.Get("parentId")); err == nil {
		filter.ParentID = &id
	}
	if v := q.Get("active"); v != "" {
		isActive := v == "true"
		filter.IsActive = &isActive
	}
	categories, total, err := h.service.ListCategories(r.Context(), a.TenantID, filter)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(categories, total, p))
}

// UpdateCategory updates a category.
func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := categoryIDParam(w, r)
	if !ok {
		return
	}
	var req UpdateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.UpdateCategory(r.Context(), a.TenantID, id, &req); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteCategory deactivates a category.
func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := caller(w, r)
	if !ok {
		return
	}
	id, ok := categoryIDParam(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteCategory(r.Context(), a.TenantID, id); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
