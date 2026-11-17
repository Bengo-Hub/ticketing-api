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

// ArticleFilter narrows an article list. Lists are keyset paged, newest first: After is the
// position of the last row of the previous page (nil for the first page) and Limit the page size.
type ArticleFilter struct {
	TenantID uuid.UUID
	Status   ArticleStatus
	Category string
	IsPublic *bool
	Search   string
	After    *Cursor
	Limit    int
}

// ArticleList is one keyset page. There is no total; the next page is asked for with NextCursor
// while HasMore is true.
type ArticleList struct {
	Articles   []*Article
	NextCursor string
	HasMore    bool
}

// PublicArticle is all the public routes show of an article. Tenant, author, status, visibility
// and metadata (an AI draft records the ticket it came from) never leave through them. Content is
// set only on the article page, not in lists or suggestions.
type PublicArticle struct {
	ID              uuid.UUID  `json:"id"`
	Slug            string     `json:"slug"`
	Title           string     `json:"title"`
	Excerpt         string     `json:"excerpt"`
	Category        string     `json:"category"`
	Tags            []string   `json:"tags"`
	Content         string     `json:"content,omitempty"`
	ViewCount       int        `json:"view_count"`
	HelpfulCount    int        `json:"helpful_count"`
	NotHelpfulCount int        `json:"not_helpful_count"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ToPublic is the public view of a, with the body only when withContent is set.
func ToPublic(a *Article, withContent bool) PublicArticle {
	p := PublicArticle{
		ID:              a.ID,
		Slug:            a.Slug,
		Title:           a.Title,
		Excerpt:         a.Excerpt,
		Category:        a.Category,
		Tags:            a.Tags,
		ViewCount:       a.ViewCount,
		HelpfulCount:    a.HelpfulCount,
		NotHelpfulCount: a.NotHelpfulCount,
		PublishedAt:     a.PublishedAt,
		UpdatedAt:       a.UpdatedAt,
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if withContent {
		p.Content = a.Content
	}
	return p
}

// CategoryCount is one category of the public knowledge base and how many articles it holds.
type CategoryCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
