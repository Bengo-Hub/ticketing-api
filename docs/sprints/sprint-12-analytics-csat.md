# Sprint 12: Analytics and CSAT

**Status: Pending.** It replaces the row-loading trend from [sprint 6](sprint-6-analytics.md). ticketing-ui sprint 11 consumes it.

## Tasks

- [ ] Hourly and daily rollup tables filled by `ticketing-worker` under `RunOnce`
- [ ] `/analytics/tickets`, `/analytics/agents`, `/analytics/sla` and `/analytics/csat`, built only from rollups and indexed `GROUP BY`
- [ ] Trends from `resolved_at`, never from the current status
- [ ] Stats with `resolved_today`, `overdue` and previous-period figures
- [ ] CSAT survey sent on resolve over the ticket's channel; survey token in ticket metadata; scores in a `csat_responses` table
- [ ] Public CSAT submit route for the token, single use with an expiry
- [ ] Export through the shared reports module, not hand-written CSV
- [ ] Benchmark with 200k seeded tickets

## Acceptance criteria

- No analytics endpoint loads ticket rows into Go.
- A ticket resolved today counts in today's resolved figure whatever day it was created.
- A CSAT token works once and expires.
- Export matches the on-screen figures for the same filters.

## Fix requirements proved

- **Fix 5, reports stay fast as data grows.** The benchmark with 200k tickets keeps every `/analytics/*` endpoint under 300 ms p95 locally.
