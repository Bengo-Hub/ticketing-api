package kb

import (
	"context"

	"github.com/google/uuid"
)

type Repository interface {
	Create(ctx context.Context, req CreateArticleRequest) (*Article, error)
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*Article, error)
	GetBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*Article, error)
	Update(ctx context.Context, tenantID, id uuid.UUID, req UpdateArticleRequest) (*Article, error)
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
	List(ctx context.Context, filter ArticleFilter) (*ArticleList, error)
	IncrementViewCount(ctx context.Context, tenantID, id uuid.UUID) error
	Vote(ctx context.Context, tenantID, id uuid.UUID, helpful bool) error
}
