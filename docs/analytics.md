# Ticketing Analytics and CSAT

This page is the contract for the analytics routes, their exports and the CSAT survey routes. ticketing-ui codes against the shapes below. Design detail is in [architecture.md](architecture.md), the tables in [erd.md](erd.md) and the survey event in [integrations.md](integrations.md).

## How the figures are made

`ticketing-worker` fills two rollups every minute under `cache.RunOnce` (key `ticketing:analytics-rollup`), each by hour and by UTC day. `ticket_rollup_dims` holds per bucket one total row and one row per value of each dimension (team, agent, category, channel, priority), and on day rows one row per team and value of each other dimension; a view with no filter, one filter, or over 31 days a team filter with at most one other filter, reads mostly these rows. `ticket_rollups` holds the fine cells, one per combination of all five dimensions; a view reads them only for the breakdowns those rows cannot answer, always under a filter. Both give the same figures; the analytics routes read period figures only from them. The live figures (the open backlog, overdue, unassigned, created today and resolved today) are counted on `tickets` with one indexed aggregate each. No route loads ticket rows.

Each figure is dated by its own moment:

| Figure | Dated by |
|---|---|
| `created` | `created_at` |
| `resolved`, `avg_resolution_minutes`, resolution SLA met | `resolved_at`, so a ticket resolved today counts today whatever day it was created |
| `first_responses`, `avg_first_response_minutes`, first response SLA met | `first_response_at` |
| first response SLA breached | the first response deadline, when it was breached |
| resolution SLA breached | the resolution deadline, when it was breached |
| CSAT | the response's `submitted_at` |

A ticket counts once per figure, at its latest moment: a ticket resolved again after a reopen counts at its last resolution. Team, agent, category, channel and priority are the ticket's current values. Average times are wall-clock minutes from creation. A target counts as met when it was met with a deadline set and no breach, and as breached at its deadline; tickets still inside their deadline count in neither. A satisfied CSAT response scores 4 or 5.

The rollup reads ticket changes about two minutes behind real time, so the newest changes show in period figures a few minutes late. The live figures have no delay. `period.rollups_as_of` says how far the rollup has read, and `period.backfill_complete` is false until the first backfill after deploy has finished.

## Query parameters

Every route takes the same parameters.

| Parameter | Value |
|---|---|
| `from`, `to` | RFC 3339 time or `YYYY-MM-DD` date in `tz`. A date as `to` includes that whole day. Default: today and the 29 days before it, up to now. At most 367 days. |
| `tz` | IANA time zone for day boundaries, today and trend buckets. Default: the tenant's time zone from auth-api, else `UTC`. |
| `interval` | `hour`, `day`, `week` (weeks start on Monday) or `month`. Default: `hour` up to 2 days, `day` up to 92 days, `week` beyond. `hour` needs a window of 31 days or less. |
| `team_id`, `agent_id`, `category_id` | UUID |
| `channel` | `web`, `email`, `api`, `chat`, `whatsapp`, `phone`, `internal` |
| `priority` | `low`, `normal`, `high`, `urgent`, `critical` |

Windows of 31 days or less read hour cells and are widened to whole hours. Longer windows read UTC-day cells and are widened to whole UTC days, so in a time zone ahead of UTC a long window's day boundaries fall at the UTC midnight. The previous period is the window of the same length that ends where this one starts. A bad parameter answers 400 with `{"error": "analytics: invalid filter: <reason>"}`.

All routes need `ticketing.analytics.view`.

## Shared objects

**`period`**

| Key | Type | Meaning |
|---|---|---|
| `from`, `to` | RFC 3339 | The effective window after widening, `to` exclusive |
| `previous_from`, `previous_to` | RFC 3339 | The previous period |
| `tz` | string | Time zone used |
| `interval` | string | Trend interval used |
| `source` | `hour` or `day` | Which cells were read |
| `rollups_as_of` | RFC 3339 or null | Ticket changes before this are in the cells |
| `backfill_complete` | bool | False until the first backfill finished |

**`filters`**: `team_id`, `agent_id`, `category_id`, `channel`, `priority`, each the applied value or null.

**`metrics`**

| Key | Type |
|---|---|
| `created`, `resolved`, `first_responses` | integer |
| `avg_first_response_minutes`, `avg_resolution_minutes` | number (one decimal) or null |
| `first_response_sla`, `resolution_sla` | `{"met": int, "breached": int, "compliance_pct": number or null}` |
| `csat` | `{"responses": int, "average": number (two decimals) or null, "satisfied_pct": number or null, "scores": {"1": int, "2": int, "3": int, "4": int, "5": int}}` |

**`change`** compares the period with the previous one. `*_pct` keys are the relative change in percent, null when the previous figure is zero or missing. `*_delta` keys are a plain difference: percentage points for compliance and satisfied share, points on the 1 to 5 scale for the CSAT average.

```json
{
  "created_pct": 12.5, "resolved_pct": -3.1,
  "avg_first_response_pct": -8.0, "avg_resolution_pct": null,
  "first_response_compliance_delta": 2.4, "resolution_compliance_delta": -1.0,
  "csat_responses_pct": 20.0, "csat_average_delta": 0.15, "csat_satisfied_delta": 3.2
}
```

**trend point**: `{"bucket": "2026-11-17T00:00:00+03:00", ...metrics}`. `bucket` is the bucket start in `tz`. Every bucket of the window is present, empty ones with zero counts.

**group**: `{"key": "...", "name": "...", "email": "...", ...metrics}`. `key` is the team, agent or category UUID, the channel or the priority. The empty key `""` is the tickets with no team, agent or category, named `Unassigned` or `No category`. `email` is set on agent groups only.

## GET /api/v1/{tenant}/analytics/tickets

```json
{
  "period": {"...": "period"},
  "filters": {"...": "filters"},
  "stats": {
    "open": 42,
    "by_status": {"new": 5, "open": 20, "in_progress": 10, "pending_customer": 6, "pending_internal": 1},
    "overdue": 3,
    "unassigned": 7,
    "created_today": 18,
    "resolved_today": 21,
    "today_start": "2026-11-17T00:00:00+03:00"
  },
  "current": {"...": "metrics"},
  "previous": {"...": "metrics"},
  "change": {"...": "change"},
  "trend": [{"bucket": "...", "...": "metrics"}],
  "by_priority": ["group, critical first"],
  "by_channel": ["group, busiest first"],
  "by_category": ["group, busiest first"],
  "by_team": ["group, busiest first"]
}
```

`stats` is live and follows the dimension filters but not the window. `open` and `by_status` count the open statuses (new, open, in progress, pending customer, pending internal). `overdue` is open tickets past their resolution deadline whose clock is not paused. `unassigned` is open tickets with no agent. `created_today` and `resolved_today` count from midnight in `tz`.

## GET /api/v1/{tenant}/analytics/agents

```json
{
  "period": {"...": "period"},
  "filters": {"...": "filters"},
  "agents": [
    {
      "agent_id": "uuid or null",
      "name": "Wanjiku",
      "email": "wanjiku@example.com",
      "open": 3,
      "...": "metrics",
      "previous_resolved": 12,
      "resolved_change_pct": 25.0
    }
  ]
}
```

One row per agent with figures in the window or open tickets now, most resolved first, then most open. The row with `agent_id` null is the unassigned tickets. `open` is live.

## GET /api/v1/{tenant}/analytics/sla

```json
{
  "period": {"...": "period"},
  "filters": {"...": "filters"},
  "overdue": 3,
  "current": {"...": "metrics"},
  "previous": {"...": "metrics"},
  "change": {"...": "change"},
  "trend": [{"bucket": "...", "...": "metrics"}],
  "by_priority": ["group, critical first"],
  "by_team": ["group, most targets first"],
  "by_agent": ["group, most targets first"]
}
```

Read `first_response_sla`, `resolution_sla` and the two average times from each metrics object. `overdue` is live, as in the ticket stats. `GET /sla/reports` from sprint 10 stays as the SLA report by creation window in policy minutes.

## GET /api/v1/{tenant}/analytics/csat

```json
{
  "period": {"...": "period"},
  "filters": {"...": "filters"},
  "current": {"responses": 40, "average": 4.35, "satisfied_pct": 85.0, "scores": {"1": 1, "2": 2, "3": 3, "4": 10, "5": 24}},
  "previous": {"...": "same shape"},
  "change": {"...": "change"},
  "trend": [{"bucket": "...", "...": "metrics; read csat"}],
  "by_agent": ["group with responses, most responses first"],
  "by_team": ["group with responses"],
  "by_channel": ["group with responses"],
  "recent_comments": [
    {"ticket_id": "uuid", "ticket_number": "TKT-000042", "score": 2, "comment": "Took three days", "submitted_at": "RFC 3339"}
  ]
}
```

`recent_comments` lists the 20 latest responses in the window that have a comment, newest first.

## GET /api/v1/{tenant}/analytics/{view}/export

`view` is `tickets`, `agents`, `sla` or `csat`; `format` is `pdf` (default), `csv` or `xlsx`; every other parameter is the view's. The file is rendered by the shared reports module (`github.com/Bengo-Hub/reports` v0.2.0) from the same object the view returns, so it shows the screen's figures for the same filters. The document carries the tenant's name and brand colour, the effective window, the time zone, the previous period and the filters. The response is a download (`Content-Disposition: attachment; filename="<view>-analytics-<from>-<to>.<ext>"`, `Cache-Control: private, no-store`). An unknown view answers 404 and a bad format 400.

## CSAT survey

Resolving a ticket whose requester has an email issues a survey token. The token is 54 characters (the ticket id and 24 random bytes, base64url). Only its SHA-256 is stored, in ticket metadata `csat` (`token_hash`, `issued_at`, `expires_at`, and `used_at` once answered). The token goes to the customer in `ticketing.ticket.csat_requested` and works for `CSAT_TOKEN_TTL` (default 7 days). Resolving the ticket again issues a new token that replaces the old one. WhatsApp-only tickets get no survey; WhatsApp surveys are off by owner decision.

The link is `CSAT_SURVEY_URL?tenant=<tenant id>&token=<token>` (default `https://ticketing.codevertexafrica.com/support/csat`). The survey page calls the two public routes below with the tenant id from the link as `{tenant}`; a public route accepts a tenant UUID as well as a slug. Both take no sign-in and are rate limited to 20 calls an hour per IP.

**POST /api/v1/{tenant}/public/csat/check** with `{"token": "..."}` checks the token without using it and answers 200 `{"ticket_number": "TKT-000042", "subject": "Printer jam", "expires_at": "RFC 3339"}`.

**POST /api/v1/{tenant}/public/csat** with `{"token": "...", "score": 1-5, "comment": "optional, at most 2000 characters"}` stores the rating and uses up the token in one transaction. It answers 201 `{"status": "recorded"}`.

| Status | Meaning (both routes) |
|---|---|
| 400 | Missing token, a score outside 1 to 5 or a comment over 2000 characters |
| 404 | Unknown token, or a token that is not the ticket's current one |
| 409 | Already answered |
| 410 | Expired |
| 429 | Rate limited |

## Benchmark

`internal/modules/analytics/benchmark_test.go` (build tag `benchmark`) seeds 200k tickets over a year with CSAT responses into the database at `ANALYTICS_BENCH_DATABASE_URL`, runs the backfill, checks the rollup's resolved total against the tickets table, then calls every analytics route 60 times for the default window, a one-year window and a filtered window, and fails when any p95 is over 300 ms.

```sh
ANALYTICS_BENCH_DATABASE_URL=postgres://localhost:5432/ticketing_bench?sslmode=disable \
  go test -tags benchmark -run TestAnalyticsP95 -timeout 60m ./internal/modules/analytics/
```

The database must be a throwaway one: the test creates the schema and deletes its tenant's rows before seeding.
