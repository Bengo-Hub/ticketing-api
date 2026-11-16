# Sprint 3: SLA Management

**Status: Done.** Policy CRUD, deadlines on create and the breach worker were built here; business calendars, pauses, warnings, escalation and the SLA report were built in [sprint 10](sprint-10-sla-leaders.md).

## Tasks

- [x] `POST/GET /api/v1/{tenant}/sla/policies`, `GET/PUT/DELETE /sla/policies/{policyId}` (`ticketing.sla.*`), list filters `priority` and `is_active`
- [x] On create, the matching active policy (priority, optional category) sets `sla_response_due_at` and `sla_resolution_due_at`
- [x] The first public agent reply sets `first_response_at`
- [x] `ticketing-worker` scans every 5 minutes, 200 tickets at a time, records each breach once with its event in one transaction, and publishes `ticketing.sla.first_response_breached` or `ticketing.sla.resolution_breached` through the outbox
- [x] The scan runs only in the worker Deployment, each tick claimed fleet-wide with the shared cache `RunOnce` (4f03d8e). Before that commit it also ran inside both API pods.
- [x] Seeded default SLA policies per tenant through `ticketing-seed`
- [x] Business hours: a policy's `calendar_id` replaced the never-applied flags and `business_hours` map (sprint 10)
- [x] Pause clocks on `pending_customer`, and on `pending_internal` when the policy says so (sprint 10)
- [x] Pre-breach warnings and escalation (sprint 10)
- [x] `GET /sla/reports` (sprint 10)

## Acceptance criteria

- A ticket past its response deadline gets exactly one breach event however many worker replicas run. Met.
- A policy with a calendar only counts business hours. Met in sprint 10.
