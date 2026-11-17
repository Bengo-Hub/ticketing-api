# Sprint 5: Knowledge Base

**Status: Written 2026-11-17, not yet built or deployed.** Article management, the public reader, votes, seeded article sets, keyset paging, the category list and the public suggestions for the support form are written. Ranked full-text search is still open. The public routes are described in [integrations.md](../integrations.md#public-routes).

## Tasks

- [x] Staff routes under `/api/v1/{tenant}/kb/admin/articles`: create, list (`search`, `category`, `status`), get, update, delete (`ticketing.kb.*`)
- [x] Public routes: `GET /api/v1/{tenant}/kb/articles`, `GET /kb/articles/{slug}`, `POST /kb/articles/{articleId}/vote`
- [x] Slug unique per tenant; status `draft`, `published` or `archived`
- [x] View count and votes updated scoped by tenant (4f03d8e)
- [x] Seeded article sets from `cmd/seed/kb/<set>/*.json`, checked by `kb_test.go`
- [x] Keyset paging on the staff and public lists with github.com/Bengo-Hub/pagination cursors, newest first, with a `(tenant_id, created_at, id)` index; offset paging and totals are gone (written)
- [x] Public article shape without tenant, author, status, visibility or metadata, and no body in lists (written)
- [x] `GET /api/v1/{tenant}/kb/categories`: the categories of the published public articles with counts (written)
- [x] `GET /api/v1/{tenant}/kb/suggestions?q=`: up to five published public articles for a ticket subject, a plain search with no AI and no credits (written)
- [x] Per tenant and IP rate limits on every public KB route, and 404 instead of 500 for an unknown id or slug (written)
- [x] Article suggestions for a ticket with AI (sprint 13)
- [ ] Ranked full-text search. Search is a trigram-indexed ILIKE on title, excerpt and content; the suggestions rank by matched words, the list by date

## Acceptance criteria

- A public reader sees only published, public articles of the tenant in the URL: list, article, categories, suggestions and votes. Covered by `public_test.go` and `public_list_test.go`, including a repository that ignores every filter, which still leaks nothing.
- Walking the public list with `next_cursor` returns every visible article once, newest first. Covered by `public_list_test.go`.
- The suggestions never call marketflow-ai and never spend credits. The route has no AI client to call.
- Search finds an article by a word in its body with ranking. Partly met: the suggestions rank by matched words; the list does not rank.
