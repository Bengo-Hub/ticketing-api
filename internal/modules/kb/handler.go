package kb

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	pagination "github.com/Bengo-Hub/pagination"
	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"github.com/bengobox/ticketing-service/internal/platform/access"
	"github.com/bengobox/ticketing-service/internal/platform/tenantcache"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Handler struct {
	service *Service
	tenants TenantLookup
	limiter rateLimiter
	logger  *zap.Logger
}

// rateLimiter enforces per-key request limits (shared-ratelimit's Limiter, as on the public
// tickets routes).
type rateLimiter interface {
	Allow(ctx context.Context, key string, opts ratelimit.Options, cost int) (bool, time.Duration)
}

// SetLimiter wires the limiter the public routes use. Without one only the global per-IP limit
// applies.
func (h *Handler) SetLimiter(l rateLimiter) { h.limiter = l }

// Public route limits, per tenant and client IP, on top of the router's global per-IP limit.
var (
	publicReadLimit    = ratelimit.Options{Name: "public-kb-read", Limit: 120, Window: time.Minute}
	publicSuggestLimit = ratelimit.Options{Name: "public-kb-suggest", Limit: 60, Window: time.Minute}
	publicVoteLimit    = ratelimit.Options{Name: "public-kb-vote", Limit: 30, Window: time.Hour}
	// One per page view from the visitor's browser; generous for a reader, tight for a counter
	// inflater.
	publicViewLimit = ratelimit.Options{Name: "public-kb-view", Limit: 60, Window: time.Minute}
)

// TenantLookup resolves a tenant slug to auth-api's tenant (tenantcache.Resolver: Redis, then
// auth-api). It is the shared tenantcache.Lookup, also used by the public tickets routes.
type TenantLookup = tenantcache.Lookup

// SetTenantLookup wires the slug lookup the public routes need.
func (h *Handler) SetTenantLookup(l TenantLookup) { h.tenants = l }

func NewHandler(service *Service, logger *zap.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger.Named("kb-handler"),
	}
}

// RegisterRoutes mounts staff/admin KB routes under /kb/admin — a distinct
// path from the public /kb mount (see RegisterPublicRoutes) because chi
// cannot Mount() two routers at the identical pattern, and ListArticles here
// has no server-side status filter (it trusts the caller), unlike
// ListPublicArticles which force-filters to published+public. Voting is
// intentionally NOT duplicated here — VotePublicArticle already covers it
// identically (neither reads claims) and this avoided dead reachable code.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/kb/admin", func(kb chi.Router) {
		kb.Route("/articles", func(articles chi.Router) {
			articles.With(access.RequirePermission("ticketing.kb.add")).Post("/", h.CreateArticle)
			articles.With(access.RequirePermission("ticketing.kb.view")).Get("/", h.ListArticles)
			articles.With(access.RequirePermission("ticketing.kb.view")).Get("/{articleId}", h.GetArticle)
			articles.With(access.RequirePermission("ticketing.kb.change")).Put("/{articleId}", h.UpdateArticle)
			articles.With(access.RequirePermission("ticketing.kb.delete")).Delete("/{articleId}", h.DeleteArticle)
		})
	})
}

// RegisterPublicRoutes mounts the public knowledge base (no sign-in, tenant by slug in the path).
// They only ever show the tenant's published, public articles, and are rate limited per tenant
// and IP. Suggestions are a plain search for the public support form, never an AI call.
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Route("/kb", func(kb chi.Router) {
		kb.With(h.limit(publicReadLimit)).Get("/articles", h.ListPublicArticles)
		kb.With(h.limit(publicReadLimit)).Get("/articles/{slug}", h.GetPublicArticleBySlug)
		kb.With(h.limit(publicVoteLimit)).Post("/articles/{articleId}/vote", h.VotePublicArticle)
		kb.With(h.limit(publicViewLimit)).Post("/articles/{articleId}/view", h.RecordPublicArticleView)
		kb.With(h.limit(publicReadLimit)).Get("/categories", h.ListPublicCategories)
		kb.With(h.limit(publicSuggestLimit)).Get("/suggestions", h.SuggestPublicArticles)
	})
}

// limit refuses a request over opts for this tenant and client IP with 429 and Retry-After.
func (h *Handler) limit(opts ratelimit.Options) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h.limiter != nil {
				key := strings.ToLower(chi.URLParam(r, "tenant")) + ":" + ratelimit.ClientIP(r)
				if ok, retry := h.limiter.Allow(r.Context(), key, opts, 1); !ok {
					if secs := int(math.Ceil(retry.Seconds())); secs > 0 {
						w.Header().Set("Retry-After", strconv.Itoa(secs))
					}
					respondError(w, http.StatusTooManyRequests, "rate limit exceeded")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
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
	tenantID, ok := requestTenant(w, r)
	if !ok {
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
	agentID := access.From(r.Context()).AuthUserID

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

// ListArticles lists every article of the tenant (any status and visibility), keyset paged,
// newest first.
// @Summary      List KB articles
// @Description  Staff list of knowledge base articles, keyset paged: pass next_cursor back as cursor while has_more is true
// @Tags         kb
// @Produce      json
// @Param        tenant   path  string true  "Tenant slug"
// @Param        search   query string false "Search text"
// @Param        category query string false "Filter by category"
// @Param        status   query string false "Filter by status"
// @Param        cursor   query string false "next_cursor of the previous page"
// @Param        limit    query int    false "Page size (default 20, at most 100)"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/kb/admin/articles [get]
func (h *Handler) ListArticles(w http.ResponseWriter, r *http.Request) {
	tenantID := h.requireTenantID(w, r)
	if tenantID == uuid.Nil {
		return
	}
	after, limit, err := parsePage(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid cursor")
		return
	}
	filter := ArticleFilter{
		TenantID: tenantID,
		Search:   pagination.SearchTerm(r),
		Category: r.URL.Query().Get("category"),
		After:    after,
		Limit:    limit,
	}
	if s := r.URL.Query().Get("status"); s != "" {
		filter.Status = ArticleStatus(s)
	}

	result, err := h.service.ListArticles(r.Context(), filter)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.CursorResponse[*Article]{
		Data: result.Articles, NextCursor: result.NextCursor, HasMore: result.HasMore,
	})
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

// ─── Public (no-auth) handlers ─────────────────────────────────────────────

// ListPublicArticles lists the tenant's published, public articles (no sign-in), keyset paged.
// @Summary      List public KB articles
// @Description  Published public articles of the tenant, newest first, without the body. Pass next_cursor back as cursor while has_more is true
// @Tags         kb-public
// @Produce      json
// @Param        tenant   path  string true  "Tenant slug"
// @Param        search   query string false "Search text"
// @Param        category query string false "Filter by category"
// @Param        cursor   query string false "next_cursor of the previous page"
// @Param        limit    query int    false "Page size (default 20, at most 100)"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      429 {object} map[string]string
// @Router       /{tenant}/kb/articles [get]
func (h *Handler) ListPublicArticles(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.publicTenant(r)
	if !ok {
		respondError(w, http.StatusNotFound, "tenant not found")
		return
	}
	after, limit, err := parsePage(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid cursor")
		return
	}
	result, err := h.service.ListPublicArticles(r.Context(), tenantID, pagination.SearchTerm(r), r.URL.Query().Get("category"), after, limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.CursorResponse[PublicArticle]{
		Data: publicList(result.Articles, false), NextCursor: result.NextCursor, HasMore: result.HasMore,
	})
}

// GetPublicArticleBySlug returns one published, public article with its body, and counts a view
// unless count_view=false (a cached server-side read whose page counts the view separately).
// @Summary      Get public KB article by slug
// @Description  A published public article of the tenant with its sanitised HTML body. Drafts, archived and private articles are not found. Counts a view unless count_view=false
// @Tags         kb-public
// @Produce      json
// @Param        tenant     path  string true  "Tenant slug"
// @Param        slug       path  string true  "Article slug"
// @Param        count_view query bool   false "false reads without counting a view (default true)"
// @Success      200 {object} PublicArticle
// @Failure      404 {object} map[string]string
// @Failure      429 {object} map[string]string
// @Router       /{tenant}/kb/articles/{slug} [get]
func (h *Handler) GetPublicArticleBySlug(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.publicTenant(r)
	if !ok {
		respondError(w, http.StatusNotFound, "article not found")
		return
	}
	articleSlug := chi.URLParam(r, "slug")
	read := h.service.GetArticleBySlug
	if r.URL.Query().Get("count_view") == "false" {
		read = h.service.ReadArticleBySlug
	}
	article, err := read(r.Context(), tenantID, articleSlug)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, ToPublic(article, true))
}

// ListPublicCategories lists the categories of the tenant's public knowledge base.
// @Summary      List public KB categories
// @Description  Categories of the tenant's published public articles with their article counts, by name
// @Tags         kb-public
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Success      200 {object} map[string]interface{}
// @Failure      404 {object} map[string]string
// @Failure      429 {object} map[string]string
// @Router       /{tenant}/kb/categories [get]
func (h *Handler) ListPublicCategories(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.publicTenant(r)
	if !ok {
		respondError(w, http.StatusNotFound, "tenant not found")
		return
	}
	cats, err := h.service.PublicCategories(r.Context(), tenantID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": cats})
}

// SuggestPublicArticles ranks published public articles for the subject a visitor is typing on
// the public support form. A plain database search: no AI call and no credits.
// @Summary      Suggest public KB articles
// @Description  Up to 5 published public articles matching the words of a ticket subject, best first, without the body. No AI and no credits
// @Tags         kb-public
// @Produce      json
// @Param        tenant path  string true  "Tenant slug"
// @Param        q      query string true  "The subject typed so far"
// @Param        limit  query int    false "At most 5"
// @Success      200 {object} map[string]interface{}
// @Failure      404 {object} map[string]string
// @Failure      429 {object} map[string]string
// @Router       /{tenant}/kb/suggestions [get]
func (h *Handler) SuggestPublicArticles(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.publicTenant(r)
	if !ok {
		respondError(w, http.StatusNotFound, "tenant not found")
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), MaxSuggestions, MaxSuggestions)
	rows, err := h.service.SuggestPublicArticles(r.Context(), tenantID, pagination.SearchTerm(r, "q", "subject", "search"), limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": publicList(rows, false)})
}

func publicList(rows []*Article, withContent bool) []PublicArticle {
	out := make([]PublicArticle, len(rows))
	for i, a := range rows {
		out[i] = ToPublic(a, withContent)
	}
	return out
}

// VotePublicArticle records a "was this helpful" vote on a published, public article.
// @Summary      Vote on a public KB article
// @Description  Counts a helpful or not helpful vote. Drafts, archived and private articles are not found
// @Tags         kb-public
// @Accept       json
// @Param        tenant    path string true "Tenant slug"
// @Param        articleId path string true "Article UUID"
// @Success      204
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      429 {object} map[string]string
// @Router       /{tenant}/kb/articles/{articleId}/vote [post]
func (h *Handler) VotePublicArticle(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.publicTenant(r)
	if !ok {
		respondError(w, http.StatusNotFound, "article not found")
		return
	}
	articleID, err := uuid.Parse(chi.URLParam(r, "articleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid article ID")
		return
	}
	var body struct {
		Helpful bool `json:"helpful"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.VoteArticle(r.Context(), tenantID, articleID, body.Helpful); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RecordPublicArticleView counts one view of a published, public article.
// @Summary      Count a public KB article view
// @Description  The help center page calls this once per page view from the visitor's browser, since the page itself reads the article through a short server-side cache. Drafts, archived and private articles are not found
// @Tags         kb-public
// @Param        tenant    path string true "Tenant slug"
// @Param        articleId path string true "Article UUID"
// @Success      204
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      429 {object} map[string]string
// @Router       /{tenant}/kb/articles/{articleId}/view [post]
func (h *Handler) RecordPublicArticleView(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.publicTenant(r)
	if !ok {
		respondError(w, http.StatusNotFound, "article not found")
		return
	}
	articleID, err := uuid.Parse(chi.URLParam(r, "articleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid article ID")
		return
	}
	if err := h.service.RecordPublicView(r.Context(), tenantID, articleID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Helpers ──────────────────────────────────────────────────────────────

func (h *Handler) requireTenantID(w http.ResponseWriter, r *http.Request) uuid.UUID {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return uuid.Nil
	}
	return *tenantID
}

// requestTenant returns the tenant the access middleware resolved for the request.
func requestTenant(w http.ResponseWriter, r *http.Request) (*uuid.UUID, bool) {
	a := access.From(r.Context())
	if a == nil || a.TenantID == uuid.Nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	id := a.TenantID
	return &id, true
}

// publicTenant resolves the {tenant} path segment (a slug or a UUID) to the tenant's real id.
// Tenant ids are auth-api's own UUIDs, never derived from the slug, so a slug is looked up; an
// unknown one is not found.
func (h *Handler) publicTenant(r *http.Request) (uuid.UUID, bool) {
	return tenantcache.PublicTenantID(r.Context(), h.tenants, chi.URLParam(r, "tenant"))
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
