# Sprint 6: Analytics and Reporting

**Status: Done, finished by [sprint 12](sprint-12-analytics-csat.md).** Sprint 6 built status and priority counts and a created and resolved trend. Sprint 12 replaced them with rollup-backed routes; the contract is [analytics.md](../analytics.md).

## Tasks

- [x] `GET /api/v1/{tenant}/tickets/stats` returned `{stats}` with counts by status from an indexed `GROUP BY` (4f03d8e). Removed in sprint 12; the live counts are `stats` in `GET /analytics/tickets`
- [x] `GET /tickets/analytics?days=` returned counts by status and priority and a created and resolved trend. Removed in sprint 12
- [x] Trend from `resolved_at` (sprint 12, from rollups)
- [x] `resolved_today`, `overdue` and previous-period figures in the stats (sprint 12)
- [x] Agent performance, SLA compliance and CSAT endpoints (sprint 12)
- [x] CSAT surveys (sprint 12)
- [x] Export (sprint 12, through the shared reports module)
- [x] Rollup tables (sprint 12)

## Acceptance criteria

- Counts never load ticket rows. Met by sprint 12.
- Analytics under 300 ms p95 at 200k tickets. The benchmark is in sprint 12; its first run is pending.
