# Sprint 9: Agent Productivity

**Status: Pending.** This sprint brings the agent workspace to the baseline every leading help desk shares. ticketing-ui sprint 8 consumes it.

## Tasks

- [ ] Keyset pagination (pagination v0.5.0) on every list, ordered by `(tenant_id, created_at, id)`, with indexes on the filter columns
- [ ] Full-text search on tickets and KB articles: a generated `tsvector` column with a GIN index, or `pg_trgm` indexes for partial words, replacing the substring match
- [ ] `ListTickets` reads `search`
- [ ] Views and saved filters per agent, in metadata or a small `ticket_views` table
- [ ] Tags: add, remove and filter on the existing `tags` column
- [ ] Macros and canned replies that set fields and add a reply in one call
- [ ] Merge tickets, and parent-child links through a `related` edge
- [ ] Bulk update of status, priority, assignee and tags
- [ ] Collision presence: viewing and typing over the existing SSE hub relayed by NATS, held in Redis with a short TTL
- [ ] @mentions that fill `ticket_comments.mentions` and publish an event for notifications-api
- [ ] Attachments: upload, signed download URLs and delete on R2 through httpware/objectstore, scoped by tenant
- [ ] Update requests that can clear a field (explicit nulls instead of pointer `omitempty`)
- [ ] Split `tickets/handler.go` and `identity/handler.go`, both over 400 lines

## Acceptance criteria

- An agent finds a ticket by a word in its description, ranked, in under 200 ms at 200k tickets.
- Paging through 200k tickets never uses `OFFSET`.
- Two agents on one ticket each see the other's presence within 2 seconds.
- A macro sets status and priority and posts its reply in one request.
- Merging two tickets moves the comments and leaves the merged ticket closed with a link.

## Fix requirements proved

- **Fix 1, no feature behind an add-on.** An e2e test on a tier-1 tenant creates, assigns, comments on, merges and resolves a ticket and applies a macro.
- **Fix 3, no missed duplicate replies.** An e2e test with two agents on one ticket shows the second agent the viewing and typing warning within 2 seconds.
