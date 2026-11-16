# Ticketing API Integrations

This page lists every integration ticketing-api has in code today, the internal create contract other services call, and the integrations planned in the sprint files. Anything planned is marked with its sprint.

## Summary

| Service | Direction | How | Status |
|---|---|---|---|
| auth-api | inbound and outbound | JWKS and API key validation, tenant lookups, public OTP token check, `auth.user.*` and `apikey.changed` events | Built |
| notifications-api | outbound | `ticketing.>` events on the `ticketing` stream | Built |
| marketflow CRM | outbound and inbound | `crm.activity.log_requested` events, the erasure link check | Built |
| projects-api | inbound | Internal create for QA defects | Built |
| hadia-api | inbound | Internal create and internal read for its support form | Built |
| marketflow-ai (Vera) | inbound | Internal create from the `create_ticket` tool | Built |
| subscriptions-api, auth-api | inbound | `tenant.purge` | Built |
| notifications-api inbound WhatsApp | inbound | `notifications.whatsapp.tenant_inbound` | Planned, sprint 11 |
| Stalwart support mailboxes | inbound | JMAP | Planned, sprint 11 |
| object storage (R2) | outbound | httpware/objectstore | Planned, sprint 9 |
| shared reports module | in process | Report export | Planned, sprint 12 |
| marketflow-ai, subscriptions-api AI wallet | outbound | Classification, summaries, replies, credits | Planned, sprint 13 |

## Internal create: POST /api/v1/internal/tickets

Services raise tickets on a tenant's desk through this route. It takes no user token.

**Headers**

| Header | Value |
|---|---|
| `X-API-Key` | `INTERNAL_SERVICE_KEY`, the fleet S2S key |
| `X-Tenant-ID` | The tenant's auth UUID. The body carries no tenant. |

**Body**

| Field | Required | Notes |
|---|---|---|
| `subject` | yes | Trimmed, must not be empty |
| `description` | yes | Trimmed, must not be empty |
| `source` | yes | The calling system, for example `projects.qa`. At most 200 characters. Stored in ticket metadata. |
| `priority` | no | `low`, `normal`, `high`, `urgent` or `critical`. Defaults to `normal`. |
| `channel` | no | `web`, `email`, `api`, `chat`, `whatsapp`, `phone` or `internal`. Defaults to `web`. |
| `category` | no | Free text, stored in ticket metadata |
| `source_ref` | no | `{type, id, parent?}`, the record the ticket was raised for. `type` and `id` are required when it is sent, at most 200 characters each. `parent` is optional context `{type, id}` with no parent of its own. Stored in ticket metadata. |
| `requester_name`, `requester_email`, `requester_phone` | no | The requester's contact, stored in ticket metadata and carried on every ticket event |
| `contact_hash` | no | The caller's keyed hash of a contact it verified. Ticketing stores it and returns it on the internal read so the caller can prove a later reader. One contact's tickets group under one synthetic customer id derived from the hash. |

**Idempotency.** The key is (tenant, `source`, `source_ref.type`, `source_ref.id`), enforced by the unique expression index `ticket_tenant_source_ref`. `source_ref.parent` is not part of the key. A repeat for the same key returns the existing ticket. When two calls race, the loser of the insert reads back the winner's ticket. Without `source_ref`, every call creates a new ticket.

**Responses**

| Status | When | Body |
|---|---|---|
| 201 | A new ticket was created | `{"id", "ticket_number", "status"}` |
| 200 | The key matched an existing ticket | `{"id", "ticket_number", "status"}` of that ticket |
| 400 | `X-Tenant-ID` missing or not a UUID, body unreadable, a required field missing, an invalid priority, channel or `source_ref` | `{"error": "..."}` |
| 401 | Missing or wrong `X-API-Key` | `{"error": "unauthorized"}` |

A created ticket goes through the same path as any other create: routing rule, SLA deadlines, per-tenant number, and `ticketing.ticket.created` in the same transaction.

## Internal read: GET /api/v1/internal/tickets/{ticketNumber}

Takes `X-API-Key` and `X-Tenant-ID`, and answers 401 and 400 on the same rules as the create. It returns what a calling service may show its own user: `id`, `ticket_number`, `status`, `priority`, `channel`, `subject`, `description`, `category`, `contact_hash`, `source`, `source_ref`, `created_at`, `updated_at` and the public comments (`author_type`, `content`, `created_at`). Internal notes and the requester's contact details never leave through it.

## Internal create callers

| Caller | Code | `source` | `channel` | `source_ref` |
|---|---|---|---|---|
| projects-api, a failed QA criterion | `internal/platform/ticketing/client.go` | `projects.qa` | `internal` | `{type: "qa_defect", id: <defect id>, parent: {type: "qa_session", id: <session id>}}`, so one QA session can raise several defects and each defect raises one ticket |
| hadia-api support form | `internal/clients/ticketing/client.go`, called from `internal/modules/support/service.go` after its own email or phone code | `hadia-contact-form` | `web` | none, so each submission opens a ticket. It sends the requester fields and `contact_hash`, and reads tickets back through the internal read. |
| marketflow-ai `create_ticket` tool | `internal/agent/tools/create_ticket.go` | `vera_ai` | `chat` | none. The tool maps the model's priority to a ticketing priority and reports the real `ticket_number`. |

ticketing-api deploys before its callers whenever this contract changes, and every caller switches in the same release.

## auth-api

- **JWT and API keys.** `RequireAuth` from auth-client v0.20.0 validates SSO tokens against `{AUTH_SERVICE_URL}/api/v1/.well-known/jwks.json` (issuer `AUTH_ISSUER`, audience `AUTH_AUDIENCE`) and accepts auth-api API keys while `AUTH_ENABLE_API_KEY` is true. The full chain is in [architecture.md](architecture.md).
- **API key revocation.** ticketing subscribes to the `auth` broadcast `apikey.changed` and drops the key from every validator on the pod.
- **Tenants.** Slugs and tenant details resolve through `internal/platform/tenantcache` (shared cache, then auth-api). The tenant's support contact is metadata `support`, read with `cache.ResolveSupportContact`.
- **Public OTP.** The public support pages get a six-digit email code from auth-api's public OTP routes. ticketing trades the resulting `X-OTP-Token` with auth-api through `internal/clients/otp.go`, which sends the internal service key. This client uses plain `net/http`, not shared-service-client, because a retried call could replay a one-time code.
- **User events.** `auth.user.created` and `auth.user.updated` arrive on a queue subscription. They create or refresh the tenant's ticketing user with the same role mapping as sign-in, and refresh an existing agent's email and name. `auth.user.deleted` is not consumed yet (integration review).

## notifications-api

Events only. Ticketing makes no direct call to notifications-api. notifications-api's ticketing consumer turns these into email:

| Event | Recipient |
|---|---|
| `ticketing.ticket.created` | Requester (receipt) |
| `ticketing.ticket.assigned` | Assigned agent |
| `ticketing.ticket.comment_added` (public agent replies only) | Requester |
| `ticketing.ticket.status_changed` (every change except to resolved) | Requester, for the statuses that need them |
| `ticketing.ticket.resolved` | Requester |
| `ticketing.sla.first_response_breached`, `ticketing.sla.resolution_breached` | Assigned agent, else the tenant's support contact |

Payloads are in [architecture.md](architecture.md). Mail goes out under the tenant's name with the tenant's support email as Reply-To. Platform tickets on the codevertex tenant use support@codevertexafrica.com. SMS, WhatsApp, @mention alerts and CSAT surveys are not sent yet (sprints 9, 11 and 12).

## marketflow CRM

- **Activity.** Ticket changes for a ticket with a CRM contact publish `crm.activity.log_requested` through the outbox. The CRM service's own stream captures it.
- **Erasure link check.** `GET /api/v1/s2s/{tenant}/crm-links/{contactID}` with `X-API-Key` answers `{"referenced": true|false}`. It is true when any ticket in any tenant carries the contact in `tickets.crm_contact_id`: CRM contact ids are random UUIDs minted by marketflow, so a match anywhere is the same person, and a ticket held under a mismatched tenant can only answer "referenced", which keeps the contact. A tenant UUID is accepted as given and a slug resolves through auth-api (an unknown slug answers 404). A bad contact id answers 400 and a missing or wrong key 401. Only the flag is returned and logs carry ids only.

## Tenant purge

The shared `TenantPurger` subscribes to `tenant.purge` on the `tenant` stream. auth-api sends it when a platform admin deletes a tenant or declines its registration, and subscriptions-api sends it for a dormant account a platform owner confirmed. Ticketing then deletes every row it holds for the tenant.

## Email

**Built: outbound only.** Ticketing publishes the ticket events above and notifications-api sends the email. Ticketing holds no SMTP, IMAP or POP3 credentials.

**Inbound email to ticket (owner decision 2026-11-16, built in [sprint 11](sprints/sprint-11-channels.md)).** It replaces the 12 November decision to stay outbound only.

- **Tenants.** Each tenant gets a forwarding address `<slug>@support.codevertexafrica.com` on our Stalwart server. The tenant sets an auto-forward from its own support mailbox, and I never store a tenant mailbox password. ticketing reads the catch-all support domain over JMAP, maps the local part to the tenant, opens a ticket or threads a reply (`In-Reply-To`, `References`, then a `[TKT-n]` subject tag) and dedupes on the Message-ID. The tenant's `support` address in auth-api metadata stays the Reply-To on outbound mail. A settings page shows the forwarding address with a verify step: a test email that must arrive.
- **Platform tenant.** `support@codevertexafrica.com` is its own Stalwart mailbox and no longer an info@ alias. In sprint 11 ticketing reads it over JMAP for the codevertex tenant on the same path as tenant mail. info@ stays a human inbox.
- **Ingest rules.** Mail from no-reply addresses, auto-replies (`Auto-Submitted`, `X-Autoreply`, bounce DSNs) and mail flagged by ClamAV never opens a ticket. Attachments go to object storage. Every requester resolves to a marketflow contact.
- The JMAP client follows the pattern mail-service already uses with Stalwart.

Until this ships, a customer replies through the public ticket page or opens a ticket through the public form, Vera or a service's internal create.

## WhatsApp (planned, sprint 11)

Messages to the platform WhatsApp number go out from notifications-api as `notifications.whatsapp.inbound` with no tenant, and only marketflow-api's WhatsApp assistant consumes them. Messages to a tenant's own number land in notifications-api's shared inbox and publish nothing another service can consume.

Sprint 11 adds `notifications.whatsapp.tenant_inbound` to notifications-api, carrying the tenant id, the WhatsApp message id, the sender, the contact name, the text or a media reference and the time received, published once per inbound message through its outbox. ticketing-api consumes it on a durable queue and threads it onto the requester's latest open WhatsApp ticket or opens a new one, dedupes on the WhatsApp message id and never opens a ticket from a STOP reply. Auto-ticketing is the tenant setting `channels.whatsapp.auto_ticket`, off by default. The notifications inbox also gets a Convert to ticket action that calls the internal create with `source: "notifications.whatsapp"`. Agent replies on a WhatsApp ticket go out through the notifications send path inside the 24-hour window, or as a template outside it.

## Order, invoice and project links

Tickets carry `order_id`, `external_reference`, `crm_contact_id` and `crm_lead_id`, and internal tickets carry `source` and `source_ref` in metadata. Ticketing calls none of pos-api, ordering, treasury or inventory today. Showing those links on the ticket is part of the integration review after sprint 13.

## Security

- Internal routes check `X-API-Key` with `httpware.ValidServiceKey` and require `X-Tenant-ID`.
- Protected routes run the full auth chain, including the tenant guard that refuses a URL tenant other than the token's.
- Public routes need an OTP token or a ticket access token, and public creates are rate limited per IP and per verified email.
- Secrets come from Kubernetes secrets as environment variables. Nothing in this repository holds a real key.

## References

- [Architecture](architecture.md)
- [ERD](erd.md)
- [Plan](plan.md)
