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
| shared reports module (`github.com/Bengo-Hub/reports` v0.2.0) | in process | PDF, CSV and Excel export of the analytics views ([analytics.md](analytics.md)) | Built (sprint 12) |
| marketflow-ai (Vera) | outbound | `POST /api/v1/internal/triage` (classify) and `POST /api/v1/agent/run` (summary, suggested reply, KB suggestions, KB draft), X-API-Key and X-Tenant-ID | Built (sprint 13), not deployed |
| subscriptions-api AI credit wallet | outbound | `GET /api/v1/s2s/credits/ai_credits/check`, `POST /api/v1/s2s/credits/ai_credits/consume`, X-API-Key | Built (sprint 13), not deployed |

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
| `phone_number_id`, `received_at` | no | Only for a WhatsApp message (`source` `notifications.whatsapp`, `source_ref.type` `wa_message`): the tenant number it came in on and when (RFC 3339). Ignored for any other source. |

**Idempotency.** The key is (tenant, `source`, `source_ref.type`, `source_ref.id`), enforced by the unique expression index `ticket_tenant_source_ref`. `source_ref.parent` is not part of the key. A repeat for the same key returns the existing ticket. When two calls race, the loser of the insert reads back the winner's ticket. Without `source_ref`, every call creates a new ticket.

**A WhatsApp message.** `source: "notifications.whatsapp"` with `source_ref: {type: "wa_message", id: <wa_message_id>}` is the key the WhatsApp ingest gives a ticket opened from that message, and the notifications inbox's Convert to ticket sends exactly that. Such a create is the ingest's ticket in every way: the customer id comes from the requester's phone as at ingest, and the ticket records the message as its origin with the WhatsApp reply window (`whatsapp_last_inbound_at`, `whatsapp_last_message_id`, `whatsapp_phone_number_id`). When the ingest already added the message to an open ticket as a comment, the create answers 200 with that ticket instead of opening a second one. The requester is not linked to a marketflow contact on this path; the ingest does that only for the tickets it opens itself.

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
| `ticketing.ticket.csat_requested` | Requester, the CSAT survey email `ticketing/ticket_csat_survey` (notifications-api `cmd/worker/ticketing_csat.go`): five score links (`csat_url` plus `&score=1..5`), threaded on the ticket's email thread, never WhatsApp; essential, so on by default and a tenant can turn it off. See below |
| `ticketing.sla.first_response_breached`, `ticketing.sla.resolution_breached` | Assigned agent, else the tenant's support contact |

Payloads are in [architecture.md](architecture.md). Mail goes out under the tenant's name with the tenant's support email as Reply-To. Platform tickets on the codevertex tenant use support@codevertexafrica.com. SMS is not sent yet.

**CSAT survey (sprint 12).** Resolving a ticket whose requester has an email records `ticketing.ticket.csat_requested` in the same transaction as the resolve, right after `ticketing.ticket.resolved`, through the outbox like every ticket event. It carries every common ticket key (including the email threading keys) plus:

| Field | Meaning |
|---|---|
| `status` | `resolved` |
| `resolved_at` | When the ticket was resolved, RFC 3339 |
| `survey_channel` | `email`. WhatsApp surveys are off by owner decision, so a ticket without a requester email gets no event |
| `csat_url` | The survey link to put in the email: `CSAT_SURVEY_URL?tenant=<tenant id>&token=<token>` |
| `csat_token` | The single-use token inside the link, in case the template builds its own link |
| `csat_expires_at` | When the link stops working, RFC 3339 (`CSAT_TOKEN_TTL` after the resolve, default 7 days) |

Example:

```json
{
  "ticket_id": "6f0c...", "ticket_number": "TKT-000042", "subject": "Printer jam",
  "status": "resolved", "priority": "normal", "channel": "email", "source": "",
  "agent_id": "", "requester_name": "Jane", "requester_email": "jane@example.com", "requester_phone": "",
  "resolved_at": "2026-11-17T09:00:00Z", "survey_channel": "email",
  "csat_url": "https://ticketing.codevertexafrica.com/support/csat?tenant=<tenant id>&token=<token>",
  "csat_token": "<54 characters>", "csat_expires_at": "2026-11-24T09:00:00Z",
  "email_message_token": "", "email_thread_token": "6f0c...", "email_in_reply_to": "", "email_references": [],
  "email_subject_tag": "[TKT-000042]", "email_reply_subject": "Re: Printer jam [TKT-000042]"
}
```

notifications-api's ticketing consumer needs a case for `ticket.csat_requested` that emails `requester_email` under the tenant's name with a "How did we do?" message, the ticket number and subject, and one button to `csat_url` (or five score links to `csat_url&score=<n>`, which the survey page reads to preselect a score). It goes in the ticket's email thread with `email_reply_subject` and the thread headers above. The token is a credential for one rating only: the email must not log it, and the event is the only place it exists in clear. The rating comes back through the public routes in [analytics.md](analytics.md), not through notifications-api.

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

## marketflow-ai (Vera), outbound

Sprint 13. ticketing-api calls marketflow-ai through shared-service-client (`internal/clients/vera.go`) with `X-API-Key` (`INTERNAL_SERVICE_KEY`) and `X-Tenant-ID`. AI calls carry no idempotency key, so the shared client never retries them, and each is limited by `AI_TIMEOUT` (30s).

| Route | Used for | Body sent | What ticketing reads |
|---|---|---|---|
| `POST /api/v1/internal/triage` | Classify a draft, auto-classify | `subject`, `description`, `tenant_id` | `category` (billing, technical, product, general), `priority`, `confidence`, `reasoning`, `requires_human_review`, `injected`. The neutral answer marketflow-ai gives when its model failed (confidence 0, no reasoning) counts as no answer and is not charged |
| `POST /api/v1/agent/run` | Summary, suggested reply, KB suggestions, KB draft | `run_id` (the credit key), `tenant_id`, `system_prompt` (ticketing's instructions), `context` (the ticket data), `no_tools: true`, `max_iterations: 1`, `billable: false` | `status` (only `completed` is an answer), `answer`, `stopped_early`, `stop_reason`, `review_notes`, `tainted` |

`no_tools: true` asks for a plain completion with no tool (marketflow-ai `a4a6a07`); an empty `allowed_tools` would still mean the read-only knowledge tools, so ticketing never sends `allowed_tools`. `billable` stays false because ticketing pays for the call itself, once, from the wallet below. marketflow-ai fences the context as data and masks emails and phone numbers, and checks the answer for figures and commitments that are not in the instructions or the data (its `docs/guardrails.md`, layer 4); ticketing adds no screening of its own.

What ticketing passes through to the UI:

- `review_notes` (list of strings) and `tainted` (bool) from `agent/run` go out unchanged on the summary, suggested reply, KB suggestion and KB draft answers, and are kept with the answer in the 24 hour replay cache. The notes are advice for the agent; nothing is blocked because of them.
- `injected` (bool) from triage goes out on the classify answer. An injected ticket always needs review, and auto-classify never applies its category.

## subscriptions-api AI credit wallet, outbound

Sprint 13. `internal/clients/credits.go`, through shared-service-client with `X-API-Key`. It is the same ledger marketflow-ai and notifications-api spend from (`api_token_wallets`, service `ai_credits`).

| Route | When | Notes |
|---|---|---|
| `GET /api/v1/s2s/credits/ai_credits/check?tenant_id=&units=1` | Before every model call | Nothing is spent. `allowed: false` with `reason` (`insufficient_credits`, `no_subscription`, `service_not_activated`) becomes 402 `ai_credits_exhausted`. `exempt: true` (the platform owner only) means no spend follows |
| `POST /api/v1/s2s/credits/ai_credits/consume` | After a usable answer | `{tenant_id, units: 1, key, description}`, with the key also sent as `Idempotency-Key` so the shared client may retry it. A repeated key answers `replayed: true` and charges nothing |

The description is what the tenant sees on its credit history, for example "Ticketing AI: suggested reply on TKT-000042". subscriptions-api decides the exemption; ticketing keeps no exemption list.

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

notifications-api is to publish `notifications.whatsapp.tenant_inbound` on its `notifications` stream once per inbound tenant message, through its outbox, never for a STOP reply: `{tenant_id, phone_number_id, wa_message_id, from, type, text, contact_name, received_at, reply_id}`. ticketing-api consumes it on the durable `ticketing-whatsapp-ingest`, gated by the tenant setting `channels.whatsapp.auto_ticket` (off by default, `GET` and `PUT /{tenant}/settings/channels`, `ticketing.settings.manage`). With it on, a message continues the sender's latest open WhatsApp ticket from the last 7 days or opens one; with it off, a message only continues such a ticket. Dedupe is on the WhatsApp message id. A ticket opened here has source `notifications.whatsapp` and source_ref `{type: "wa_message", id: <wa_message_id>}`, so the inbox's Convert to ticket action (notifications-api `POST /api/v1/whatsapp/conversations/{conversationId}/ticket`, described in its api-contracts.md) calls the internal create with the same source and ref and the two paths never open two tickets for one message.

Comments from a channel carry what the agent thread shows, read from the comment metadata and never stored apart: `channel` (`email` or `whatsapp`), `external_id` (the Message-ID or WhatsApp message id), `sender` (`{name, email, phone}`, from the `from_name`, `from_email` and `from_phone` metadata the ingest writes) and, for email, `email_subject` (the subject the customer wrote under, collapsed to one line, at most 300 characters). Every other comment has none of them. WhatsApp comments stored before 17 November 2026 have no sender, and email comments from before then have the sender's email but no name or subject. Agent replies go back on WhatsApp through `ticketing.ticket.comment_added` as described under notifications-api above.

## Order, invoice and project links

Tickets carry `order_id`, `external_reference`, `crm_contact_id` and `crm_lead_id`, and internal tickets carry `source` and `source_ref` in metadata. Ticketing calls none of pos-api, ordering, treasury or inventory today. Showing those links on the ticket is part of the integration review after sprint 13.

## Security

- Internal routes check `X-API-Key` with `httpware.ValidServiceKey` and require `X-Tenant-ID`.
- Protected routes run the full auth chain, including the tenant guard that refuses a URL tenant other than the token's.
- Public routes need an OTP token or a ticket access token, and public creates are rate limited per IP and per verified email. The CSAT survey routes take only their single-use survey token, stored as a SHA-256 hash, and are rate limited per IP.
- Secrets come from Kubernetes secrets as environment variables. Nothing in this repository holds a real key.

## References

- [Architecture](architecture.md)
- [ERD](erd.md)
- [Plan](plan.md)
