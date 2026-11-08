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
| Notifications delivery | notifications-api (S2S call) |
| Email templates | notifications-api |
| File storage (attachments) | S3-compatible object store |

---

## Module Structure

```
internal/
├── app/                  # Wire all dependencies
├── clients/
│   └── notifications.go  # S2S → notifications-api
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
    → events.Publisher.publish (outbox → NATS background publisher)
    → (async) notifications.Client.SendTicketCreated
```

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
| `ticketing.ticket.assigned` | ticketing-api (outbox) | notifications-api |
| `ticketing.ticket.resolved` | ticketing-api (outbox) | notifications-api |
| `ticketing.sla.first_response_breached`, `ticketing.sla.resolution_breached` | ticketing-api (worker) | notifications-api |

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
| `NOTIFICATIONS_SERVICE_URL` | — | `http://localhost:4003` | notifications-api base URL |
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
