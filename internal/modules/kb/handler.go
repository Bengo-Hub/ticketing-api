package kb

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

type Handler struct {
	service *Service
	logger  *zap.Logger
}

func NewHandler(service *Service, logger *zap.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger.Named("kb-handler"),
	}
}

// RegisterRoutes mounts KB routes under the tenant router.
// Public (read-only) routes are mounted separately via RegisterPublicRoutes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/kb", func(kb chi.Router) {
		kb.Route("/articles", func(articles chi.Router) {
			articles.Post("/", h.CreateArticle)
			articles.Get("/", h.ListArticles)
			articles.Get("/{articleId}", h.GetArticle)
			articles.Put("/{articleId}", h.UpdateArticle)
			articles.Delete("/{articleId}", h.DeleteArticle)
			articles.Post("/{articleId}/vote", h.VoteArticle)
		})
	})
}

// RegisterPublicRoutes mounts read-only public KB endpoints (no auth).
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Route("/kb", func(kb chi.Router) {
		kb.Get("/articles", h.ListPublicArticles)
		kb.Get("/articles/{slug}", h.GetPublicArticleBySlug)
		kb.Post("/articles/{articleId}/vote", h.VotePublicArticle)
	})
}

// ─── Admin / agent handlers ────────────────────────────────────────────────

// CreateArticle creates a new KB article.
// @Summary      Create KB article
// @Description  Create a new knowledge base article for the authenticated tenant
// @Tags         kb
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CreateArticleRequest true "Article payload"
// @Success      201 {object} Article
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/kb/articles [post]
func (h *Handler) CreateArticle(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Title    string        `json:"title"`
		Slug     string        `json:"slug"`
		Content  string        `json:"content"`
		Excerpt  string        `json:"excerpt"`
		Category string        `json:"category"`
		Tags     []string      `json:"tags"`
		Status   ArticleStatus `json:"status"`
		IsPublic *bool         `json:"is_public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	isPublic := true
	if body.IsPublic != nil {
		isPublic = *body.IsPublic
	}
	agentID := uuid.MustParse(claims.Subject)

	article, err := h.service.CreateArticle(r.Context(), CreateArticleRequest{
		TenantID: *tenantID,
		AuthorID: agentID,
		Title:    body.Title,
		Slug:     body.Slug,
		Content:  body.Content,
		Excerpt:  body.Excerpt,
		Category: body.Category,
		Tags:     body.Tags,
		Status:   body.Status,
		IsPublic: isPublic,
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, article)
}

// GetArticle retrieves a KB article by ID.
// @Summary      Get KB article
// @Description  Retrieve a knowledge base article by its UUID
// @Tags         kb
// @Produce      json
// @Param        tenant    path string true "Tenant slug"
// @Param        articleId path string true "Article UUID"
// @Success      200 {object} Article
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/kb/articles/{articleId} [get]
func (h *Handler) GetArticle(w http.ResponseWriter, r *http.Request) {
	tenantID := h.requireTenantID(w, r)
	if tenantID == uuid.Nil {
		return
	}
	articleID, err := uuid.Parse(chi.URLParam(r, "articleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid article ID")
		return
	}
	article, err := h.service.GetArticle(r.Context(), tenantID, articleID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, article)
}

// ListArticles lists KB articles with filtering and pagination.
// @Summary      List KB articles
// @Description  List knowledge base articles for the authenticated tenant
// @Tags         kb
// @Produce      json
// @Param        tenant   path  string false "Tenant slug"
// @Param        search   query string false "Search text"
// @Param        category query string false "Filter by category"
// @Param        status   query string false "Filter by status"
// @Param        page     query int    false "Page number"
// @Param        limit    query int    false "Results per page"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/kb/articles [get]
func (h *Handler) ListArticles(w http.ResponseWriter, r *http.Request) {
	tenantID := h.requireTenantID(w, r)
	if tenantID == uuid.Nil {
		return
	}
	p := pagination.Parse(r)
	filter := ArticleFilter{
		TenantID: tenantID,
		Search:   r.URL.Query().Get("search"),
		Category: r.URL.Query().Get("category"),
		Limit:    p.Limit,
		Offset:   p.Offset,
	}
	if s := r.URL.Query().Get("status"); s != "" {
		filter.Status = ArticleStatus(s)
	}

	result, err := h.service.ListArticles(r.Context(), filter)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(result.Articles, result.Total, p))
}

func (h *Handler) UpdateArticle(w http.ResponseWriter, r *http.Request) {
	tenantID := h.requireTenantID(w, r)
	if tenantID == uuid.Nil {
		return
	}
	articleID, err := uuid.Parse(chi.URLParam(r, "articleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid article ID")
		return
	}

	var body struct {
		Title    *string        `json:"title"`
		Slug     *string        `json:"slug"`
		Content  *string        `json:"content"`
		Excerpt  *string        `json:"excerpt"`
		Category *string        `json:"category"`
		Tags     []string       `json:"tags"`
		Status   *ArticleStatus `json:"status"`
		IsPublic *bool          `json:"is_public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	article, err := h.service.UpdateArticle(r.Context(), tenantID, articleID, UpdateArticleRequest{
		Title:    body.Title,
		Slug:     body.Slug,
		Content:  body.Content,
		Excerpt:  body.Excerpt,
		Category: body.Category,
		Tags:     body.Tags,
		Status:   body.Status,
		IsPublic: body.IsPublic,
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, article)
}

func (h *Handler) DeleteArticle(w http.ResponseWriter, r *http.Request) {
	tenantID := h.requireTenantID(w, r)
	if tenantID == uuid.Nil {
		return
	}
	articleID, err := uuid.Parse(chi.URLParam(r, "articleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid article ID")
		return
	}
	if err := h.service.DeleteArticle(r.Context(), tenantID, articleID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) VoteArticle(w http.ResponseWriter, r *http.Request) {
	articleID, err := uuid.Parse(chi.URLParam(r, "articleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid article ID")
		return
	}
	var body struct {
		Helpful bool `json:"helpful"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.VoteArticle(r.Context(), articleID, body.Helpful); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Public (no-auth) handlers ─────────────────────────────────────────────

// ListPublicArticles lists publicly visible KB articles (no auth).
// @Summary      List public KB articles
// @Description  List publicly visible knowledge base articles — no authentication required
// @Tags         kb-public
// @Produce      json
// @Param        tenant   path  string false "Tenant slug"
// @Param        search   query string false "Search text"
// @Param        category query string false "Filter by category"
// @Param        page     query int    false "Page number"
// @Param        limit    query int    false "Results per page"
// @Success      200 {object} map[string]interface{}
// @Router       /{tenant}/kb/articles [get]
func (h *Handler) ListPublicArticles(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "tenant")
	tenantID := tenantIDFromSlug(slug)
	p := pagination.Parse(r)
	isPublic := true
	filter := ArticleFilter{
		TenantID: tenantID,
		Status:   StatusPublished,
		IsPublic: &isPublic,
		Search:   r.URL.Query().Get("search"),
		Category: r.URL.Query().Get("category"),
		Limit:    p.Limit,
		Offset:   p.Offset,
	}
	result, err := h.service.ListArticles(r.Context(), filter)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(result.Articles, result.Total, p))
}

// GetPublicArticleBySlug retrieves a public KB article by its slug (no auth).
// @Summary      Get public KB article by slug
// @Description  Retrieve a publicly visible knowledge base article by slug
// @Tags         kb-public
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        slug   path string true "Article slug"
// @Success      200 {object} Article
// @Failure      404 {object} map[string]string
// @Router       /{tenant}/kb/articles/{slug} [get]
func (h *Handler) GetPublicArticleBySlug(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "tenant")
	tenantID := tenantIDFromSlug(slug)
	articleSlug := chi.URLParam(r, "slug")
	article, err := h.service.GetArticleBySlug(r.Context(), tenantID, articleSlug)
	if err != nil {
		h.handleError(w, err)
		return
	}
	if !article.IsPublic {
		respondError(w, http.StatusNotFound, "article not found")
		return
	}
	respondJSON(w, http.StatusOK, article)
}

func (h *Handler) VotePublicArticle(w http.ResponseWriter, r *http.Request) {
	articleID, err := uuid.Parse(chi.URLParam(r, "articleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid article ID")
		return
	}
	var body struct {
		Helpful bool `json:"helpful"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.VoteArticle(r.Context(), articleID, body.Helpful); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Helpers ──────────────────────────────────────────────────────────────

func (h *Handler) requireTenantID(w http.ResponseWriter, r *http.Request) uuid.UUID {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return uuid.Nil
	}
	tenantID, err := claims.TenantUUID()
	if err != nil || tenantID == nil {
		respondError(w, http.StatusBadRequest, "tenant ID required")
		return uuid.Nil
	}
	return *tenantID
}

func tenantIDFromSlug(slug string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(slug))
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrArticleNotFound):
		respondError(w, http.StatusNotFound, "article not found")
	case errors.Is(err, ErrSlugConflict):
		respondError(w, http.StatusConflict, "article slug already exists")
	default:
		h.logger.Error("kb handler error", zap.Error(err))
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
