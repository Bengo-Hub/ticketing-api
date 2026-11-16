# Sprint 10: SLA That Matches the Leaders

**Status: Pending.** It finishes what [sprint 3](sprint-3-sla.md) started. ticketing-ui sprint 9 consumes it.

## Tasks

- [ ] Business-hours calendars with holidays, in the tenant or outlet timezone, applied when a policy's business-hours flags are set
- [ ] A default calendar seeded for every tenant with Kenyan public holidays
- [ ] SLA clocks pause on `pending_customer` and `pending_internal` and resume on the next active status
- [ ] Pre-breach warning events, with `notify_before_breach_min` in policy metadata
- [ ] Escalation levels: raise the priority, reassign to the supervisor team, write `escalation_level` and `escalated_at`, publish `ticketing.ticket.escalated`
- [ ] Every scan stays in `ticketing-worker` under `RunOnce`
- [ ] `GET /api/v1/{tenant}/sla/reports` with breach counts, breach rate and average response and resolution times by priority and period, from rollups
- [ ] Testcontainers tests for deadline calculation across weekends, holidays and pauses

## Acceptance criteria

- A ticket opened at 17:00 on a Friday with a 2-hour business-hours response target is due at 10:00 on Monday when the calendar runs 08:00 to 17:00 on weekdays.
- A ticket in `pending_customer` for 3 hours gets 3 hours more on its resolution deadline.
- A warning event arrives the configured number of minutes before breach, once.
- An escalated ticket shows its new level, priority and team, and the event fires once.

## Fix requirements proved

- **Fix 2, low learning curve.** A new tenant gets the seeded categories, SLA policy, macros and Kenyan holiday calendar, and an e2e test takes a fresh tenant from first ticket to resolution without opening an admin page.
- **Fix 1, no feature behind an add-on.** SLA with business hours and escalation works on a tier-1 tenant.
