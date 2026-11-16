package kb

import (
	"context"
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
		return nil, err
	}
	return toArticle(row), nil
}

func (r *EntRepository) GetBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*Article, error) {
	row, err := r.client.KBArticle.Query().
		Where(entpkg.TenantID(tenantID), entpkg.SlugEQ(slug), entpkg.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, err
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

func (r *EntRepository) List(ctx context.Context, filter ArticleFilter) (*ArticleList, error) {
	q := r.client.KBArticle.Query().
		Where(entpkg.TenantID(filter.TenantID), entpkg.DeletedAtIsNil())

	if filter.Status != "" {
		q = q.Where(entpkg.StatusEQ(entpkg.Status(filter.Status)))
	}
	if filter.Category != "" {
		q = q.Where(entpkg.CategoryEQ(filter.Category))
	}
	if filter.IsPublic != nil {
		q = q.Where(entpkg.IsPublicEQ(*filter.IsPublic))
	}
	if term := pagination.NormalizeSearch(filter.Search); term != "" {
		q = q.Where(articleSearch(term))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := q.
		Order(ent.Desc(entpkg.FieldCreatedAt)).
		Limit(limit).
		Offset(filter.Offset).
		All(ctx)
	if err != nil {
		return nil, err
	}

	articles := make([]*Article, len(rows))
	for i, row := range rows {
		articles[i] = toArticle(row)
	}
	return &ArticleList{Articles: articles, Total: total}, nil
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
