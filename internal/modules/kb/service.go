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

func (s *Service) GetArticleBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*Article, error) {
	article, err := s.repo.GetBySlug(ctx, tenantID, slug)
	if err != nil {
		return nil, err
	}
	// Fire-and-forget view count increment
	go func() {
		if ierr := s.repo.IncrementViewCount(context.Background(), article.ID); ierr != nil {
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

func (s *Service) VoteArticle(ctx context.Context, id uuid.UUID, helpful bool) error {
	return s.repo.Vote(ctx, id, helpful)
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
