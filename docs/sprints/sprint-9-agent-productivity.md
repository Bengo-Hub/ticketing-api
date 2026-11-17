# Sprint 9: Agent Productivity

**Status: Pending.** This sprint brings the agent workspace to the baseline every leading help desk shares. ticketing-ui sprint 8 consumes it.

## Tasks

- [x] Keyset pagination (pagination v0.5.0) on every list, ordered by `(tenant_id, created_at, id)`, with indexes on the filter columns. Tickets, comments and attachments since c8e8631b; the agent and team lists written on 2026-11-17 (newest first, `{data, next_cursor, has_more}`, indexes `agent_tenant_id_created_at_id` and `team_tenant_id_created_at_id`; offset paging and totals are gone)
- [x] Ticket list order `sort=sla_due`: the nearest open SLA target first (the response target until the first response, and the resolution target), running clocks before paused ones and tickets with no target last in each group, keyset paged by `(rank, due key, id)` through the `ticket_tenant_sla_due` expression index (written 2026-11-17)
- [x] Comment author names: every comment carries `author_name` (the agent's display name for staff, the channel sender, the ticketing user's name or the ticket's requester name for a customer, "System" for a system comment), resolved with one query per page (written 2026-11-17)
- [x] Tickets carry `sla_policy_name` and `sla_calendar_name` (empty for 24x7), from one query per list page or ticket (written 2026-11-17)
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
- Paging through 200k tickets never uses `OFFSET`, in any order.
- A page of 25 tickets or comments makes one extra query for its names, whatever the number of authors or policies on it. `TestSLADueKeysetWalkAndOneLabelLookupPerPage` and `TestPageCommentsNamesAuthorsInOneLookup` check the lookup count; `TestAgentKeysetWalksEveryAgentOnce` walks the agent list across pages with tied timestamps.
- Two agents on one ticket each see the other's presence within 2 seconds.
- A macro sets status and priority and posts its reply in one request.
- Merging two tickets moves the comments and leaves the merged ticket closed with a link.

## Fix requirements proved

- **Fix 1, no feature behind an add-on.** An e2e test on a tier-1 tenant creates, assigns, comments on, merges and resolves a ticket and applies a macro.
- **Fix 3, no missed duplicate replies.** An e2e test with two agents on one ticket shows the second agent the viewing and typing warning within 2 seconds.
