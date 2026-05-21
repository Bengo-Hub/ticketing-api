# Sprint 5 — Knowledge Base

**Duration**: Week 6  
**Status**: 🔲 Pending

---

## Overview

Implements the knowledge base module: articles with rich content, tagging, categorization, search, and AI-powered article suggestions shown to agents when they open a ticket.

---

## Deliverables

### 1. Knowledge Article API

```
POST   /api/v1/{tenant}/kb/articles           create article
GET    /api/v1/{tenant}/kb/articles           list (search, tag, category filters + pagination)
GET    /api/v1/{tenant}/kb/articles/{id}      get article
PUT    /api/v1/{tenant}/kb/articles/{id}      update
DELETE /api/v1/{tenant}/kb/articles/{id}      archive
POST   /api/v1/{tenant}/kb/articles/{id}/publish   publish draft
```

### 2. Public Knowledge Base (no auth)

```
GET /api/v1/{tenant}/public/kb/articles        list published articles
GET /api/v1/{tenant}/public/kb/articles/{id}   get article (increments view count)
GET /api/v1/{tenant}/public/kb/search?q=...    full-text search
```

### 3. Article Schema

```go
type KBArticle struct {
    ID          uuid.UUID
    TenantID    uuid.UUID
    CategoryID  *uuid.UUID
    AuthorID    uuid.UUID        // agent user_id
    Title       string
    Slug        string           // URL-safe, unique per tenant
    Content     string           // markdown/HTML
    Summary     string
    Tags        []string
    Status      ArticleStatus    // draft, published, archived
    ViewCount   int
    HelpfulVotes int
    CreatedAt   time.Time
    UpdatedAt   time.Time
    PublishedAt *time.Time
}
```

### 4. AI Article Suggestions
- `GET /api/v1/{tenant}/tickets/{ticketId}/kb-suggestions` — returns top 3 matching articles
- Uses keyword matching against ticket subject + description initially
- Later: vector similarity via marketflow-ai embedding endpoint

### 5. Article Feedback
- `POST /api/v1/{tenant}/public/kb/articles/{id}/feedback` — customer marks helpful/not helpful

---

## Ent Schema to Add

```
internal/ent/schema/kbarticle.go
```

Fields: `id`, `tenant_id`, `category_id`, `author_id`, `title`, `slug`, `content`, `summary`, `tags`, `status`, `view_count`, `helpful_votes`, `created_at`, `updated_at`, `published_at`.

---

## Definition of Done

- [ ] KB article CRUD for agents
- [ ] Public read-only article API
- [ ] Full-text search (PostgreSQL tsvector)
- [ ] Article suggestions on ticket view
- [ ] Atlas migration for kb_articles table

---

## Next Sprint

**Sprint 6**: Analytics & Reporting — ticket stats, agent performance, SLA compliance, CSAT.
