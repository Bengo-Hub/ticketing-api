# Sprint 6: Analytics and Reporting

**Status: Partial.** Status and priority counts and a created and resolved trend exist. Agent, SLA and CSAT reports, export and rollups are not built and move to [sprint 12](sprint-12-analytics-csat.md).

## Tasks

- [x] `GET /api/v1/{tenant}/tickets/stats` returns `{stats}` with counts by status from an indexed `GROUP BY` (4f03d8e)
- [x] `GET /tickets/analytics?days=` returns counts by status and priority (`GROUP BY`) and a trend of created and resolved tickets (`ticketing.analytics.view`)
- [ ] Trend from `resolved_at`. Today the trend loads the period's rows into Go and counts a ticket as resolved by its current status in the bucket it was created in.
- [ ] `resolved_today`, `overdue` and previous-period figures in the stats
- [ ] Agent performance, SLA compliance and CSAT endpoints
- [ ] CSAT surveys
- [ ] Export
- [ ] Rollup tables

## Acceptance criteria

- Counts never load ticket rows. Met for counts, not for the trend.
- Analytics under 300 ms p95 at 200k tickets. Not measured, sprint 12.
