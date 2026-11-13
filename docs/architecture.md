# Ticketing Service — Architecture

## Overview

Multi-tenant SaaS ticketing service. All data is tenant-scoped via `tenant_id` derived from JWT claims. No local tenant table — tenant identity is owned by auth-api and cached via `github.com/Bengo-Hub/cache v0.2.0`.

---

## Service Boundaries

| Concern | Owned By |
|---------|----------|
| Users / auth / SSO | auth-api |
| Tenant branding + subscription | auth-api (cached in Redis) |
| Agents / teams | ticketing-api ✅ |
| Tickets / comments / categories | ticketing-api ✅ |
| SLA policies | ticketing-api (Sprint 3) |
| Notifications delivery | notifications-api, from the ticket events on `ticketing.>` (no direct call) |
| Tenant support contact (support email, phone, WhatsApp) | auth-api tenant metadata `support`, read through the shared tenant cache |
| Email templates | notifications-api |
| File storage (attachments) | S3-compatible object store |

---

## Module Structure

```
internal/
├── app/                  # Wire all dependencies
├── clients/
│   └── otp.go            # auth-api public OTP token validation
├── config/               # envconfig-based config
├── ent/                  # Ent ORM (schema + generated code)
│   └── schema/           # agents, teams, tickets, comments, attachments, categories, slapolicies, outboxevents
├── http/
│   ├── handlers/         # health, metrics
│   └── router/           # chi router + all route registrations
├── modules/
│   ├── identity/         # teams, agents, JIT auth event handler
│   ├── tickets/          # tickets, comments, attachments, categories
│   └── sla/              # SLA policies (Sprint 3)
├── platform/
│   ├── cache/            # Redis client
│   ├── database/         # pgx pool
│   ├── events/           # NATS conn + JetStream stream setup + outbox publisher
│   └── tenantcache/      # cache.Aside wrapper for tenant detail lookups
└── shared/
    ├── logger/           # zap logger factory
    └── middleware/       # request ID, logging, recovery
```

---

## Data Flow

### Ticket Creation (JWT)
```
Client → chi router → JWT middleware → tickets.Handler.CreateTicket
    → tickets.Service.CreateTicket → tickets.EntRepository.CreateTicket (PG)
    then events.Publisher.PublishTicketCreated (outbox, then the NATS background publisher)
    then the notifications-api ticketing consumer (email to the requester)
```

The requester's contact is stored in ticket metadata as `requester_name`, `requester_email` and
`requester_phone` on every create path: the signed-in route (the body's `requester_*` fields, or
the caller's own email when they raise a ticket for themselves), the S2S route and the public form
(its OTP-verified email). Public tickets created before 13 November 2026 kept `customer_email` and
`customer_name`; readers still accept those.

### Ticket Creation (S2S — Vera AI)
```
marketflow-ai (X-API-Key) → POST /api/v1/internal/tickets
    → tickets.Handler.InternalCreateTicket (validates INTERNAL_SERVICE_KEY)
    → tickets.Service.CreateTicket → same flow above
```

### JIT Agent Provisioning
```
auth-api → NATS publish auth.user.created
    → identity.AuthEventHandler.SubscribeToAuthEvents (plain NATS subscribe)
    → handleUserCreated: check role → create agent record in PG
```

### Tenant Branding
```
Client → GET /api/v1/{tenant}/branding
    → tenantcache.Resolver.GetTenantDetails(slug)
    → cache.GetOrSet → Redis hit → return
    → cache miss → GET auth-api /api/v1/tenants/by-slug/{slug} → cache write → return
```

---

## Shared Libraries

| Library | Version | Purpose |
|---------|---------|---------|
| `github.com/Bengo-Hub/shared-auth-client` | v0.6.1 | JWT validation middleware, ClaimsFromContext, GetTenantSlug |
| `github.com/Bengo-Hub/cache` | v0.2.0 | Redis-backed tenant detail cache (Aside pattern) |
| `github.com/Bengo-Hub/pagination` | v0.2.0 | Parse(r) → Params, NewResponse() on all list endpoints |
| `github.com/Bengo-Hub/shared-events` | v0.2.0 | Transactional outbox + background NATS publisher |
| `entgo.io/ent` | v0.14.5 | Schema-as-code ORM |

---

## NATS Subjects

| Subject | Publisher | Subscriber |
|---------|-----------|-----------|
| `auth.user.created` | auth-api | ticketing-api (JIT provisioning) |
| `auth.user.updated` | auth-api | ticketing-api (agent sync) |
| `ticketing.ticket.created` | ticketing-api (outbox) | notifications-api (to the requester) |
| `ticketing.ticket.assigned` | ticketing-api (outbox) | notifications-api (to the agent) |
| `ticketing.ticket.comment_added` | ticketing-api (outbox), public agent replies only | notifications-api (to the requester) |
| `ticketing.ticket.status_changed` | ticketing-api (outbox), every change except to resolved | notifications-api (to the requester) |
| `ticketing.ticket.resolved` | ticketing-api (outbox) | notifications-api (to the requester) |
| `ticketing.sla.first_response_breached`, `ticketing.sla.resolution_breached` | ticketing-api (worker) | notifications-api (to the agent) |
| `crm.activity.log_requested` | ticketing-api (outbox) | marketflow CRM |

Payloads are built in `internal/modules/tickets/events.go` and covered by `events_test.go`. Every
ticket event carries `ticket_id`, `ticket_number`, `subject`, `status`, `priority`, `channel`,
`source`, `agent_id`, `requester_name`, `requester_email` and `requester_phone` (each key always
present, empty when unknown). On top of that:

| Event | Extra keys |
|-------|------------|
| `ticket.created` | `description` (first 500 characters), `created_at` |
| `ticket.assigned` | `agent_email` |
| `ticket.comment_added` | `comment_id`, `comment` (first 500 characters), `author_id`, `author_type`, `agent_email` |
| `ticket.status_changed` | `old_status`, `new_status` |
| `ticket.resolved` | `old_status`, `new_status` (`resolved`), `resolved_at` |

SLA events carry `ticket_id`, `ticket_number`, `subject`, `priority`, `status`, `agent_id`,
`agent_email`, `team_id`, `response_breached`, `resolution_breached` and `event_type`, and never the
requester's contact.

## Support routing (12 November 2026)

notifications-api emails the requester from the contact on the event: the receipt
(`ticket.created`), every public agent reply (`ticket.comment_added`), a status change that needs
them (waiting for their reply, closed, cancelled, reopened) and the resolution. The mail goes out
under the tenant's name with the tenant's support address as Reply-To (auth-api tenant metadata
`support`, read with `cache.ResolveSupportContact`, falling back to its contact email), so a
reply reaches the tenant's support inbox. Platform tickets (the codevertex tenant) use
support@codevertexafrica.com. A requester with no email gets nothing, never a copy in the
tenant's mailbox. SMS and WhatsApp to the requester's phone are not sent yet: WhatsApp needs
templates approved by Meta, and both wait for that work.

Inbound email and WhatsApp to ticket are designed, not built: a message to a support address or
number does not open a ticket yet. Tickets are opened by the signed-in route, the public form and
the S2S route (Vera, marketflow-ai, other services).

---

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `POSTGRES_URL` | ✅ | — | PostgreSQL DSN |
| `REDIS_ADDR` | — | `localhost:6379` | Redis address |
| `EVENTS_NATS_URL` | — | `nats://localhost:4222` | NATS server URL |
| `AUTH_SERVICE_URL` | — | `http://localhost:8080` | auth-api base URL |
| `AUTH_ISSUER` | — | `https://sso.codevertexafrica.com` | JWT issuer (must match the SSO token `iss`) |
| `AUTH_AUDIENCE` | — | `codevertex` | JWT audience (SSO tokens carry `codevertex`) |
| `INTERNAL_SERVICE_KEY` | — | `""` | Shared S2S API key |
| `TENANT_CACHE_TTL` | — | `5m` | Tenant detail cache TTL |
| `HTTP_PORT` | — | `4009` | HTTP listen port |

## Seed

`cmd/seed` (built into the image as `/app/ticketing-seed`, run at start by `scripts/entrypoint.sh`)
is idempotent and per tenant. It writes only what the tenant does not have yet.

| Variable | Required | Description |
|----------|----------|-------------|
| `TENANT_ID` | yes | The tenant's auth UUID |
| `KB_SET` | no | Knowledge-base article set to load from `cmd/seed/kb/<set>/*.json` (for example `hadia`) |
| `SEED_SCOPE` | no | `kb` loads only the articles and leaves categories and SLA policies alone; default `all` |

Without `TENANT_ID` the start-up seed exits early, which is expected for a shared deployment.
Articles are published and public, with author `00000000-0000-0000-0000-000000000000` (seeded) and
`metadata.seeded_from = ticketing-seed`. An article whose slug the tenant already has is never
changed, even if staff edited, archived or deleted it. Each JSON file holds `slug`, `title`,
`excerpt`, `category`, `tags` and `content_html`; `kb_test.go` checks every set before it ships.

To seed one tenant in the cluster, run a one-off Job with the deployment's image and its own
`POSTGRES_URL` and `POSTGRES_MIGRATE_URL` secret references, plus `TENANT_ID`, `KB_SET` and
`SEED_SCOPE=kb`. First used 8 November 2026 for Hadia's safety article (Job
`ticketing-kb-seed-hadia`, cleaned up an hour after it finishes).
