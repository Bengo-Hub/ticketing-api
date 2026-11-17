# Sprint 11: Channels

**Status: ingest deployed 2026-11-17 (ticketing-api a67e2e6, email-provisioner 3da3d72, notifications-api bec16df). Email intake goes live once the Stalwart support domain, the ingest mailbox and DNS are in place.** Every channel opens tickets through one ingest service in ticketing-api (`internal/modules/ingest`). ticketing-ui sprint 10 consumes it. The email design is the owner decision of 2026-11-16, which replaces the 12 November decision to stay outbound only, as refined by the research of 2026-11-17: email-provisioner is the only JMAP client, and ticketing consumes its event.

## Tasks

### Ingest service

- [x] One ingest path for every channel: resolve the tenant, thread or open through the tickets service's `CreateTicket` (numbering, routing, SLA, outbox events), dedupe, record attachments the sender already stored on R2. The requester is a marketflow contact, and the ticket keeps only `requester_name`, `requester_email` and `requester_phone`.
- [x] Dedupe on the tenant, channel and message id: a ticket keeps the message as `source` and `source_ref` under `ticket_tenant_source_ref`, a comment as `channel` and `external_id` under the new unique index `ticketcomment_tenant_channel_external_id`
- [x] Ingest rules: nothing opens or continues from our own addresses, no-reply and daemon senders, auto-replies (`Auto-Submitted`, `X-Autoreply`, `Precedence`, auto-reply subjects), delivery status reports, mail flagged as spam or carrying a virus, or a WhatsApp STOP reply
- [x] A reply to a ticket waiting on the customer or resolved reopens it; a reply to a closed or cancelled ticket opens a follow-up with `follow_up_of`
- [x] Tests: tenant from the recipient, threading order, dedupe, auto-reply refusal, WhatsApp opt-in, the open versus continue decision, attachment key ownership, the inbound comment transaction and the reply payload
- [x] Resolve the requester of a new ticket to a marketflow contact id (`crm_contact_id`) at ingest, through marketflow-api's `POST /api/v1/internal/contacts/upsert` with the shared `shared-service-client/crm` client (v0.5.0) and `INTERNAL_SERVICE_KEY`. The client creates the contact when marketflow has none for the email or phone. A failed lookup is logged with ids only and the ticket opens without a contact id. A message that continues a ticket makes no call. `MARKETFLOW_API_URL` points at marketflow-api.
- [x] `GET /{tenant}/tickets?channel=` filters on the indexed `(tenant_id, channel)` column; an unknown channel answers 400

### WhatsApp

- [x] notifications-api publishes `notifications.whatsapp.tenant_inbound` once per stored inbound text message to a tenant's own number, on the `notifications` stream (notifications-api bec16df): `{tenant_id, phone_number_id, wa_message_id, from, type, text, contact_name, received_at, reply_id}`. This is a notifications-api change, agreed with its owner first.
- [x] ticketing-api consumes it on the durable `ticketing-whatsapp-ingest`, threads it onto the sender's latest open WhatsApp ticket active in the last 7 days or opens one, and dedupes on the WhatsApp message id. The phone is normalised through httpware/contact.
- [x] Tenant setting `channels.whatsapp.auto_ticket` in the new per-tenant settings document, off by default, through `GET` and `PUT /{tenant}/settings/channels` (`ticketing.settings.manage`). Off, a message only continues an open WhatsApp ticket and never opens one.
- [ ] Convert to ticket action in the notifications inbox, calling the internal create with `source: "notifications.whatsapp"` and `source_ref: {type: "wa_message", id: <wa_message_id>}`, the key the auto-ticketing path uses, so the two never open two tickets for one message
- [x] WhatsApp ingest records the customer's last message in ticket metadata (`whatsapp_last_inbound_at`, `whatsapp_last_message_id`, `whatsapp_phone_number_id`); a late delivery of an older message never moves it back. The ticket DTO derives `whatsapp_window_expires_at` from it (null for other channels and before the first inbound message).
- [x] Agent replies on a WhatsApp ticket go out through the notifications send path inside the 24-hour window: `ticket.comment_added` carries `whatsapp_window_open`, `whatsapp_window_expires_at`, `whatsapp_reply_to`, `phone_number_id` and `whatsapp_text`, and notifications-api sends the plain-text reply from the tenant's own number. Email still goes when the requester has one.
- [ ] Agent replies outside the 24-hour window. No approved Meta template carries a ticket reply (`broadcast_service_notice_v1` is a MARKETING broadcast notice), so nothing goes on WhatsApp and notifications-api logs it. A UTILITY ticket reply template has to be drafted, approved by Meta and added to the tenant template selection first.

### Email

- [x] email-provisioner (3da3d72) reads the configured support ingest mailboxes over JMAP, stores attachments through httpware/objectstore under `support-inbound/<owner>/inbound-mail/`, publishes `email.inbound.support` once per message with the fields in [integrations.md](../integrations.md), and marks the message seen
- [x] ticketing-api consumes it on the durable `ticketing-email-ingest` (explicit ack, `MaxDeliver` 5). The stream is `INGEST_EMAIL_STREAM`, default `subscription`, the stream that already owns `email.>`.
- [x] Tenant from the recipient: `support@codevertexafrica.com` is the codevertex tenant, `<slug>@support.codevertexafrica.com` resolves the slug through the tenant cache, and an unknown slug or address is acknowledged and logged, never retried
- [x] Threading: a Message-ID ticketing sent or received in `In-Reply-To` or `References`, then the `[TKT-n]` subject tag from the ticket's requester, else a new ticket
- [x] Every ticket event carries the thread fields (`email_message_token`, `email_thread_token`, `email_in_reply_to`, `email_references`, `email_subject_tag`, `email_reply_subject`)
- [x] notifications-api (bec16df) sets the Message-ID from `email_message_token`, writes `In-Reply-To` and `References` on SMTP and Brevo, and keeps the `[TKT-n]` tag in the subject. Brevo picks its own Message-ID, so replies to Brevo mail thread through References or the tag.
- [x] The tenant's `support` address in auth-api metadata stays the Reply-To on outbound mail (already the case)
- [x] Forwarding verify step (API): every email that reaches a tenant stores `channels.email.last_received_at` in its settings document, at most once a minute per tenant (the replica skips a tenant it wrote under a minute ago, and the write itself compares the stored time). `GET` and `PUT /{tenant}/settings/channels` return `email.last_received_at`, null until the first forwarded email; `PUT` writes only `whatsapp` and ignores any `email` in the body. The UI shows the address as working once it is set.
- [x] The Stalwart forwarding domain is live (2026-11-17, owner approved). `support.codevertexafrica.com` has its catch-all (`x:Domain.catchAllAddress`) set to `ingest@support.codevertexafrica.com`, and has MX, SPF and DMARC in Cloudflare. email-provisioner reads ingest@ and support@. A live test opened TKT-000001 through the catch-all and TKT-000002 through support@ on the codevertex tenant.

### Web

- [ ] Embeddable web widget on the existing public create with email codes

## Acceptance criteria

- A WhatsApp message to a tenant with auto-ticketing on opens one ticket linked to the marketflow contact; the same message delivered twice opens one ticket.
- With auto-ticketing off, nothing opens until an agent uses Convert to ticket.
- A reply to a ticket email threads onto that ticket; an out-of-office reply opens nothing.
- Mail to `support@codevertexafrica.com` opens a ticket on the codevertex tenant.

## Fix requirements proved

- **Flip, WhatsApp first.** WhatsApp is a first-class channel through notifications-api, with no add-on.
- **Fix 1, no feature behind an add-on.** Email and WhatsApp intake work on a tier-1 tenant.
