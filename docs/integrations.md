# Ticketing API Integrations

This page lists every integration ticketing-api has in code today, the internal create contract other services call, and the integrations planned in the sprint files. Anything planned is marked with its sprint.

## Summary

| Service | Direction | How | Status |
|---|---|---|---|
| auth-api | inbound and outbound | JWKS and API key validation, tenant lookups, public OTP token check, `auth.user.*` and `apikey.changed` events | Built |
| notifications-api | outbound | `ticketing.>` events on the `ticketing` stream | Built |
| marketflow CRM | outbound and inbound | Contact upsert at channel ingest, `crm.activity.log_requested` events, the erasure link check | Built |
| projects-api | inbound | Internal create for QA defects | Built |
| hadia-api | inbound | Internal create and internal read for its support form | Built |
| marketflow-ai (Vera) | inbound | Internal create from the `create_ticket` tool | Built |
| subscriptions-api, auth-api | inbound | `tenant.purge` | Built |
| notifications-api inbound WhatsApp | inbound | `notifications.whatsapp.tenant_inbound`, durable `ticketing-whatsapp-ingest` | Consumer built (sprint 11); the publisher is a notifications-api change |
| email-provisioner (Stalwart support mailboxes) | inbound | `email.inbound.support`, durable `ticketing-email-ingest` | Consumer built (sprint 11); the JMAP reader and publisher are an email-provisioner change |
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

Payloads are in [architecture.md](architecture.md). Mail goes out under the tenant's name with the tenant's support email as Reply-To. Platform tickets on the codevertex tenant use support@codevertexafrica.com. SMS and CSAT surveys are not sent yet (sprint 12).

**WhatsApp replies (sprint 11).** `ticketing.ticket.comment_added` carries these fields on every event. They hold values only on a WhatsApp ticket with a requester phone and a recorded inbound message; otherwise they are false or empty.

| Field | Meaning |
|---|---|
| `requester_phone` | The requester's phone, as on every ticket event |
| `whatsapp_window_open` | True when the customer's last WhatsApp message is under 24 hours old at the time of the reply |
| `whatsapp_window_expires_at` | When that window closes, RFC 3339, so a consumer can check a late event again |
| `whatsapp_reply_to` | The WhatsApp message id of the customer's last message (`whatsapp_last_message_id`) |
| `phone_number_id` | The tenant number the customer wrote to (`whatsapp_phone_number_id`) |
| `whatsapp_text` | The whole reply as plain text with its line breaks, at most 4096 characters |

The window comes from ticket metadata the WhatsApp ingest writes: `whatsapp_last_inbound_at`, `whatsapp_last_message_id` and `whatsapp_phone_number_id`. notifications-api sends the reply as free-form text from the tenant's own number while the window is open. Outside it nothing goes on WhatsApp, because no approved Meta template carries a ticket reply yet; the email copy still goes when the requester has an address.

**Threaded replies (sprint 11).** Every ticket event carries `email_message_token`, `email_thread_token`, `email_in_reply_to`, `email_references`, `email_subject_tag` and `email_reply_subject`. For a reply to come back to its ticket, notifications-api's ticketing consumer and email worker need these changes, which are agreed with its owner first and not made from here:

1. In `cmd/worker/ticketing_consumer.go`, when `email_message_token` is set, use it as `messaging.Message.RequestID` instead of a new UUID. The worker already turns the request id into the Message-ID local part (`email.WithMessageID`), and bounce matching keeps working because the token is still the request id.
2. Carry `email_in_reply_to`, `email_references` and `email_thread_token` into `msg.Metadata` (for example under `email_thread`).
3. In `cmd/worker/main.go`, read them into the context next to `WithMessageID` (a new `email.WithThreadHeaders`).
4. In `internal/providers/email/smtp.go`, write `In-Reply-To: <email_in_reply_to>` and `References:` built as `<email_thread_token@sending domain>` followed by `email_references`, leaving the root out when it equals this message's own Message-ID (the receipt). In `brevo.go`, send the same two values in `payload.Headers`.
5. Use `email_reply_subject` as the subject of requester mail on `ticket.comment_added`, and add `email_subject_tag` to the other requester subjects, so a reply keeps the `[TKT-n]` tag.

Until then, a customer reply that quotes the subject tag still threads, and anything else opens a new ticket.

## marketflow CRM

- **Contact at ingest.** A ticket opened from an email or WhatsApp message links its requester through `POST {MARKETFLOW_API_URL}/api/v1/internal/contacts/upsert` (`X-API-Key` is `INTERNAL_SERVICE_KEY`; the tenant goes as `tenant_id` in the body, which is how marketflow reads it). The call goes through the shared `shared-service-client/crm` client, sends the email or phone with the name split into first and last, is sent once and never retried, and times out after five seconds. The returned id becomes `crm_contact_id`. A failure is logged with ids only and the ticket opens without it. Ticketing keeps no other contact data than the requester fields it already had.
- **Activity.** Ticket changes for a ticket with a CRM contact publish `crm.activity.log_requested` through the outbox. The CRM service's own stream captures it.
- **Erasure link check.** `GET /api/v1/s2s/{tenant}/crm-links/{contactID}` with `X-API-Key` answers `{"referenced": true|false}`. It is true when any ticket in any tenant carries the contact in `tickets.crm_contact_id`: CRM contact ids are random UUIDs minted by marketflow, so a match anywhere is the same person, and a ticket held under a mismatched tenant can only answer "referenced", which keeps the contact. A tenant UUID is accepted as given and a slug resolves through auth-api (an unknown slug answers 404). A bad contact id answers 400 and a missing or wrong key 401. Only the flag is returned and logs carry ids only.

## Tenant purge

The shared `TenantPurger` subscribes to `tenant.purge` on the `tenant` stream. auth-api sends it when a platform admin deletes a tenant or declines its registration, and subscriptions-api sends it for a dormant account a platform owner confirmed. Ticketing then deletes every row it holds for the tenant.

## Email

**Outbound.** Ticketing publishes the ticket events above and notifications-api sends the email. Ticketing holds no SMTP, IMAP, POP3 or JMAP credentials.

**Inbound (owner decision 2026-11-16, consumer built in [sprint 11](sprints/sprint-11-channels.md)).** email-provisioner is the only JMAP client and holds the Stalwart credentials. It reads `support@codevertexafrica.com` (the codevertex tenant) and the catch-all `support.codevertexafrica.com`, where each tenant auto-forwards its own support mailbox to `<slug>@support.codevertexafrica.com`, and publishes `email.inbound.support` once per message. ticketing-api consumes it on the durable `ticketing-email-ingest` (explicit ack, `MaxDeliver` 5). The stream is the config value `INGEST_EMAIL_STREAM`, default `email`, because email-provisioner's publisher is still being written; set it to the stream that publisher creates.

The event ticketing reads, bare or inside a shared-events envelope under `payload`:

| Field | Meaning |
|---|---|
| `message_id`, `in_reply_to`, `references[]` | Header values, with or without angle brackets |
| `from_email`, `from_name`, `to[]`, `cc[]` | Sender and visible recipients |
| `recipient` | The envelope recipient, which decides the tenant |
| `subject`, `text`, `html` | Body parts; the text part is preferred, the HTML is sanitised through httpware/richtext |
| `received_at` | RFC 3339 |
| `attachments[]` | `{key, filename, content_type, size, scan_status}`; the key must be in the tenant's own prefix, `<service>/<tenant id or slug>/attachments/...` |
| `auto_submitted`, `spam`, `virus`, `headers` | Optional loop and spam signals; `headers` holds other headers keyed in lower case (`x-autoreply`, `precedence`, `content-type`) |

Every email that resolves to a tenant, refused or not, stores `channels.email.last_received_at` in that tenant's settings document, at most once a minute per tenant. `GET /{tenant}/settings/channels` returns it as `email.last_received_at` (null until the first forwarded email), which is the forwarding verify step; `PUT` cannot change it.

Threading, dedupe and the ingest rules are in [architecture.md](architecture.md). The tenant's `support` address in auth-api metadata stays the Reply-To on outbound mail, so the customer's reply goes to the tenant's mailbox, is forwarded and comes back in.

## WhatsApp

Messages to the platform WhatsApp number go out from notifications-api as `notifications.whatsapp.inbound` with no tenant, and only marketflow-api's WhatsApp assistant consumes them. Messages to a tenant's own number land in notifications-api's shared inbox.

notifications-api is to publish `notifications.whatsapp.tenant_inbound` on its `notifications` stream once per inbound tenant message, through its outbox, never for a STOP reply: `{tenant_id, phone_number_id, wa_message_id, from, type, text, contact_name, received_at, reply_id}`. ticketing-api consumes it on the durable `ticketing-whatsapp-ingest`, gated by the tenant setting `channels.whatsapp.auto_ticket` (off by default, `GET` and `PUT /{tenant}/settings/channels`, `ticketing.settings.manage`). With it on, a message continues the sender's latest open WhatsApp ticket from the last 7 days or opens one; with it off, a message only continues such a ticket. Dedupe is on the WhatsApp message id. A ticket opened here has source `notifications.whatsapp` and source_ref `{type: "wa_message", id: <wa_message_id>}`, so the inbox's Convert to ticket action should call the internal create with the same source and ref and the two paths never open two tickets for one message. Agent replies go back on WhatsApp through `ticketing.ticket.comment_added` as described under notifications-api above.

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
