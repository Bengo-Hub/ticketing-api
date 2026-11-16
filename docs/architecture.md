# Ticketing API Architecture

ticketing-api is the multi-tenant help desk behind ticketing-ui. Every row it owns carries a `tenant_id`, and every query is scoped by it in code. Ticketing keeps no tenant table and no user directory of its own: tenants and users belong to auth-api, and the tenant record is read through the shared tenant cache.

## Service boundaries

| Concern | Owner |
|---|---|
| Users, sign-in, SSO, API keys, tenants | auth-api |
| Tenant branding, subscription and support contact (metadata `support`) | auth-api, read through the shared tenant cache |
| Ticketing roles, permissions and users (local RBAC) | ticketing-api |
| Agents and teams | ticketing-api |
| Tickets, comments, attachment records, categories, ticket numbers | ticketing-api |
| SLA policies, routing rules, knowledge base articles | ticketing-api |
| Email and message delivery, templates | notifications-api, from the `ticketing.>` events |
| CRM contacts and activity | marketflow |
| Attachment files | object storage (upload is planned in sprint 9) |

## Processes

The image holds four binaries, built by the `Dockerfile`.

| Binary | Role |
|---|---|
| `ticketing-api` | HTTP API on `HTTP_PORT` (default 4009) and Prometheus metrics on `METRICS_PORT` (default 9090). It also drains the outbox to JetStream. |
| `ticketing-migrate` | Applies the versioned migrations. `scripts/entrypoint.sh` runs it before the API starts. |
| `ticketing-worker` | The SLA breach scan. It runs as its own Deployment and is the only process that runs the scan. |
| `ticketing-seed` | Per-tenant seed of categories, SLA policies and knowledge base sets. It is a manual step. |

## Module layout

```
cmd/
  api/        HTTP server
  migrate/    versioned migrations under the advisory lock
  worker/     SLA breach scan
  seed/       per-tenant seed and knowledge base sets (kb/<set>/*.json)
internal/
  app/        wiring of every dependency
  clients/    otp.go, the auth-api public OTP token check
  config/     envconfig settings
  ent/        Ent schema and generated code; migrate/migrations holds the SQL files
  http/
    handlers/ health, readiness, status
    metrics/  Prometheus collectors and middleware
    router/   chi router and the auth chain
  modules/
    identity/ teams, agents, auth.user.* consumer
    kb/       knowledge base articles
    rbac/     permission catalogue, roles, ticketing users, auth/me
    routing/  routing rules
    sla/      SLA policies and deadline calculation
    tickets/  tickets, comments, attachments, categories, public and internal routes, SSE, outbox
  platform/
    access/   resolved caller, tenant guard, RequirePermission
    database/ pgx pool
    events/   NATS connection, stream setup, outbox
    sse/      per-tenant event broker relayed across replicas
    tenantcache/ tenant lookups through the shared cache
  shared/logger/
```

## Authorization chain

Protected routes live under `/api/v1/{tenant}`. The router applies the global middleware first (request id, trusted real IP, logging, recovery, metrics, CORS and a limit of 300 requests a minute per IP), then runs this chain on every protected route, in this order:

1. **`RequireAuth`** (auth-client v0.20.0). It validates the SSO JWT against the auth-api JWKS. API keys are accepted as well, because `AUTH_ENABLE_API_KEY` defaults to true. A revoked or rotated key is dropped from every validator on the pod when auth broadcasts `apikey.changed`.
2. **`RequireActiveSubscriptionForMutations`**. Reads are never gated. A write from a tenant without an active subscription gets a 403 that ticketing-ui shows as an upgrade prompt.
3. **`httpware.TenantV2`**. It refuses an `X-Tenant-ID` or `X-Tenant-Slug` header that names a tenant other than the token's.
4. **Tenant guard** (`platform/access/tenant.go`). The `{tenant}` segment may be a slug or a UUID. A slug resolves through the tenant cache. A caller who is not a platform owner acts only in the token's tenant, and a segment naming any other tenant gets 403 `tenant_mismatch`. A platform owner may pick any tenant with `?tenantId=<uuid>`, or by the URL segment.
5. **`rbac.ResolveAccess`**. A user acting in their own tenant is provisioned just in time as a `TicketingUser`. A user with no role yet gets the role mapped from their global roles, and a staff role also gets an agent profile. The resolved roles, permissions and agent id go into the request context. Platform owners, superusers and service keys bypass the permission checks. An API key without a user gets the permissions of the role its global roles map to. A platform owner acting in another tenant gets no local user there.
6. **`access.OutletContext`**. It stores the optional `X-Outlet-ID` header. Outlet scoping is optional.
7. **`RequirePermission`** or **`RequireAnyPermission`** on each route. A request that reaches a permission gate without resolved access gets 401, so a route mounted outside the group can never pass by accident.

Global roles map to ticketing roles as follows:

| Global role | Ticketing role |
|---|---|
| `superuser`, `admin` | `ticketing_admin` |
| `manager` | `supervisor` |
| `staff`, `receptionist`, `cashier`, `waiter`, `kitchen`, `bar` | `agent` |
| anything else (member, customer, viewer, rider) | `customer` |

A user with several global roles gets the strongest mapped role. `ticketing_admin`, `supervisor` and `agent` are staff roles and get an agent profile. Grants are cached per pod for 60 seconds, the just-in-time user write happens at most once per user per pod every 5 minutes, and each tenant's system roles are checked at most every 30 minutes.

### Permission catalogue

Codes follow `ticketing.{module}.{action}`. The catalogue is every module with every action, 108 codes in all, and it is upserted on every API start.

| Modules | Actions |
|---|---|
| `tickets`, `comments`, `attachments`, `categories`, `agents`, `teams`, `sla`, `kb`, `routing`, `analytics`, `settings`, `rbac` | `add`, `view`, `view_own`, `change`, `change_own`, `delete`, `delete_own`, `manage`, `manage_own` |

Implication rules, applied within one module (`access.Satisfies`):

- `manage` grants every action in the module.
- `manage_own` grants every `_own` action.
- An action grants its own variant, so `view` grants `view_own` and `change` grants `change_own`.

System roles, seeded for each tenant the first time one of its users signs in:

| Role | Permissions |
|---|---|
| `ticketing_admin` | Every code |
| `supervisor` | `tickets.manage`, `comments.manage`, `kb.manage`, `attachments.view`, `categories.view`, `agents.view`, `agents.change_own`, `teams.view`, `sla.view`, `routing.view`, `analytics.view` |
| `agent` | `tickets.view`, `tickets.add`, `tickets.change`, `comments.add`, `comments.change_own`, `attachments.view`, `categories.view`, `agents.view`, `agents.change_own`, `teams.view`, `kb.view` |
| `customer` | `tickets.add`, `tickets.view_own`, `tickets.change_own`, `comments.add`, `comments.change_own`, `attachments.view_own`, `categories.view`, `kb.view` |

A caller who holds only `tickets.view_own` sees and changes only tickets they raised, may change only the subject and category, never sees internal notes, and posts public comments only. Comment authors always come from the token, never from the request body.

### Local RBAC tables

`ticketing_permissions` holds the catalogue. `ticketing_roles` holds each tenant's roles, `role_permissions` links them, `ticketing_users` holds one row per auth user per tenant, and `user_role_assignments` gives users their roles. Column detail is in [erd.md](erd.md).

Role administration routes:

| Route | Permission |
|---|---|
| `GET /api/v1/{tenant}/ticketing/roles` | `ticketing.rbac.view` |
| `GET /api/v1/{tenant}/ticketing/users` | `ticketing.rbac.view` |
| `PUT /api/v1/{tenant}/ticketing/users/{userId}/roles` with `{"roles": ["agent"]}` | `ticketing.rbac.manage` |

### GET /api/v1/{tenant}/ticketing/auth/me

Any signed-in caller in the tenant may call it. It returns the caller's ticketing access:

```json
{
  "user_id": "auth-user-uuid",
  "email": "agent@example.com",
  "name": "Jane Agent",
  "tenant_id": "tenant-uuid",
  "tenant_slug": "acme",
  "global_roles": ["staff"],
  "service_role": "agent",
  "permissions": ["ticketing.tickets.add", "ticketing.tickets.view"],
  "agent_id": "agent-uuid",
  "is_platform_owner": false
}
```

`service_role` is the strongest role held. A caller who bypasses the checks and holds no role reports `ticketing_admin`. `agent_id` is null for a caller without an agent profile, and `permissions` is always an array.

## Request flows

### Signed-in create

```
POST /api/v1/{tenant}/tickets  (ticketing.tickets.add)
  tickets.Handler.CreateTicket
  tickets.Service.CreateTicket: routing rule, SLA deadlines
  one transaction: next ticket number, ticket row, outbox rows
  outbox publisher sends ticketing.ticket.created to JetStream
  notifications-api emails the requester
```

The requester's contact sits in ticket metadata as `requester_name`, `requester_email` and `requester_phone` on every create path: the signed-in route (the body's `requester_*` fields, or the caller's own email when they raise a ticket for themselves), the internal route and the public form (its OTP-verified email). Public tickets created before 13 November 2026 kept `customer_email` and `customer_name`, and readers still accept those.

### Internal create

`POST /api/v1/internal/tickets` takes `X-API-Key` and `X-Tenant-ID` and no user token. The contract and its callers are in [integrations.md](integrations.md).

### Public support pages

`/api/v1/{tenant}/public/tickets` takes a create with an `X-OTP-Token` from auth-api's public email code, rate limited to 5 creates an hour per IP and 10 a day per verified email. Reading a ticket and replying need either an OTP token for the requester's email or a ticket access token (`POST .../public/tickets/{ticketNumber}/access`). Access tokens are signed with a key derived from `INTERNAL_SERVICE_KEY`, so without that key every public read is refused.

### Live updates

`GET /api/v1/{tenant}/tickets/stream` and `/tickets/{ticketId}/stream` are Server-Sent Events streams that need `ticketing.tickets.view`. The broker keeps per-tenant subscribers and relays events across replicas over NATS. The UI reads the stream with fetch and sends the bearer token in a header.

## Ticket numbers

Numbers are unique per tenant (`TKT-000001` onwards), enforced by the unique index `ticket_tenant_id_ticket_number` on `(tenant_id, ticket_number)`. The create transaction claims the next value from the tenant's `ticket_sequences` row:

```sql
INSERT INTO ticket_sequences (id, tenant_id, next_value, updated_at)
VALUES ($1, $2, 1, now())
ON CONFLICT (tenant_id) DO UPDATE SET next_value = ticket_sequences.next_value + 1, updated_at = now()
RETURNING next_value
```

The row lock is held until the transaction ends, so concurrent creates queue on it and never read the same value. A rolled-back create leaves a gap, never a duplicate. The migration that added the table seeded each tenant's counter from the highest `TKT-` number it already had.

## Transactional outbox

Ticket changes and their events commit in one transaction: the repository writes `outbox_events` rows inside the change's own transaction, whether or not NATS is up. The shared-events background publisher in each API pod relays pending rows to JetStream. The SLA worker writes breach events to the outbox the same way, and the API pods relay them.

## Events

| Subject | Publisher | Consumer |
|---|---|---|
| `ticketing.ticket.created` | outbox | notifications-api, to the requester |
| `ticketing.ticket.assigned` | outbox | notifications-api, to the agent |
| `ticketing.ticket.comment_added` | outbox, public agent replies only | notifications-api, to the requester |
| `ticketing.ticket.status_changed` | outbox, every change except to resolved | notifications-api, to the requester |
| `ticketing.ticket.resolved` | outbox | notifications-api, to the requester |
| `ticketing.sla.first_response_breached`, `ticketing.sla.resolution_breached` | outbox, from the worker | notifications-api, to the agent |
| `crm.activity.log_requested` | outbox | marketflow CRM |
| `auth.user.created`, `auth.user.updated` | auth-api | ticketing-api, queue subscription, provisions or refreshes the ticketing user and agent |
| `tenant.purge` (stream `tenant`) | auth-api (deleted or declined tenant), subscriptions-api (confirmed dormant account) | ticketing-api, the shared `TenantPurger` deletes every row held for the tenant |
| `apikey.changed` (auth broadcast) | auth-api | ticketing-api, drops the key from the validator cache |

Payloads are built in `internal/modules/tickets/events.go` and covered by `events_test.go`. Every ticket event carries `ticket_id`, `ticket_number`, `subject`, `status`, `priority`, `channel`, `source`, `agent_id`, `requester_name`, `requester_email` and `requester_phone`, each key always present and empty when unknown. On top of that:

| Event | Extra keys |
|---|---|
| `ticket.created` | `description` (first 500 characters), `created_at` |
| `ticket.assigned` | `agent_email` |
| `ticket.comment_added` | `comment_id`, `comment` (first 500 characters), `author_id`, `author_type`, `agent_email` |
| `ticket.status_changed` | `old_status`, `new_status` |
| `ticket.resolved` | `old_status`, `new_status` (`resolved`), `resolved_at` |

SLA events carry `ticket_id`, `ticket_number`, `subject`, `priority`, `status`, `agent_id`, `agent_email`, `team_id`, `response_breached`, `resolution_breached` and `event_type`, and never the requester's contact.

## Support routing

notifications-api emails the requester from the contact on the event: the receipt, every public agent reply, a status change that needs them (waiting for their reply, closed, cancelled, reopened) and the resolution. The mail goes out under the tenant's name with the tenant's support address as Reply-To (auth-api tenant metadata `support`, read with `cache.ResolveSupportContact`, falling back to its contact email). Platform tickets on the codevertex tenant use support@codevertexafrica.com. A requester with no email gets nothing, never a copy in the tenant's mailbox. SMS and WhatsApp to the requester are not sent yet.

## Inbound email and WhatsApp

Not built yet. A message to a support address or a WhatsApp number does not open a ticket today. Tickets are opened by the signed-in route, the public form and the internal route.

The design is set (owner decision, 2026-11-16) and is built in [sprint 11](sprints/sprint-11-channels.md):

- **Tenant email.** Each tenant gets a forwarding address `<slug>@support.codevertexafrica.com` on our Stalwart mail server. The tenant sets an auto-forward from its own support mailbox, so ticketing never stores a tenant mailbox password. ticketing reads the catch-all support domain over JMAP, maps the local part to the tenant, opens a ticket or threads a reply (`In-Reply-To`, `References`, then a `[TKT-n]` subject tag) and dedupes on the Message-ID. The tenant's `support` address stays the Reply-To on outbound mail.
- **Platform email.** `support@codevertexafrica.com` is already its own Stalwart mailbox, no longer an info@ alias. Sprint 11 reads it over JMAP for the codevertex tenant on the same path. info@ stays a human inbox.
- **WhatsApp.** notifications-api publishes a new `notifications.whatsapp.tenant_inbound` event for messages to a tenant's own number. ticketing consumes it on a durable queue, threads it onto the requester's open WhatsApp ticket or opens one, dedupes on the WhatsApp message id and ignores STOP replies. Auto-ticketing is a tenant opt-in, off by default, and the notifications inbox gains a Convert to ticket action.
- **Ingest rules.** No-reply senders, auto-replies (`Auto-Submitted`, `X-Autoreply`, bounce DSNs) and mail flagged by ClamAV never open a ticket. Every requester resolves to a marketflow contact.

## Migrations

Schema changes are versioned Atlas migrations in `internal/ent/migrate/migrations`, embedded in the `ticketing-migrate` binary and recorded in `atlas_schema_revisions`.

- `scripts/entrypoint.sh` runs `ticketing-migrate` at every pod start, before the API, and retries up to 60 times at 5 second intervals while printing each failure.
- The run holds a Postgres advisory lock over `POSTGRES_MIGRATE_URL` (the direct URL, because PgBouncer transaction pooling drops a session lock), so two pods starting together apply each file once and a pod with nothing pending starts at once.
- Each file runs with its revision record in a single transaction, so a failed statement leaves neither partial DDL nor a partial revision.
- **Baseline rule.** `20261116090000_baseline.sql` is the schema that Ent auto-migrate built up to commit c0d863b. A database that has the `tickets` table but no revision history records the baseline as applied without running it, and only later files run. Any other database without history is new and runs every file.
- `20261116090100_phase0.sql` adds per-tenant ticket numbers, the internal-create idempotency index, `tenant_id` on comments and attachments, agents unique per tenant and the RBAC tables.
- `RESET_DB=true` drops and recreates the public schema before migrating. It exists for local development only.

The API never auto-migrates. In production (commit 4f03d8e, deployed 2026-11-16) the baseline was recorded as applied, `20261116090100_phase0.sql` ran, and the API seeded the 108 permission codes on start.

## SLA worker

`ticketing-worker` scans every 5 minutes for tickets past a response or resolution deadline, 200 at a time. Each tick is claimed fleet-wide through the shared cache `RunOnce` (key `ticketing:sla-breach-scan` per period), so a second replica or a restart inside the period never repeats a scan. Without Redis (local development) a single worker runs every tick. Each breach is recorded once, with its event, in one transaction per ticket. The API pods never run the scan.

Deadlines are wall-clock minutes from creation today. The policy's business-hours flags and `business_hours` map are stored but not applied yet (sprint 10).

## Health and metrics

| Endpoint | Behaviour |
|---|---|
| `GET /healthz` | Liveness, always `OK`. |
| `GET /readyz` | Pings Postgres, Redis and NATS within 3 seconds and returns each result. Postgres is required: without it the pod answers 503 and leaves the load balancer. Redis and NATS are reported but do not take the pod out, because the service degrades without them (per-pod limits and caches, events held in the outbox). |
| `GET /api/v1/status` | Signed-in dependency summary. |
| `GET /metrics` on port 9090 | Prometheus text format on its own listener, so metrics never go out through the ingress. It serves the Go and process collectors plus `ticketing_http_requests_total` (method, route pattern, status) and `ticketing_http_request_duration_seconds` (method, route pattern). The route label is chi's matched pattern, never the raw path. |

CI (`.github/workflows/deploy.yml`) runs `go vet ./...` and `go test -timeout 300s ./...` before the image build.

## Shared libraries

| Library | Version | Used for |
|---|---|---|
| `github.com/Bengo-Hub/auth-client` (imported as `shared-auth-client`) | v0.20.0 | `RequireAuth`, API keys, claims, `RequireActiveSubscriptionForMutations` |
| `github.com/Bengo-Hub/httpware` | v0.20.0 | `TenantV2`, request id, logging, recovery, CORS, `ValidServiceKey`, outlet header |
| `github.com/Bengo-Hub/shared-events` | v0.11.1 | Outbox publisher, queue subscriptions, broadcaster, `TenantPurger` |
| `github.com/Bengo-Hub/cache` | v0.5.10 | Redis client, tenant cache, `RunOnce`, local caches |
| `github.com/Bengo-Hub/pagination` | v0.5.0 | `Parse` and `NewResponse` on every list (offset paging today, keyset in sprint 9) |
| `github.com/Bengo-Hub/shared-ratelimit` | v0.3.1 | Global and public limits, `TrustedRealIP` |
| `entgo.io/ent` | v0.14.5 | Schema and queries |
| `ariga.io/atlas` | v0.32.1 pre-release | Migration executor |

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `POSTGRES_URL` | required | PostgreSQL DSN used by the API and worker |
| `POSTGRES_MIGRATE_URL` | `POSTGRES_URL` | Direct DSN for migrations and seed, bypassing PgBouncer |
| `POSTGRES_MAX_OPEN_CONNS`, `POSTGRES_MAX_IDLE_CONNS` | 6, 3 | Pool limits for the shared database server |
| `REDIS_URL` or `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB` | `localhost:6379` | Redis |
| `EVENTS_NATS_URL` | `nats://localhost:4222` | NATS |
| `NATS_STREAM` | `ticketing` | JetStream stream name |
| `AUTH_SERVICE_URL` | `http://localhost:8080` | auth-api base URL (JWKS, API keys, OTP, tenants) |
| `AUTH_ISSUER` | `https://sso.codevertexafrica.com` | JWT issuer |
| `AUTH_AUDIENCE` | `codevertex` | JWT audience |
| `AUTH_ENABLE_API_KEY` | `true` | Accept auth-api API keys on protected routes |
| `INTERNAL_SERVICE_KEY` | empty | S2S key for internal routes, also the root of the ticket access token key |
| `TENANT_CACHE_TTL` | `5m` | Tenant cache lifetime |
| `HTTP_PORT` | `4009` | API port |
| `METRICS_PORT` | `9090` | Metrics port |
| `HTTP_ALLOWED_ORIGINS` | fleet UI origins | CORS allow list |
| `RESET_DB` | unset | Migrate only: drop the public schema first (local use) |

## Seed

`cmd/seed` (`/app/ticketing-seed` in the image) is idempotent and per tenant, and writes only what the tenant does not have yet. The entrypoint does not run it.

| Variable | Required | Description |
|---|---|---|
| `TENANT_ID` | yes | The tenant's auth UUID |
| `KB_SET` | no | Knowledge base set to load from `cmd/seed/kb/<set>/*.json` (for example `hadia`) |
| `SEED_SCOPE` | no | `kb` loads only articles and leaves categories and SLA policies alone; default `all` |

Articles are published and public, with author `00000000-0000-0000-0000-000000000000` and `metadata.seeded_from = ticketing-seed`. An article whose slug the tenant already has is never changed, even if staff edited, archived or deleted it. Each JSON file holds `slug`, `title`, `excerpt`, `category`, `tags` and `content_html`, and `kb_test.go` checks every set before it ships.

To seed one tenant in the cluster I run a one-off Job with the deployment's image and its `POSTGRES_URL` and `POSTGRES_MIGRATE_URL` secret references, plus `TENANT_ID`, `KB_SET` and `SEED_SCOPE=kb`.
