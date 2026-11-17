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
	// JSON tags match ticketing-ui's KBArticle. Without them every KB route sent Go field names
	// (ID, Title, IsPublic) and the admin KB pages read empty fields (found 2026-11-17).
	ID              uuid.UUID      `json:"id"`
	TenantID        uuid.UUID      `json:"tenant_id"`
	AuthorID        uuid.UUID      `json:"author_id"`
	Title           string         `json:"title"`
	Slug            string         `json:"slug"`
	Content         string         `json:"content"`
	Excerpt         string         `json:"excerpt"`
	Category        string         `json:"category"`
	Tags            []string       `json:"tags"`
	Status          ArticleStatus  `json:"status"`
	IsPublic        bool           `json:"is_public"`
	ViewCount       int            `json:"view_count"`
	HelpfulCount    int            `json:"helpful_count"`
	NotHelpfulCount int            `json:"not_helpful_count"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	PublishedAt     *time.Time     `json:"published_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
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
