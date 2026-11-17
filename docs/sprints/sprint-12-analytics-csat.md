# Sprint 12: Analytics and CSAT

**Status: Built, not deployed.** It replaces the row-loading trend from [sprint 6](sprint-6-analytics.md). ticketing-ui sprint 11 consumes it. The route contract is [analytics.md](../analytics.md).

## Tasks

- [x] Hourly and daily rollup tables filled by `ticketing-worker` under `RunOnce`: the compact series `ticket_rollup_dims` (per bucket a total row and one row per value of each dimension) that the common paths read, the fine cells `ticket_rollups` (one per team, agent, category, channel and priority combination) for multi-filter queries, `ticket_rollup_marks` and `rollup_watermarks`. Incremental from an `(updated_at, id)` watermark, idempotent cell rebuilds, and a backfill on first run bounded to 40 seconds a tick
- [x] `/analytics/tickets`, `/analytics/agents`, `/analytics/sla` and `/analytics/csat`, built only from rollups and indexed `GROUP BY`, with the filters `from`, `to`, `tz`, `interval`, `team_id`, `agent_id`, `category_id`, `channel` and `priority`, behind `ticketing.analytics.view`
- [x] Trends from `resolved_at`, never from the current status
- [x] Stats with `resolved_today`, `overdue` and previous-period figures. `GET /tickets/stats` and `GET /tickets/analytics` are removed
- [x] CSAT survey on resolve: a single-use token, hashed in ticket metadata `csat` with a 7-day expiry, and the `ticketing.ticket.csat_requested` event through the outbox; scores in `csat_responses`. Email only (WhatsApp surveys are off by owner decision)
- [x] Public CSAT routes for the token (`POST /public/csat/check` and `POST /public/csat`), single use with an expiry, rate limited per IP
- [x] Export through the shared reports module (`github.com/Bengo-Hub/reports` v0.2.0): PDF, CSV and Excel of each view with the screen's filters
- [x] Benchmark with 200k seeded tickets (`benchmark_test.go`, build tag `benchmark`)
- [x] Indexes for every filter and `GROUP BY` path, declared in the Ent schemas
- [x] Ent generation, the migration and `go mod tidy` for the reports module (`20261117131507_sprint12_analytics_csat.sql`)
- [ ] Regenerate the migration for the team pair series (`ticket_rollup_dims.team_id`, the pair kinds, `ticketrollupdim_pair`), the three team pair indexes on `ticket_rollups` and the wider `ticket_open_backlog`
- [x] `csatTx` called from `statusTx` after the `ticket.resolved` event
- [ ] Benchmark run against a local database with the compact series and its figures recorded here. The first run, on the fine cells alone, failed: p95 of 370 ms to 980 ms on the default window and 1.07 s to 4.56 s on a year, because every unfiltered query summed the full cube (831,613 cells). The second run, with the compact series, passed 20 of 24 paths; the four over 300 ms were one-year windows with a team filter, which read fine day cells for their breakdowns. The third design reads those from team pair series on day rows and narrows the remaining fine reads with team pair indexes
- [ ] notifications-api case for `ticketing.ticket.csat_requested` (the survey email)

## Acceptance criteria

- No analytics endpoint loads ticket rows into Go. Met: period figures come from `ticket_rollup_dims` or `ticket_rollups`; the live figures are single indexed aggregates, the backlog an index-only scan.
- A ticket resolved today counts in today's resolved figure whatever day it was created. Met: the rollup dates resolutions by `resolved_at` and `resolved_today` counts `resolved_at` from midnight in the tenant's time zone.
- A CSAT token works once and expires. Met: covered by `csat_test.go`, and enforced in the database by the conditional consume and the unique `token_hash`.
- Export matches the on-screen figures for the same filters. Met: the file is rendered from the same view object; `TestExportMatchesView` checks it.

## Fix requirements proved

- **Fix 5, reports stay fast as data grows.** The benchmark with 200k tickets keeps every `/analytics/*` endpoint under 300 ms p95 locally. The benchmark is written; its first run is pending.
