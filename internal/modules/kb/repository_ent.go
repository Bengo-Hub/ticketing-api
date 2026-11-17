package kb

import (
	"context"
	"sort"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	pagination "github.com/Bengo-Hub/pagination"
	"github.com/bengobox/ticketing-service/internal/ent"
	entpkg "github.com/bengobox/ticketing-service/internal/ent/kbarticle"
	"github.com/bengobox/ticketing-service/internal/ent/predicate"
	"github.com/google/uuid"
)

// articleSearch matches title, excerpt and content by ILIKE through their trigram indexes
// (kbarticle_*_trgm). A term under three characters, which a trigram index cannot serve,
// matches the start of the title or excerpt instead.
func articleSearch(term string) predicate.KBArticle {
	pattern, cols := pagination.ContainsPattern(term), []string{entpkg.FieldTitle, entpkg.FieldExcerpt, entpkg.FieldContent}
	if !pagination.MeetsMinLength(term, 3) {
		pattern, cols = pagination.EscapeLike(term)+"%", []string{entpkg.FieldTitle, entpkg.FieldExcerpt}
	}
	return func(s *entsql.Selector) {
		preds := make([]*entsql.Predicate, len(cols))
		for i, col := range cols {
			c := s.C(col)
			preds[i] = entsql.P(func(b *entsql.Builder) {
				b.WriteString(c).WriteString(" ILIKE ").Arg(pattern)
			})
		}
		s.Where(entsql.Or(preds...))
	}
}

type EntRepository struct {
	client *ent.Client
}

func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client}
}

func toArticle(row *ent.KBArticle) *Article {
	a := &Article{
		ID:              row.ID,
		TenantID:        row.TenantID,
		AuthorID:        row.AuthorID,
		Title:           row.Title,
		Slug:            row.Slug,
		Content:         row.Content,
		Excerpt:         row.Excerpt,
		Category:        row.Category,
		Tags:            row.Tags,
		Status:          ArticleStatus(row.Status),
		IsPublic:        row.IsPublic,
		ViewCount:       row.ViewCount,
		HelpfulCount:    row.HelpfulCount,
		NotHelpfulCount: row.NotHelpfulCount,
		Metadata:        row.Metadata,
		PublishedAt:     row.PublishedAt,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
	if row.Tags == nil {
		a.Tags = []string{}
	}
	return a
}

func (r *EntRepository) Create(ctx context.Context, req CreateArticleRequest) (*Article, error) {
	q := r.client.KBArticle.Create().
		SetTenantID(req.TenantID).
		SetAuthorID(req.AuthorID).
		SetTitle(req.Title).
		SetSlug(req.Slug).
		SetContent(req.Content).
		SetExcerpt(req.Excerpt).
		SetCategory(req.Category).
		SetStatus(entpkg.Status(req.Status)).
		SetIsPublic(req.IsPublic)

	if len(req.Tags) > 0 {
		q = q.SetTags(req.Tags)
	}
	if req.Metadata != nil {
		q = q.SetMetadata(req.Metadata)
	}
	if req.Status == StatusPublished {
		now := time.Now()
		q = q.SetPublishedAt(now)
	}

	row, err := q.Save(ctx)
	if err != nil {
		return nil, err
	}
	return toArticle(row), nil
}

func (r *EntRepository) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*Article, error) {
	row, err := r.client.KBArticle.Query().
		Where(entpkg.ID(id), entpkg.TenantID(tenantID), entpkg.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return toArticle(row), nil
}

// notFound turns ent's not found into ErrArticleNotFound, so the routes answer 404, not 500.
func notFound(err error) error {
	if ent.IsNotFound(err) {
		return ErrArticleNotFound
	}
	return err
}

func (r *EntRepository) GetBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*Article, error) {
	row, err := r.client.KBArticle.Query().
		Where(entpkg.TenantID(tenantID), entpkg.SlugEQ(slug), entpkg.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return toArticle(row), nil
}

func (r *EntRepository) Update(ctx context.Context, tenantID, id uuid.UUID, req UpdateArticleRequest) (*Article, error) {
	_, err := r.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	u := r.client.KBArticle.UpdateOneID(id)
	if req.Title != nil {
		u = u.SetTitle(*req.Title)
	}
	if req.Slug != nil {
		u = u.SetSlug(*req.Slug)
	}
	if req.Content != nil {
		u = u.SetContent(*req.Content)
	}
	if req.Excerpt != nil {
		u = u.SetExcerpt(*req.Excerpt)
	}
	if req.Category != nil {
		u = u.SetCategory(*req.Category)
	}
	if req.Tags != nil {
		u = u.SetTags(req.Tags)
	}
	if req.IsPublic != nil {
		u = u.SetIsPublic(*req.IsPublic)
	}
	if req.Metadata != nil {
		u = u.SetMetadata(req.Metadata)
	}
	if req.Status != nil {
		u = u.SetStatus(entpkg.Status(*req.Status))
		if *req.Status == StatusPublished {
			now := time.Now()
			u = u.SetPublishedAt(now)
		}
	}

	row, err := u.Save(ctx)
	if err != nil {
		return nil, err
	}
	return toArticle(row), nil
}

func (r *EntRepository) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := r.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	now := time.Now()
	return r.client.KBArticle.UpdateOneID(id).SetDeletedAt(now).Exec(ctx)
}

func (r *EntRepository) List(ctx context.Context, filter ArticleFilter) ([]*Article, error) {
	q := r.client.KBArticle.Query().Where(filterPredicates(filter)...)
	if c := filter.After; c != nil {
		q = q.Where(afterCursor(c))
	}
	rows, err := q.
		Order(ent.Desc(entpkg.FieldCreatedAt), ent.Desc(entpkg.FieldID)).
		Limit(normalLimit(filter.Limit) + 1).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return toArticles(rows), nil
}

// filterPredicates are the conditions of a filter: always the tenant and not deleted, then the
// status, category, visibility and search when set. Every list, count and suggestion uses them,
// so a public call that sets published and public can never reach a draft, a private article or
// another tenant's article.
func filterPredicates(filter ArticleFilter) []predicate.KBArticle {
	preds := []predicate.KBArticle{entpkg.TenantID(filter.TenantID), entpkg.DeletedAtIsNil()}
	if filter.Status != "" {
		preds = append(preds, entpkg.StatusEQ(entpkg.Status(filter.Status)))
	}
	if filter.Category != "" {
		preds = append(preds, entpkg.CategoryEQ(filter.Category))
	}
	if filter.IsPublic != nil {
		preds = append(preds, entpkg.IsPublicEQ(*filter.IsPublic))
	}
	if term := pagination.NormalizeSearch(filter.Search); term != "" {
		preds = append(preds, articleSearch(term))
	}
	return preds
}

// afterCursor is the keyset condition (created_at, id) < cursor, one row comparison so the
// (tenant_id, created_at, id) index serves it.
func afterCursor(c *Cursor) predicate.KBArticle {
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("(").WriteString(s.C(entpkg.FieldCreatedAt)).WriteString(", ").WriteString(s.C(entpkg.FieldID)).
				WriteString(") < (").Arg(c.CreatedAt).WriteString(", ").Arg(c.ID).WriteString(")")
		}))
	}
}

func toArticles(rows []*ent.KBArticle) []*Article {
	out := make([]*Article, len(rows))
	for i, row := range rows {
		out[i] = toArticle(row)
	}
	return out
}

func (r *EntRepository) Categories(ctx context.Context, filter ArticleFilter) ([]CategoryCount, error) {
	var rows []struct {
		Category string `json:"category"`
		Count    int    `json:"count"`
	}
	preds := append(filterPredicates(filter), entpkg.CategoryNotNil(), entpkg.CategoryNEQ(""))
	if err := r.client.KBArticle.Query().Where(preds...).
		GroupBy(entpkg.FieldCategory).
		Aggregate(ent.Count()).
		Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]CategoryCount, len(rows))
	for i, row := range rows {
		out[i] = CategoryCount{Name: row.Category, Count: row.Count}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// suggestColumns are the columns a suggestion word is searched in; rankSuggestions weighs a word
// found in the title 3, the excerpt 2 and the body 1.
var suggestColumns = []string{entpkg.FieldTitle, entpkg.FieldExcerpt, entpkg.FieldContent}

// suggestCandidates caps the matching articles read for one suggestion request. They are ranked
// in Go (rankSuggestions), because an ORDER BY expression built with ent's OrderExprFunc numbers
// its placeholders from $1 again and collides with the WHERE arguments.
const suggestCandidates = 50

// suggestMatch keeps articles with at least one of words in the title, excerpt or body, each an
// ILIKE served by the trigram indexes (every word has at least three characters).
func suggestMatch(words []string) predicate.KBArticle {
	return func(s *entsql.Selector) {
		var preds []*entsql.Predicate
		for _, w := range words {
			pattern := pagination.ContainsPattern(w)
			for _, name := range suggestColumns {
				col := s.C(name)
				preds = append(preds, entsql.P(func(b *entsql.Builder) {
					b.WriteString(col).WriteString(" ILIKE ").Arg(pattern)
				}))
			}
		}
		s.Where(entsql.Or(preds...))
	}
}

// suggestScore weighs each word found in an article: 3 in the title, 2 in the excerpt, 1 in the
// body, case-insensitive, matching the ILIKE contains test of suggestMatch.
func suggestScore(a *Article, words []string) int {
	title, excerpt, content := strings.ToLower(a.Title), strings.ToLower(a.Excerpt), strings.ToLower(a.Content)
	score := 0
	for _, w := range words {
		w = strings.ToLower(w)
		if strings.Contains(title, w) {
			score += 3
		}
		if strings.Contains(excerpt, w) {
			score += 2
		}
		if strings.Contains(content, w) {
			score += 1
		}
	}
	return score
}

// rankSuggestions orders candidates by score, keeping the database order (helpful votes, then
// newest) between equal scores, and returns the first limit.
func rankSuggestions(candidates []*Article, words []string, limit int) []*Article {
	scores := make(map[*Article]int, len(candidates))
	for _, a := range candidates {
		scores[a] = suggestScore(a, words)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return scores[candidates[i]] > scores[candidates[j]] })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func (r *EntRepository) Suggest(ctx context.Context, filter ArticleFilter, words []string, limit int) ([]*Article, error) {
	if len(words) == 0 || limit <= 0 {
		return []*Article{}, nil
	}
	rows, err := r.client.KBArticle.Query().
		Where(append(filterPredicates(filter), suggestMatch(words))...).
		Order(entpkg.ByHelpfulCount(entsql.OrderDesc()), entpkg.ByCreatedAt(entsql.OrderDesc()), entpkg.ByID(entsql.OrderDesc())).
		Limit(suggestCandidates).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return rankSuggestions(toArticles(rows), words, limit), nil
}

func (r *EntRepository) IncrementViewCount(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := r.client.KBArticle.Update().
		Where(entpkg.ID(id), entpkg.TenantID(tenantID)).
		AddViewCount(1).
		Save(ctx)
	return err
}

func (r *EntRepository) Vote(ctx context.Context, tenantID, id uuid.UUID, helpful bool) error {
	u := r.client.KBArticle.Update().Where(entpkg.ID(id), entpkg.TenantID(tenantID))
	if helpful {
		u = u.AddHelpfulCount(1)
	} else {
		u = u.AddNotHelpfulCount(1)
	}
	n, err := u.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrArticleNotFound
	}
	return nil
}
