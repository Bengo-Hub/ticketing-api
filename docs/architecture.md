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
| AI models, prompt guardrails | marketflow-ai (Vera); ticketing builds the prompts from its own data |
| Prepaid AI credit wallet and the AI exemption | subscriptions-api |
| Attachment files | object storage (upload is planned in sprint 9) |

## Processes

The image holds four binaries, built by the `Dockerfile`.

| Binary | Role |
|---|---|
| `ticketing-api` | HTTP API on `HTTP_PORT` (default 4009) and Prometheus metrics on `METRICS_PORT` (default 9090). It also drains the outbox to JetStream. |
| `ticketing-migrate` | Applies the versioned migrations. `scripts/entrypoint.sh` runs it before the API starts. |
| `ticketing-worker` | The SLA scans (breaches, pre-breach warnings and escalations) and the analytics rollup. It runs as its own Deployment and is the only process that runs them. |
| `ticketing-seed` | Per-tenant seed of categories, SLA policies, the default business calendar and knowledge base sets. It is a manual step. |

## Module layout

```
cmd/
  api/        HTTP server
  migrate/    versioned migrations under the advisory lock
  worker/     SLA breach, warning and escalation scans, the analytics rollup
  seed/       per-tenant seed and knowledge base sets (kb/<set>/*.json)
internal/
  app/        wiring of every dependency
  clients/    otp.go (auth-api OTP), members.go (auth-api members), vera.go (marketflow-ai), credits.go (subscriptions-api AI credits)
  config/     envconfig settings
  ent/        Ent schema and generated code; migrate/migrations holds the SQL files
  http/
    handlers/ health, readiness, status
    metrics/  Prometheus collectors and middleware
    router/   chi router and the auth chain
  modules/
    ai/       AI with Vera: classify, summary, suggested reply, KB suggestions, KB draft, credit metering, after-commit auto-classify
    analytics/ rollup job, /analytics views, export through the shared reports module
    identity/ teams, agents, auth.user.* consumer
    ingest/   channel ingest: email and WhatsApp consumers, tenant from recipient, threading, dedupe
    kb/       knowledge base articles
    rbac/     permission catalogue, roles, ticketing users, auth/me
    routing/  routing rules
    settings/ per-tenant settings document (tenant_settings), GET and PUT /settings/channels and /settings/ai
    sla/      SLA policies, business calendars, deadline calculation, the SLA report
      clock/  business-time arithmetic, Kenyan holidays, policy settings (no internal imports)
    tickets/  tickets, comments, attachments, categories, public and internal routes, CSAT surveys, SSE, outbox
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

`ticketing_permissions` holds the catalogue. `ticketing_roles` holds each tenant's roles, `role_permissions` links them, `ticketing_users` holds one row per auth user per tenant, `user_role_assignments` gives users their roles and `audit_logs` records every role and user change. Column detail is in [erd.md](erd.md).

### Role and user administration

Ported from treasury-api in sprint 14, with treasury's paths, JSON field names and rules. Every route sits under `/api/v1/{tenant}/ticketing`. Reads need `ticketing.rbac.view` and changes need `ticketing.rbac.manage`; treasury does not split users from roles, so there is no `ticketing.users.*` module. On top of `rbac.manage`, creating, customizing, cloning, editing or deleting a role, changing its permissions, removing a role from someone and adding a user need an organisation admin (the token's `admin` or `superuser` role, or a platform owner), so a manager cannot widen a role and then hand it out. A platform owner acts in any tenant with `?tenantId=`. A service key has no person behind it and is refused.

| Route | Gate | Body | Answer |
|---|---|---|---|
| `GET /roles` | view | | `{"roles": [RoleView]}` |
| `POST /roles` | manage, admin | `{role_code?, name, description?, permission_codes?}` | 201 `Role` |
| `POST /roles/customize` | manage, admin | `{role_code}` | 200 `Role` |
| `POST /roles/{id}/clone` | manage, admin | `{role_code?, name, description?}` | 201 `Role` |
| `PATCH /roles/{id}` | manage, admin | `{name?, description?}` | 200 `Role` |
| `DELETE /roles/{id}` | manage, admin | | 204 |
| `GET /roles/{id}/permissions` | view | | `{"permission_codes": [...]}` |
| `PUT /roles/{id}/permissions` | manage, admin | `{permission_codes}` (required) | 200 `Role` |
| `GET /permissions` | view | | `{"permissions": [Permission]}` |
| `POST /users` | manage, admin | `{email, tenant_slug?, roles?}` | 201 `{user_id, email, tenant_id, created}` |
| `GET /users` | view | `?search=&limit=&page=` | page of `UserView` |
| `GET /users/{id}` | view | | `User` |
| `GET /users/{id}/roles` | view | | `{"roles": [Role]}` |
| `POST /users/{id}/roles` | manage | `{role_id, expires_at?}` | 201 `Role` |
| `DELETE /users/{id}/roles/{role_id}` | manage, admin | | 204 |
| `GET /rbac/audit-logs` | view | `?resource_type=&user_id=&action=&from=&to=&limit=&page=` | page of audit entries |

Pages use `github.com/Bengo-Hub/pagination` (`{data, total, limit, page, hasMore}`). A user `{id}` may be the ticketing user id or the auth-api user id.

Rules:

- **System roles.** The four system roles are seeded per tenant and kept in step with their definitions. They are listed with `scope` `global`. A system role cannot be deleted (409). Editing one, through `PATCH`, `PUT /permissions` or `POST /roles/customize`, customizes it: the row keeps its id and code, `is_system_role` turns false, `scope` becomes `tenant`, `is_customized` is true, and the seeder leaves it alone from then on. Deleting a customized role (once nobody holds it) brings the default back at the next seed. A tenant role cannot take a system code.
- **Grants.** `POST /users/{id}/roles` grants a role or renews it, with an optional end date in the future. A caller who is not an organisation admin may give only a role whose every permission they hold themselves. Nobody but a platform owner may give an admin-equivalent role (`admin`, `owner`, `superuser` and their spellings). A staff role gets the person an agent profile.
- **Revokes.** Removing a role ends the assignment now and keeps the row, so sign-in never gives the role back.
- **Self-lockout.** Nobody but a platform owner or superuser may remove their own last way to hold `ticketing.rbac.manage`, by revoke or by editing a role's permissions (409).
- **Adding a user.** `POST /users` calls auth-api's S2S member add (`POST /api/v1/s2s/tenants/{tenant_id}/members` with the internal service key), which finds or creates the SSO account by email; ticketing then records the ticketing user. Ticketing stores no password. Roles in the body are tenant membership roles sent to auth-api (default `staff`); admin-equivalent ones are refused. The ticketing role comes from sign-in or an admin grant.
- **Audit.** Every change writes an `audit_logs` row; a failed write is logged and never blocks the change. `GET /rbac/audit-logs` lists only resource types `role` and `user` of the acting tenant; `from` and `to` are East Africa Time days.
- **Caches.** A change drops this pod's cached grants at once; other pods catch up within 60 seconds.

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

`/api/v1/{tenant}/public/tickets` takes a create with an `X-OTP-Token` from auth-api's public email code, rate limited to 5 creates an hour per IP and 10 a day per verified email. Reading a ticket and replying need either an OTP token for the requester's email or a ticket access token (`POST .../public/tickets/{ticketNumber}/access`). Access tokens are signed with a key derived from `INTERNAL_SERVICE_KEY`, so without that key every public read is refused. The CSAT survey routes `POST /api/v1/{tenant}/public/csat/check` and `POST /api/v1/{tenant}/public/csat` take only the survey token and are limited to 20 calls an hour per IP.

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

Ticket changes and their events commit in one transaction: the repository writes `outbox_events` rows inside the change's own transaction, whether or not NATS is up. The shared-events background publisher in each API pod relays pending rows to JetStream. The SLA worker writes breach, warning and escalation events to the outbox the same way, and the API pods relay them.

## Events

| Subject | Publisher | Consumer |
|---|---|---|
| `ticketing.ticket.created` | outbox | notifications-api, to the requester |
| `ticketing.ticket.assigned` | outbox | notifications-api, to the agent |
| `ticketing.ticket.comment_added` | outbox, public agent replies only | notifications-api, to the requester |
| `ticketing.ticket.status_changed` | outbox, every change except to resolved | notifications-api, to the requester |
| `ticketing.ticket.resolved` | outbox | notifications-api, to the requester |
| `ticketing.ticket.csat_requested` | outbox, on resolve when the requester has an email | notifications-api, the survey email to the requester (consumer not built yet) |
| `ticketing.sla.first_response_breached`, `ticketing.sla.resolution_breached` | outbox, from the worker | notifications-api, to the agent |
| `ticketing.sla.breach_warning` | outbox, from the worker, once per ticket and target | notifications-api, to the agent (consumer not built yet) |
| `ticketing.ticket.escalated` | outbox, from the worker, once per escalation level | notifications-api, to the agent or team (consumer not built yet) |
| `crm.activity.log_requested` | outbox | marketflow CRM |
| `auth.user.created`, `auth.user.updated` | auth-api | ticketing-api, queue subscription, provisions or refreshes the ticketing user and agent |
| `tenant.purge` (stream `tenant`) | auth-api (deleted or declined tenant), subscriptions-api (confirmed dormant account) | ticketing-api, the shared `TenantPurger` deletes every row held for the tenant |
| `apikey.changed` (auth broadcast) | auth-api | ticketing-api, drops the key from the validator cache |
| `email.inbound.support` (stream `INGEST_EMAIL_STREAM`, default `email`) | email-provisioner, once per message read from the support mailboxes | ticketing-api, durable and deliver group `ticketing-email-ingest` |
| `notifications.whatsapp.tenant_inbound` (stream `notifications`) | notifications-api, once per message to a tenant's own number | ticketing-api, durable and deliver group `ticketing-whatsapp-ingest` |

Payloads are built in `internal/modules/tickets/events.go` and covered by `events_test.go`. Every ticket event carries `ticket_id`, `ticket_number`, `subject`, `status`, `priority`, `channel`, `source`, `agent_id`, `requester_name`, `requester_email` and `requester_phone`, each key always present and empty when unknown. On top of that:

| Event | Extra keys |
|---|---|
| `ticket.created` | `description` (first 500 characters), `created_at` |
| `ticket.assigned` | `agent_email` |
| `ticket.comment_added` | `comment_id`, `comment` (first 500 characters), `author_id`, `author_type`, `agent_email` |
| `ticket.status_changed` | `old_status`, `new_status` |
| `ticket.resolved` | `old_status`, `new_status` (`resolved`), `resolved_at` |
| `ticket.csat_requested` | `resolved_at`, `survey_channel` (`email`), `csat_url`, `csat_token`, `csat_expires_at` |

Every ticket event also carries what an email about the ticket needs to thread, so the customer's reply comes back to the same ticket:

| Key | Value |
|---|---|
| `email_message_token` | The local part notifications-api should give this email's Message-ID: the ticket id on `ticket.created`, the comment id on `ticket.comment_added`, empty on other events |
| `email_thread_token` | The ticket id, the token of the receipt email and so the root of the References chain |
| `email_in_reply_to` | The customer's last Message-ID on the ticket in angle brackets, or empty |
| `email_references` | The customer's Message-IDs on the ticket, oldest first, in angle brackets (at most 20) |
| `email_subject_tag`, `email_reply_subject` | `[TKT-000042]` and `Re: <subject> [TKT-000042]` |

SLA events and `ticket.escalated` carry `ticket_id`, `ticket_number`, `subject`, `priority`, `status`, `agent_id` and `team_id`, and never the requester's contact. On top of that:

| Event | Extra keys |
|---|---|
| `sla.first_response_breached`, `sla.resolution_breached` | `agent_email`, `response_breached`, `resolution_breached`, `event_type` |
| `sla.breach_warning` | `agent_email`, `target` (`first_response` or `resolution`), `due_at`, `minutes_left`, `event_type` |
| `ticket.escalated` | `escalation_level`, `escalated_at`, `old_priority`, `old_team_id`, `target`, `when`, `minutes`, `event_type`; `priority`, `team_id` and `agent_id` are the values after the escalation |

## Support routing

notifications-api emails the requester from the contact on the event: the receipt, every public agent reply, a status change that needs them (waiting for their reply, closed, cancelled, reopened) and the resolution. The mail goes out under the tenant's name with the tenant's support address as Reply-To (auth-api tenant metadata `support`, read with `cache.ResolveSupportContact`, falling back to its contact email). Platform tickets on the codevertex tenant use support@codevertexafrica.com. A requester with no email gets nothing, never a copy in the tenant's mailbox. SMS and WhatsApp to the requester are not sent yet.

## Inbound email and WhatsApp

Built in [sprint 11](sprints/sprint-11-channels.md) on the ticketing side. Every channel goes through one service, `internal/modules/ingest`, which runs in the API pods because it opens tickets through the same `CreateTicket` as every other path: numbering, routing, SLA deadlines, the outbox events and the live SSE update all apply.

- **Mail ownership.** email-provisioner is the platform's only JMAP client and holds the Stalwart credentials; ticketing has none. It reads `support@codevertexafrica.com` and the `support.codevertexafrica.com` catch-all, stores attachments in the private bucket through httpware/objectstore and publishes `email.inbound.support` once per message.
- **Tenant from the recipient.** The envelope recipient (then To and Cc) decides the tenant. `support@codevertexafrica.com` is the codevertex tenant; `<slug>@support.codevertexafrica.com` (a `+tag` is ignored) is the tenant with that slug, looked up through the tenant cache. Mail to any other address, or to a slug auth-api does not know, is acknowledged and logged, never retried. An auth-api outage is retried.
- **Threading, email.** In order: a Message-ID in `In-Reply-To` or `References` that ticketing sent (its local part is a ticket or comment id, from `email_message_token`) or received (a customer email already on a ticket); then a `[TKT-n]` subject tag, only when the sender is that ticket's requester, because a tag can be guessed; else a new ticket.
- **Threading, WhatsApp.** The sender's most recently active WhatsApp ticket that is not closed or cancelled and saw activity in the last 7 days (`INGEST_WHATSAPP_THREAD_WINDOW`). The phone is normalised to E.164 through httpware/contact.
- **Open or continue.** A message that continues a ticket becomes a public customer comment, in one transaction with its attachments, the Message-ID added to the ticket's thread (`email_message_ids` in metadata) and, for a ticket waiting on the customer or resolved, the reopen. A reply to a closed or cancelled ticket opens a follow-up ticket with `follow_up_of` in its metadata.
- **Dedupe.** A ticket opened from a message carries it as `source` and `source_ref` (`email.inbound` with type `email_message`, or `notifications.whatsapp` with type `wa_message`, the source the inbox's Convert to ticket also uses), under the existing unique index `ticket_tenant_source_ref`. A comment carries `channel` and `external_id` in metadata under the unique index `ticketcomment_tenant_channel_external_id`. A redelivered message is found before anything is written, and a race between replicas loses on the unique index.
- **Ingest rules.** Nothing is opened or continued from our own addresses, no-reply and daemon senders, `Auto-Submitted` other than `no`, `X-Autoreply`, bulk or list `Precedence`, delivery status reports, auto-reply subjects (out of office, automatic reply, undeliverable), mail flagged as spam or carrying a virus, or a WhatsApp STOP reply. Attachments are recorded only when their key is in the tenant's own `attachments` prefix and not reported infected.
- **WhatsApp opt-in.** `channels.whatsapp.auto_ticket` in the tenant settings, off by default. Off, a message still continues an open WhatsApp ticket (for example one an agent made with Convert to ticket) but never opens one.
- **Data ownership.** The requester is a marketflow contact. A ticket keeps only `requester_name`, `requester_email` and `requester_phone` in metadata, as every create path does.
- **Acknowledgement.** Both consumers use explicit acks and `MaxDeliver` 5. A permanent failure is acknowledged and logged; a transient one is retried with a growing delay.

## AI with Vera

The AI routes and their shapes are in [ai.md](ai.md). The design in short:

- **Where the model runs.** `internal/clients/vera.go` calls marketflow-ai with the internal service key and `X-Tenant-ID`. Classification uses marketflow-ai's ticket triage route; the other four features use its generic task route (`/api/v1/agent/run`) with ticketing's own instructions, the ticket data as the run's context and no tools. marketflow-ai's guardrails (data fencing, masking of emails and phone numbers, the 0.8 confidence cap on injected tickets) apply to every call, so ticketing adds no screening of its own.
- **What the model reads.** Only the tenant's own records, loaded under the caller's tenant: the ticket (number, subject, description, status, priority, channel), its newest messages as plain text with caps, and for KB suggestions up to 20 of the tenant's own published articles. Requester contact details are never sent. A suggested reply and a KB draft read the public thread only.
- **How it is paid.** `internal/modules/ai/meter.go`, for every call: a kept answer for the same request key is returned first; otherwise subscriptions-api's credit check for one AI credit, then the model, then one spend under the key `ticketing:ai:<feature>:<ticket or draft>:<request key>`. A refused check is 402 `ai_credits_exhausted` and the model never runs. A wallet that cannot be read stops the call (fail closed). A failed model call is never charged. The answer is kept in Redis for 24 hours under the tenant and the key, so a retry costs nothing and gets the same answer.
- **Exemption.** subscriptions-api answers `exempt` for the platform owner only, and nothing is spent. There is no local exemption check, so the demo tenant pays like any other.
- **Never blocking.** Ticket create and update never call AI. `tickets.Service` calls its `CreatedObserver` on a goroutine after the create committed, with panics contained; the AI service is that observer and runs auto-classify only when the tenant set `ai.auto_classify`, with a 45 second limit and 4 at a time per pod.

## Migrations

Schema changes are versioned Atlas migrations in `internal/ent/migrate/migrations`, embedded in the `ticketing-migrate` binary and recorded in `atlas_schema_revisions`.

- `scripts/entrypoint.sh` runs `ticketing-migrate` at every pod start, before the API, and retries up to 60 times at 5 second intervals while printing each failure.
- The run holds a Postgres advisory lock over `POSTGRES_MIGRATE_URL` (the direct URL, because PgBouncer transaction pooling drops a session lock), so two pods starting together apply each file once and a pod with nothing pending starts at once.
- Each file runs with its revision record in a single transaction, so a failed statement leaves neither partial DDL nor a partial revision.
- **Baseline rule.** `20261116090000_baseline.sql` is the schema that Ent auto-migrate built up to commit c0d863b. A database that has the `tickets` table but no revision history records the baseline as applied without running it, and only later files run. Any other database without history is new and runs every file.
- `20261116090100_phase0.sql` adds per-tenant ticket numbers, the internal-create idempotency index, `tenant_id` on comments and attachments, agents unique per tenant and the RBAC tables.
- `20261117090000_sprint10.sql` adds `business_calendars`, replaces the policy business-hours flags and map with `calendar_id`, adds `sla_policy_id`, `sla_paused_at`, `sla_paused_minutes`, `sla_first_response_minutes` and `sla_resolution_minutes` to tickets, and gives open tickets with deadlines the policy that set them.
- `20261117120000_sprint11.sql` adds `tenant_settings` (one settings document per tenant), the unique expression index `ticketcomment_tenant_channel_external_id` that stores each channel message once, and the expression index `ticket_tenant_requester_phone` behind the WhatsApp threading lookup.
- `RESET_DB=true` drops and recreates the public schema before migrating. It exists for local development only.

The API never auto-migrates. In production (commit 4f03d8e, deployed 2026-11-16) the baseline was recorded as applied, `20261116090100_phase0.sql` ran, and the API seeded the 108 permission codes on start.

## SLA worker

`ticketing-worker` runs three SLA scans every minute, then the analytics rollup (see Analytics rollups). Each scan of each tick is claimed fleet-wide through the shared cache `RunOnce` under its own period key (`ticketing:sla-breach-scan`, `ticketing:sla-warning-scan`, `ticketing:sla-escalation-scan`), so a second replica or a restart inside the period never repeats one. Without Redis (local development) a single worker runs every tick. The API pods never run a scan. Every scan skips deleted tickets, closed statuses and tickets whose clock is paused, and records each change with its event in one transaction per ticket.

- **Breaches**, 200 per tick: tickets past a response or resolution deadline. Each breach is recorded once.
- **Warnings**, 200 per tick: targets due within the policy's `notify_before_breach_min`. The lead is wall-clock minutes before the deadline. The ticket's `metadata.sla_warnings.<target>` records the warning, and the update only matches an unwarned target, so each target is warned once.
- **Escalations**, 100 per tick: tickets whose next rule (the rule at index `escalation_level`) has fired. The update only matches the expected level, so each level is applied once.

The warning and escalation scans join `sla_policies` on `(id, tenant_id)` and read each deadline through its own index within a bounded range: up to 7 days ahead, and for escalations no further back than 30 days, so old tickets are never read.

## SLA clocks

**Calendars.** `business_calendars` holds a tenant's working week (`weekly_hours`, periods per weekday in local time), its holidays (a JSON list of dates with names, `moon_dependent` marking dates to confirm) and an IANA timezone, `Africa/Nairobi` by default. A tenant's first calendar becomes its default and is seeded with the Kenyan public holidays of the current and the next year: New Year, Good Friday and Easter Monday (computed), Labour Day, Madaraka Day, Mazingira Day (10 October, called Utamaduni Day until the 2024 amendment), Mashujaa Day, Jamhuri Day, Christmas and Boxing Day, a holiday falling on a Sunday also observed on the next free day, and Idd ul-Fitr flagged as moon-dependent for the years with a published date (2026 and 2027). `ticketing-seed` creates this calendar for a tenant that has none.

**Policies.** A policy's `calendar_id` makes both targets count business minutes in that calendar; a null `calendar_id` counts every minute. The business-hours flags and the `business_hours` map are gone. Policy metadata carries the settings the worker reads, validated on save:

| Key | Meaning |
|---|---|
| `notify_before_breach_min` | Minutes before a deadline to publish `ticketing.sla.breach_warning`, 0 to 10080 |
| `pause_on_pending_internal` | `true` stops the clock in `pending_internal` too |
| `escalation_rules` | Up to 5 rules, applied in order: `target` (`first_response` or `resolution`), `when` (`before_breach` or `after_breach`), `minutes`, `raise_priority`, `team_id` |

An escalation raises the priority one level (up to critical), assigns the rule's team and clears the agent, writes `escalation_level` and `escalated_at`, leaves an internal system note and publishes `ticketing.ticket.escalated`. A team deleted since the rule was saved is skipped.

**Deadlines.** On create the matching policy's targets are added to the creation time in business minutes (`internal/modules/sla/clock`), skipping nights, days without working periods and holidays, and the ticket keeps `sla_policy_id`.

**Pause.** `pending_customer` stops the clock, and so does `pending_internal` when the policy says so. Entering the status sets `sla_paused_at`. Leaving it measures the pause in the policy's clock, moves each deadline not yet met by that many business minutes, adds them to `sla_paused_minutes` and clears `sla_paused_at`. A ticket resolved straight from a pending status restarts first, so its breach check uses the moved deadline.

**Elapsed time.** The first public agent reply writes `sla_first_response_minutes` and resolving writes `sla_resolution_minutes`: the minutes since creation in the policy's clock, less every pause.

## SLA report

`GET /api/v1/{tenant}/sla/reports?from&to&team_id&agent_id&priority` (`ticketing.analytics.view`) covers the tickets created in `[from, to)`, by default the last 30 days and at most 366. One `GROUP BY GROUPING SETS ((priority), (assigned_team_id), ())` query over the `(tenant_id, created_at, id)` index returns the overall, per-priority and per-team rows; no ticket row is loaded into Go. A target counts as met when it was answered or resolved without a breach and as breached when its flag is set; tickets still inside their deadline are in neither count.

```json
{
  "from": "2026-10-18T00:00:00Z",
  "to": "2026-11-17T00:00:00Z",
  "overall": {
    "tickets": 120,
    "first_response": {"met": 104, "breached": 6, "compliance_pct": 94.5, "avg_minutes": 38.2, "p90_minutes": 95},
    "resolution": {"met": 88, "breached": 9, "compliance_pct": 90.7, "avg_minutes": 412.5, "p90_minutes": 960}
  },
  "by_priority": [{"priority": "high", "tickets": 30, "first_response": {}, "resolution": {}}],
  "by_team": [{"team_id": null, "tickets": 12, "first_response": {}, "resolution": {}}]
}
```

`compliance_pct`, `avg_minutes` and `p90_minutes` are null when nothing was measured. The minutes are business minutes in each ticket's policy clock, net of pauses.

## Analytics rollups

The `/analytics` routes read period figures only from two rollup tables; the contract is in [analytics.md](analytics.md). Each figure is dated by its own moment: created by `created_at`, resolved by `resolved_at`, first responses by `first_response_at`, a breach by its deadline and CSAT by `submitted_at`.

- **`ticket_rollups`, the fine cells.** One tenant's figures for one hour or one UTC day and one combination of team, agent, category, channel and priority, with a missing team, agent or category stored as the zero UUID so the cell key has no nulls. At 200k tickets a year this is about 830k rows, roughly one per moment a ticket counts at, so a query that sums them without a narrow filter is slow.
- **`ticket_rollup_dims`, the compact series.** Per tenant and bucket, one total row (`dim_kind` `all`) and one row per value of each single dimension, the `GROUPING SETS ((), (team_id), (agent_id), (category_id), (channel), (priority))` of the fine cells. Day rows also hold the team pairs, `(team_id, agent_id)`, `(team_id, category_id)`, `(team_id, channel)` and `(team_id, priority)`, with the team in `team_id` (`dim_kind` `team_agent` and so on), so a long window with a team filter reads its breakdowns from them. Hour rows have no pairs: short windows read few fine cells. A year of totals is 365 day rows; a year of one team's priority breakdown is about 365 rows per priority.

**Which table each query reads.** The repository picks the cheapest from the filters:

| Query | No filter | One filter | Team and one other filter | Any other two or more filters |
|---|---|---|---|---|
| Totals (current and previous period), trend | series `all`, `ticketrollupdim_key` | series of that filter's value, `ticketrollupdim_key` | windows over 31 days: the team pair series, `ticketrollupdim_key`; shorter: fine cells, the team pair index (`ticketrollup_team_<dim>`; the agent index for team and agent) | fine cells, the narrowest filter index |
| Breakdown by D | series of kind D, `ticketrollupdim_pair` | D filtered: its series, `ticketrollupdim_key`. Team filter over 31 days: the team pair series of kind `team_D`, `ticketrollupdim_pair`. Otherwise fine cells, the filter's index | D is the team or the other filtered dimension: the pair row, `ticketrollupdim_key`. A third D: fine cells, the team pair index | fine cells, one `GROUPING SETS` pass |

Windows up to 31 days read hour rows and longer ones day rows, so a series read is at most about 744 hour rows or 367 day rows per value. The fine-cell paths only run under a filter, and a team with a second filter has its own composite index, so they read only the cells of both values. The routes read each table once per figure: the current and previous totals are two series reads of a few hundred rows, and all the fine-cell breakdowns of a request share one `GROUPING SETS` pass.

`ticketing-worker` fills both every minute under `RunOnce` (`ticketing:analytics-rollup`), after the SLA scans. One step, in one transaction:

1. Locks the `ticket_rollups` row of `rollup_watermarks` (`FOR UPDATE`), so two runs never interleave.
2. Reads the next 2000 tickets with `(updated_at, id)` past the watermark and `updated_at` more than two minutes old, across tenants, through the tickets `(updated_at, id)` index. Deleted tickets are read too.
3. Works out each ticket's hours now (creation, resolution, first response, both deadlines, its CSAT responses; none for a deleted ticket) and adds the hours it counted in at the last step, from `ticket_rollup_marks`. A deadline that moved or a second resolution therefore leaves no ticket behind in an old cell.
4. Per tenant, deletes those hour cells and rebuilds them from `tickets` and `csat_responses` with one `INSERT ... SELECT ... GROUP BY ... ON CONFLICT DO UPDATE`. Each moment is a range scan of its own `(tenant_id, moment)` index per hour.
5. Deletes and rebuilds the same hours' series rows from the fine cells just written, in one `GROUPING SETS` pass.
6. Rebuilds the UTC-day fine cells of those hours from their hour cells, then the day series rows, team pairs included, from those day cells in one `GROUPING SETS` pass.
7. Stores the new marks and moves the watermark to the last ticket read; a step that read less than a full batch sets `caught_up_at`.

A run steps until it reaches the two-minute horizon or has spent 40 seconds, so the first run after deploy (the backfill, from the epoch) spreads over as many ticks as it needs. A failed step rolls back whole and the next tick repeats it. Rebuilding a bucket replaces it, so a repeated step gives the same rows, and the series always agree with the fine cells because they are derived from them in the same transaction. Ticket writes all move `updated_at`, including a CSAT submit, which is how a new response reaches its hour.

The routes add live figures counted on `tickets`. The open backlog by status, with overdue and unassigned, and the open count per agent are an index-only scan of the partial index `ticket_open_backlog` (`(tenant_id, status, assigned_team_id, assigned_agent_id, category_id, channel, priority, sla_resolution_due_at, sla_paused_at)` over undeleted tickets in an open status), which holds every filter column, so a filtered backlog never visits the heap either. Today's created and resolved use the `(tenant_id, created_at, id)` and `(tenant_id, resolved_at)` indexes. The old `GET /tickets/stats` and `GET /tickets/analytics` are removed; their figures are in `GET /analytics/tickets`.

`internal/modules/analytics/benchmark_test.go` (build tag `benchmark`) proves Fix 5: 200k seeded tickets, every route under 300 ms p95 for the default window, a year, and filtered windows of both lengths.

## CSAT surveys

Resolving a ticket whose requester has an email issues a survey in the resolve transaction (`csatTx`, called from `statusTx`, so single changes, bulk updates and macros all issue it): a 54-character token whose SHA-256 goes into ticket metadata `csat` with `issued_at` and `expires_at`, and the `ticketing.ticket.csat_requested` event carrying the link. The public routes `POST /{tenant}/public/csat/check` and `POST /{tenant}/public/csat` find the ticket from the id inside the token and compare hashes. The submit marks the token used with a conditional update (hash matches, not used, not expired) and inserts the `csat_responses` row in the same transaction, and `csat_responses.token_hash` is unique, so a token gives one response even under a race. Resolving again issues a new token that replaces the old one.

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
| `github.com/Bengo-Hub/reports` | v0.2.0 | PDF, CSV and Excel export of the analytics views |
| `github.com/Bengo-Hub/shared-ratelimit` | v0.3.1 | Global and public limits, `TrustedRealIP` |
| `github.com/Bengo-Hub/shared-service-client` | v0.5.0 | S2S calls to auth-api members, marketflow-ai and subscriptions-api credits (circuit breaker, retries only with an idempotency key); its `crm` package for marketflow contacts |
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
| `INGEST_ENABLED` | `true` | Run the email and WhatsApp ingest consumers |
| `INGEST_EMAIL_STREAM`, `INGEST_EMAIL_SUBJECT` | `email`, `email.inbound.support` | Stream and subject of the inbound email event; the stream name follows email-provisioner's publisher |
| `INGEST_SUPPORT_DOMAIN` | `support.codevertexafrica.com` | Catch-all forwarding domain, `<slug>@` this domain |
| `INGEST_PLATFORM_ADDRESS`, `INGEST_PLATFORM_SLUG` | `support@codevertexafrica.com`, `codevertex` | The platform tenant's support mailbox and slug |
| `INGEST_WHATSAPP_STREAM`, `INGEST_WHATSAPP_SUBJECT` | `notifications`, `notifications.whatsapp.tenant_inbound` | Stream and subject of the tenant inbound WhatsApp event |
| `INGEST_WHATSAPP_THREAD_WINDOW` | `168h` | How long a WhatsApp conversation stays on one ticket |
| `CSAT_SURVEY_URL` | `https://ticketing.codevertexafrica.com/support/csat` | Public survey page; the link adds `?tenant=<tenant id>&token=<token>`. Empty sends no surveys |
| `CSAT_TOKEN_TTL` | `168h` | How long a survey link works |
| `MARKETFLOW_AI_URL` | `http://marketflow-ai.marketflow.svc.cluster.local:4001` | marketflow-ai (Vera), which runs every AI model call |
| `AI_TIMEOUT` | `30s` | Limit of one AI call; AI calls are never retried |
| `SUBSCRIPTION_BASE_URL` | `http://subscription-api.subscriptions.svc.cluster.local:4000` | subscriptions-api, owner of the prepaid AI credit wallet |

## Seed

`cmd/seed` (`/app/ticketing-seed` in the image) is idempotent and per tenant, and writes only what the tenant does not have yet. The entrypoint does not run it.

| Variable | Required | Description |
|---|---|---|
| `TENANT_ID` | yes | The tenant's auth UUID |
| `KB_SET` | no | Knowledge base set to load from `cmd/seed/kb/<set>/*.json` (for example `hadia`) |
| `SEED_SCOPE` | no | `kb` loads only articles and leaves categories and SLA policies alone; default `all` |

Articles are published and public, with author `00000000-0000-0000-0000-000000000000` and `metadata.seeded_from = ticketing-seed`. An article whose slug the tenant already has is never changed, even if staff edited, archived or deleted it. Each JSON file holds `slug`, `title`, `excerpt`, `category`, `tags` and `content_html`, and `kb_test.go` checks every set before it ships.

To seed one tenant in the cluster I run a one-off Job with the deployment's image and its `POSTGRES_URL` and `POSTGRES_MIGRATE_URL` secret references, plus `TENANT_ID`, `KB_SET` and `SEED_SCOPE=kb`.
