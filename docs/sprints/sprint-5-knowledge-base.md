# Sprint 5: Knowledge Base

**Status: Partial.** Article management, the public reader, votes and seeded article sets are built. Real search, a category list and suggestions are missing.

## Tasks

- [x] Staff routes under `/api/v1/{tenant}/kb/admin/articles`: create, list (`search`, `category`, `status`), get, update, delete (`ticketing.kb.*`)
- [x] Public routes: `GET /api/v1/{tenant}/kb/articles`, `GET /kb/articles/{slug}`, `POST /kb/articles/{articleId}/vote`
- [x] Slug unique per tenant; status `draft`, `published` or `archived`
- [x] View count and votes updated scoped by tenant (4f03d8e)
- [x] Seeded article sets from `cmd/seed/kb/<set>/*.json`, checked by `kb_test.go`
- [ ] Full-text search. Search today is a case-insensitive substring match on title, excerpt and content (sprint 9)
- [ ] A KB category list endpoint; `category` is free text on each article
- [ ] Article suggestions for a ticket (sprint 13)

## Acceptance criteria

- A public reader sees only published, public articles of the tenant in the URL. Met, covered by `public_test.go`.
- Search finds an article by a word in its body with ranking. Not met, sprint 9.
