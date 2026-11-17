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
	// List returns up to filter.Limit+1 rows, newest first, after filter.After.
	List(ctx context.Context, filter ArticleFilter) ([]*Article, error)
	// Categories counts the filtered articles per non-empty category, by name.
	Categories(ctx context.Context, filter ArticleFilter) ([]CategoryCount, error)
	// Suggest ranks the filtered articles by how many of words their title, excerpt and body
	// contain (title first), at most limit rows. It is plain SQL search, never a model call.
	Suggest(ctx context.Context, filter ArticleFilter, words []string, limit int) ([]*Article, error)
	IncrementViewCount(ctx context.Context, tenantID, id uuid.UUID) error
	Vote(ctx context.Context, tenantID, id uuid.UUID, helpful bool) error
}
