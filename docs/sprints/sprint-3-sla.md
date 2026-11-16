# Sprint 3: SLA Management

**Status: Partial.** Policy CRUD, deadlines on create and the breach worker are built. Deadlines are wall-clock: business hours, pauses, escalation and the SLA report move to [sprint 10](sprint-10-sla-leaders.md).

## Tasks

- [x] `POST/GET /api/v1/{tenant}/sla/policies`, `GET/PUT/DELETE /sla/policies/{policyId}` (`ticketing.sla.*`), list filters `priority` and `is_active`
- [x] On create, the matching active policy (priority, optional category) sets `sla_response_due_at` and `sla_resolution_due_at`
- [x] The first public agent reply sets `first_response_at`
- [x] `ticketing-worker` scans every 5 minutes, 200 tickets at a time, records each breach once with its event in one transaction, and publishes `ticketing.sla.first_response_breached` or `ticketing.sla.resolution_breached` through the outbox
- [x] The scan runs only in the worker Deployment, each tick claimed fleet-wide with the shared cache `RunOnce` (4f03d8e). Before that commit it also ran inside both API pods.
- [x] Seeded default SLA policies per tenant through `ticketing-seed`
- [ ] Apply `first_response_business_hours`, `resolution_business_hours` and `business_hours` (stored, ignored today)
- [ ] Pause clocks on `pending_customer` and `pending_internal`
- [ ] Pre-breach warnings and escalation
- [ ] `GET /sla/reports`

## Acceptance criteria

- A ticket past its response deadline gets exactly one breach event however many worker replicas run. Met.
- A policy marked business hours only counts business hours. Not met, sprint 10.
