# Ticketing API Plan

ticketing-api is the multi-tenant help desk for the Codevertex platform: tickets, comments, categories, agents and teams, SLA policies, routing rules, the knowledge base and the public support routes. It owns those records and its local ticketing roles and permissions. It does not own users or tenants (auth-api), contacts (marketflow CRM), message delivery (notifications-api), or the orders, invoices and projects a ticket refers to.

Design detail is in [architecture.md](architecture.md), the schema in [erd.md](erd.md) and every integration in [integrations.md](integrations.md).

## State on 2026-11-16

Commit 4f03d8e is live in production. The production migration recorded the baseline, applied `20261116090100_phase0.sql`, and the API seeded the 108 permission codes; `/readyz` answers OK. That commit delivered:

- Trinity authorization: `RequireAuth` with API keys, `RequireActiveSubscriptionForMutations`, `TenantV2`, the tenant guard, just-in-time ticketing users with role mapping, `OutletContext` and `RequirePermission` on every route, plus `GET /{tenant}/ticketing/auth/me`.
- Per-tenant ticket numbers from `ticket_sequences`.
- Outbox writes in the domain transaction.
- `tenant_id` on comments and attachments, agents unique per tenant, KB counters scoped by tenant.
- Versioned migrations applied at pod start under an advisory lock.
- The SLA scan only in the worker under `RunOnce`, real `/metrics` and `/readyz`, `go vet` and `go test` in CI.
- The internal create contract with `source` and `source_ref`.
- Shared libraries at auth-client v0.20.0, httpware v0.20.0, shared-events v0.11.1, cache v0.5.10, pagination v0.5.0 and shared-ratelimit v0.3.1.

On 2026-11-17 Sprint 10 (2f0a568) is live, and so is the Sprint 11 ingest (a67e2e6). It has one ingest service for email and WhatsApp, two durable consumers, the per-tenant settings document with `channels.whatsapp.auto_ticket`, the threading fields on every ticket event and migration `20261117120000_sprint11.sql`. Its publishers are deployed too. email-provisioner 3da3d72 reads the support mailboxes and publishes `email.inbound.support`. notifications-api bec16df publishes `notifications.whatsapp.tenant_inbound` and threads ticket emails. Email intake starts once the Stalwart support domain, the ingest mailbox and its DNS records exist.

On 2026-11-17 I also wrote sprint 14: treasury-api's user management and role administration, ported to ticketing. Roles can be created, customized, cloned, edited and deleted, people are added through auth-api's S2S member add, roles are granted with an optional end date and revoked without sign-in giving them back, and every change lands in the new `audit_logs` table. The old `PUT /ticketing/users/{userId}/roles` is gone. It is not deployed yet.

On 2026-11-17 I also built sprint 12: hourly and daily rollups filled by the worker, the `/analytics/tickets`, `/analytics/agents`, `/analytics/sla` and `/analytics/csat` routes read only from them, export through the shared reports module, and CSAT surveys sent on resolve with a single-use link. The route contract is [analytics.md](analytics.md). It is not deployed yet.

The sprint list below gives the status of each sprint against the code.

## Find, Flip, Fix (read 2026-11-16)

Vendor comparison blogs are weak evidence. Capterra and G2 review tallies are stronger evidence.

### Find: what people pay for

| Product | Price (USD per agent per month, sources disagree) | Known for | Top complaints |
|---|---|---|---|
| Zendesk | Suite Team about 55, Professional about 115; Copilot, WFM and Contact Center add-ons at 50 each; AI resolutions 1.50 to 2.00 each | Unified agent workspace, omnichannel, the largest app ecosystem | Price and add-ons, key features locked to high tiers, admin complexity, slow vendor support (Trustpilot 1.8) |
| Freshdesk | Free (2 agents), Growth from about 15, AI add-on about 29 | Collision detection from Growth, parent-child tickets on Pro, SLA with business hours, portal | G2: "missing features" (115) and "ticketing issues" (93) |
| Zoho Desk | Free (3 agents), Standard 9 to 14, Professional 16 to 23 | Cheapest, Zia AI in 40+ languages, Zoho suite integration | Steep learning curve (329 and 233 G2 mentions) |
| Help Scout | 19 to 115, partly contact-based | Simple shared-inbox email support | Costs climb unpredictably with contact volume |
| HappyFox | 21 to 24 (help desk), 49 (ITSM) | Fast setup, easy navigation | Limited automation, small integration set |
| Jira Service Management | Free (3 agents), Standard about 20, Premium about 45 | Internal IT desk, change and problem management | Learning curve, complexity, change management locked to Premium |

The features the leaders share make up the baseline:

- Agent workspace: views and saved filters, collision detection, macros and canned replies, internal notes and public replies, merge, parent-child links, bulk actions, tags, @mentions.
- SLA: business hours, holidays, pause on pending, pre-breach alerts and escalation.
- Customer side: CSAT after resolution, a knowledge base with real search and deflection, a customer portal.
- Reporting: agent and SLA reports.
- Channels: email, chat or WhatsApp, and a web form.

### Flip: the angle for Kenya and East Africa

- WhatsApp is the first channel, not an add-on, through notifications-api, which the fleet already uses.
- Pricing is in KES with no per-feature add-ons. Ticketing is part of the existing subscription plans, gated only through entitlements, and core ticket work is never plan-gated.
- AI (Vera through marketflow-ai) is billed only from the existing prepaid AI credit wallet, with no per-resolution metering.
- The customer is the marketflow contact. Ticketing stores no PII copy, so tickets link to POS orders, ordering orders, projects QA and treasury invoices across the platform.
- The agent UI works offline, the PWA is mobile-first, and it is in English and Swahili.

### Fix: competitor complaints as testable requirements

1. **No feature behind an add-on.** An e2e test with a tier-1 tenant creates, assigns, comments on, merges and resolves a ticket, and uses macros and SLA. Proved in sprints 9, 10 and 11.
2. **Low learning curve.** Seeded defaults (categories, SLA policy, macros, a business-hours calendar with Kenyan public holidays) give a new tenant a working desk with zero configuration. An e2e test runs a fresh tenant from first ticket to resolution without opening the admin pages. Proved in sprint 10.
3. **No missed duplicate replies.** Collision detection covers viewing and typing. An e2e test with two agents on one ticket shows the second agent the warning within 2 seconds. Proved in sprint 9.
4. **Predictable cost.** AI calls debit the AI credit wallet and refuse cleanly at zero balance. A unit test with a zero-balance tenant hides the AI suggestion and still saves the ticket. Proved in sprint 13.
5. **Reports stay fast as data grows.** Analytics come from rollup tables and indexed `GROUP BY`, never row loads. A benchmark with 200k tickets keeps `/analytics/*` under 300 ms p95 locally. Proved in sprint 12 by `internal/modules/analytics/benchmark_test.go`; the first recorded run is pending.

### Sources (read 2026-11-16)

- https://www.featurebase.app/blog/freshdesk-vs-zendesk-vs-zoho
- https://eesel.ai/blog/zendesk-review
- https://hiverhq.com/blog/zendesk-reviews
- https://www.capterra.com/p/164283/Zendesk/reviews/
- https://www.capterra.co.za/reviews/169505/zoho-desk
- https://www.capterra.co.za/reviews/124981/freshdesk
- https://www.g2.com/compare/freshdesk-vs-helpdesk-vs-zoho-desk
- https://www.getmacha.com/blog/freshdesk-features
- https://toolradar.com/blog/help-desk-software-comparison
- https://www.eesel.ai/blog/help-scout-review
- https://costbench.com/compare/happyfox-help-desk-vs-jira-service-management/
- https://superframeworks.com/articles/best-zendesk-alternatives

## Sprints

| Sprint | Scope | Status | What is missing |
|---|---|---|---|
| [0](sprints/sprint-0-foundation.md) | Foundation | Partial | Tracing, Testcontainers tests |
| [1](sprints/sprint-1-identity.md) | Identity and access | Done | |
| [2](sprints/sprint-2-tickets.md) | Ticket management | Partial | Attachment upload, ticket search, clearing fields on update |
| [3](sprints/sprint-3-sla.md) | SLA management | Done | Finished by sprint 10 |
| [4](sprints/sprint-4-public-api.md) | Public API and email codes | Done | |
| [5](sprints/sprint-5-knowledge-base.md) | Knowledge base | Partial | Full-text search, category list, suggestions |
| [6](sprints/sprint-6-analytics.md) | Analytics | Done | Finished by sprint 12 |
| [7](sprints/sprint-7-notifications.md) | Notifications | Partial | Preferences, @mentions, SMS and WhatsApp |
| [8](sprints/sprint-8-routing.md) | Routing and classification | Partial | Least-loaded fallback, ticket counts, escalation, AI classification |
| [9](sprints/sprint-9-agent-productivity.md) | Agent productivity | Pending | Keyset paging, full-text search, views, tags, macros, merge, parent-child, bulk, collision presence, @mentions, attachments on R2 |
| [10](sprints/sprint-10-sla-leaders.md) | SLA that matches the leaders | Built, not deployed | Testcontainers tests for the scans and the report; the notifications-api consumer for warnings and escalations |
| [11](sprints/sprint-11-channels.md) | Channels | Ingest, publishers and the settings page deployed; remainder in progress | The Stalwart support domain, ingest mailbox and DNS (need the owner to run the keyed calls), CRM contact link at ingest, WhatsApp agent replies and the 24-hour window, forwarding verify step, channel badge and filter, the web widget, and Convert to ticket in the notifications inbox |
| [12](sprints/sprint-12-analytics-csat.md) | Analytics and CSAT | Built, not deployed | The migration for the compact series and the backlog index, a passing 200k benchmark run, the notifications-api survey email |
| [13](sprints/sprint-13-ai-vera.md) | AI with Vera | Pending | Classify, summarize, suggested reply, KB suggestions, AI wallet credits, `IsGatingExempt` for codevertex only |
| [14](sprints/sprint-14-user-management.md) | User management and role administration (treasury port) | Built, not deployed | Ent generation and the `audit_logs` migration, build and tests, the ticketing-ui pages |

After sprint 13 comes the integration review and test pass: auth-api (including `auth.user.deleted`), notifications-api, marketflow, subscriptions-api, marketflow-ai, projects-api end to end, and the pos, ordering and treasury links shown on a ticket, with Testcontainers repository tests and Playwright e2e tests for the five Fix requirements.

## Known issues and pending

| Item | Repo | Where it is fixed |
|---|---|---|
| GitHub reports 57 Dependabot alerts on ticketing-ui | UI | Integration review and test pass |
| No service worker ships in production: next-pwa runs only under webpack and the Docker build uses Turbopack | UI | UI sprint 8, app shell and offline |
| The public support pages still use native form fields and a plain textarea | UI | UI sprint 8 |
| `SLASidebar` and the policy form still use the removed `first_response_business_hours`, `resolution_business_hours` and `business_hours`; the API now takes `calendar_id` (null is 24x7) and `clear_calendar` | UI | UI sprint 9 |
| Update requests cannot clear a field, because `UpdateTicketRequest` uses pointers with `omitempty` | API | Sprint 9 |
| Fields the UI needs and the API lacks: a KB category list, comment author names (`resolved_today`, `overdue` and previous-period figures are in `/analytics/tickets` since sprint 12) | API | Sprint 9 |
| The OTP client is not on shared-service-client, because a retried call could replay a one-time code | API | Kept as is by design |
| `internal/modules/tickets/handler.go` (469 lines) and `internal/modules/identity/handler.go` (465 lines) are over 400 lines | API | Sprint 9 |
| `ListTickets` does not read the `search` parameter yet | API | Sprint 9 |
| Turbopack on Windows refuses the shared-ui-lib `@source` path through the pnpm junction, so local verification uses `next build --webpack` while CI builds with Turbopack on Linux | UI | Open |
| `IncrementAgentTicketCount`, `DecrementAgentTicketCount` and `GetLeastLoadedAgent` exist and the ticket flow does not call them | API | Sprint 8 follow-up |

## Runtime

- API on `HTTP_PORT`, default 4009. Metrics on `METRICS_PORT`, default 9090.
- Public host `ticketingapi.codevertexafrica.com`.
- Migrations run from `scripts/entrypoint.sh` at every pod start. The SLA scans (breaches, warnings, escalations) and the analytics rollup run only in the `ticketing-worker` Deployment, every minute.

## References

- [Architecture](architecture.md)
- [ERD](erd.md)
- [Integrations](integrations.md)
- [Analytics and CSAT](analytics.md)
- [Superset design](superset-integration.md)
- [ticketing-ui plan](../../ticketing-ui/docs/plan.md)
