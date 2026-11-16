package kb

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Service struct {
	repo Repository
	log  *zap.Logger
}

func NewService(repo Repository, log *zap.Logger) *Service {
	return &Service{repo: repo, log: log}
}

func (s *Service) CreateArticle(ctx context.Context, req CreateArticleRequest) (*Article, error) {
	if req.Slug == "" {
		req.Slug = slugify(req.Title)
	}
	return s.repo.Create(ctx, req)
}

func (s *Service) GetArticle(ctx context.Context, tenantID, id uuid.UUID) (*Article, error) {
	return s.repo.GetByID(ctx, tenantID, id)
}

// GetArticleBySlug serves the public KB page: only a public, published article of the tenant is
// found (a draft or archived article, or one marked private, is not found) and only then counted.
func (s *Service) GetArticleBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*Article, error) {
	article, err := s.repo.GetBySlug(ctx, tenantID, slug)
	if err != nil {
		return nil, err
	}
	if !publiclyVisible(article) {
		return nil, ErrArticleNotFound
	}
	// Fire-and-forget view count increment
	go func() {
		if ierr := s.repo.IncrementViewCount(context.Background(), tenantID, article.ID); ierr != nil {
			s.log.Warn("failed to increment view count", zap.Error(ierr))
		}
	}()
	return article, nil
}

func (s *Service) UpdateArticle(ctx context.Context, tenantID, id uuid.UUID, req UpdateArticleRequest) (*Article, error) {
	return s.repo.Update(ctx, tenantID, id, req)
}

func (s *Service) DeleteArticle(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.repo.Delete(ctx, tenantID, id)
}

func (s *Service) ListArticles(ctx context.Context, filter ArticleFilter) (*ArticleList, error) {
	return s.repo.List(ctx, filter)
}

// VoteArticle records a public "was this helpful" vote, only on a public, published article of
// the tenant in the path.
func (s *Service) VoteArticle(ctx context.Context, tenantID, id uuid.UUID, helpful bool) error {
	article, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if !publiclyVisible(article) {
		return ErrArticleNotFound
	}
	return s.repo.Vote(ctx, tenantID, id, helpful)
}

// publiclyVisible is what the public routes may show: public and published.
func publiclyVisible(a *Article) bool {
	return a != nil && a.IsPublic && a.Status == StatusPublished
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
