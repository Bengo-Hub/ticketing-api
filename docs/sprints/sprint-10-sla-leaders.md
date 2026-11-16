# Sprint 10: SLA That Matches the Leaders

**Status: Built, not yet deployed.** It finishes what [sprint 3](sprint-3-sla.md) started. ticketing-ui sprint 9 consumes it. The design is in [architecture.md](../architecture.md), sections SLA worker, SLA clocks and SLA report.

## Tasks

- [x] Business calendars per tenant (`/sla/calendars`, `ticketing.sla.*`): IANA timezone (default `Africa/Nairobi`), working periods per weekday, holidays with names, one default
- [x] A tenant's first calendar is seeded with the Kenyan public holidays of this year and next; Idd ul-Fitr is flagged moon-dependent for the tenant to confirm, and no date is guessed for a year without a published one. `ticketing-seed` creates that calendar for a tenant without one
- [x] A policy's `calendar_id` makes both targets count business minutes; null counts every minute. The unused business-hours flags and `business_hours` map are removed
- [x] Deadlines on create come from the business-time function in `internal/modules/sla/clock`, not from creation time plus minutes
- [x] The clock stops in `pending_customer`, and in `pending_internal` when the policy sets `pause_on_pending_internal`; leaving the status moves the open deadlines by the paused business minutes (`sla_paused_at`, `sla_paused_minutes`)
- [x] Pre-breach warnings: `notify_before_breach_min` in policy metadata, `ticketing.sla.breach_warning` once per ticket and target, deduplicated in ticket metadata
- [x] Escalation rules in policy metadata: before or after a breach, raise the priority one level and or assign a team, write `escalation_level` and `escalated_at`, an internal note and `ticketing.ticket.escalated`, bounded per tick
- [x] Every scan stays in `ticketing-worker` under `RunOnce`, one period key per scan
- [x] `GET /api/v1/{tenant}/sla/reports` (`ticketing.analytics.view`): compliance, met and breached counts, average and p90 first-response and resolution business minutes, by priority and by team, from one indexed `GROUP BY GROUPING SETS` query
- [x] Unit tests: the business-time function (weekends, holidays, timezones, overnight periods, a DST zone), the Kenyan holiday lists for 2026 and 2027, pause and resume maths, warning dedupe, escalation, report SQL building
- [ ] Testcontainers tests for the scans and the report against PostgreSQL (integration review and test pass)
- [ ] notifications-api consumer for `ticketing.sla.breach_warning` and `ticketing.ticket.escalated`

## Acceptance criteria

- A ticket opened at 17:00 on a Friday with a 2-hour business-hours response target is due at 10:00 on Monday when the calendar runs 08:00 to 17:00 on weekdays. Met, `TestAddNairobiWeekdays` and `TestComputeDeadlinesUsesCalendar`.
- A ticket in `pending_customer` for 3 hours gets 3 hours more on its resolution deadline. Met, `TestPauseAndResumeMoveDeadlines` and `TestStatusFlowPausesAndResumes`.
- A warning event arrives the configured number of minutes before breach, once. Met in unit tests (`TestBreachWarningOnce`); the scan query needs the database test.
- An escalated ticket shows its new level, priority and team, and the event fires once. Met in unit tests (`TestApplyEscalation`).

## Decisions

- One clock per policy. A calendar applies to both targets; a policy that needs a 24x7 response and a business-hours resolution is two policies' worth of configuration that no leader asked for, so the per-target flags went.
- The warning lead is wall-clock minutes before the deadline, so the scan is one indexed range query.
- 10 October is Mazingira Day. The Statute Law (Miscellaneous Amendments) Act 2024 renamed Utamaduni Day.
- Escalating to a team clears the agent, so the ticket lands in the team's queue.
- A ticket can be escalated up to 30 days after its deadline; that bound keeps the scan off old tickets.

## Fix requirements proved

- **Fix 2, low learning curve.** A new tenant gets the seeded categories, SLA policy, macros and Kenyan holiday calendar, and an e2e test takes a fresh tenant from first ticket to resolution without opening an admin page. The calendar part is built; the e2e test is in the test pass.
- **Fix 1, no feature behind an add-on.** SLA with business hours and escalation works on a tier-1 tenant: nothing here is plan-gated.
