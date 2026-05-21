package kb

import (
	"time"

	"github.com/google/uuid"
)

type ArticleStatus string

const (
	StatusDraft     ArticleStatus = "draft"
	StatusPublished ArticleStatus = "published"
	StatusArchived  ArticleStatus = "archived"
)

type Article struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	AuthorID       uuid.UUID
	Title          string
	Slug           string
	Content        string
	Excerpt        string
	Category       string
	Tags           []string
	Status         ArticleStatus
	IsPublic       bool
	ViewCount      int
	HelpfulCount   int
	NotHelpfulCount int
	Metadata       map[string]any
	PublishedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateArticleRequest struct {
	TenantID uuid.UUID
	AuthorID uuid.UUID
	Title    string
	Slug     string
	Content  string
	Excerpt  string
	Category string
	Tags     []string
	Status   ArticleStatus
	IsPublic bool
	Metadata map[string]any
}

type UpdateArticleRequest struct {
	Title    *string
	Slug     *string
	Content  *string
	Excerpt  *string
	Category *string
	Tags     []string
	Status   *ArticleStatus
	IsPublic *bool
	Metadata map[string]any
}

type ArticleFilter struct {
	TenantID uuid.UUID
	Status   ArticleStatus
	Category string
	IsPublic *bool
	Search   string
	Limit    int
	Offset   int
}

type ArticleList struct {
	Articles []*Article
	Total    int
}
