# Sprint 11: Channels

**Status: API ingest built, not deployed.** Every channel opens tickets through one ingest service in ticketing-api (`internal/modules/ingest`). ticketing-ui sprint 10 consumes it. The email design is the owner decision of 2026-11-16, which replaces the 12 November decision to stay outbound only, as refined by the research of 2026-11-17: email-provisioner is the only JMAP client, and ticketing consumes its event.

## Tasks

### Ingest service

- [x] One ingest path for every channel: resolve the tenant, thread or open through the tickets service's `CreateTicket` (numbering, routing, SLA, outbox events), dedupe, record attachments the sender already stored on R2. The requester is a marketflow contact, and the ticket keeps only `requester_name`, `requester_email` and `requester_phone`.
- [x] Dedupe on the tenant, channel and message id: a ticket keeps the message as `source` and `source_ref` under `ticket_tenant_source_ref`, a comment as `channel` and `external_id` under the new unique index `ticketcomment_tenant_channel_external_id`
- [x] Ingest rules: nothing opens or continues from our own addresses, no-reply and daemon senders, auto-replies (`Auto-Submitted`, `X-Autoreply`, `Precedence`, auto-reply subjects), delivery status reports, mail flagged as spam or carrying a virus, or a WhatsApp STOP reply
- [x] A reply to a ticket waiting on the customer or resolved reopens it; a reply to a closed or cancelled ticket opens a follow-up with `follow_up_of`
- [x] Tests: tenant from the recipient, threading order, dedupe, auto-reply refusal, WhatsApp opt-in, the open versus continue decision, attachment key ownership, the inbound comment transaction and the reply payload
- [ ] Resolve the requester to a marketflow contact id (`crm_contact_id`) at ingest; today the ticket carries the contact fields only

### WhatsApp

- [ ] notifications-api publishes `notifications.whatsapp.tenant_inbound` once per inbound message to a tenant's own number, through its outbox, on the `notifications` stream: `{tenant_id, phone_number_id, wa_message_id, from, type, text, contact_name, received_at, reply_id}`. This is a notifications-api change, agreed with its owner first.
- [x] ticketing-api consumes it on the durable `ticketing-whatsapp-ingest`, threads it onto the sender's latest open WhatsApp ticket active in the last 7 days or opens one, and dedupes on the WhatsApp message id. The phone is normalised through httpware/contact.
- [x] Tenant setting `channels.whatsapp.auto_ticket` in the new per-tenant settings document, off by default, through `GET` and `PUT /{tenant}/settings/channels` (`ticketing.settings.manage`). Off, a message only continues an open WhatsApp ticket and never opens one.
- [ ] Convert to ticket action in the notifications inbox, calling the internal create with `source: "notifications.whatsapp"` and `source_ref: {type: "wa_message", id: <wa_message_id>}`, the key the auto-ticketing path uses, so the two never open two tickets for one message
- [ ] Agent replies on a WhatsApp ticket go out through the notifications send path inside the 24-hour window, or as an approved template outside it

### Email

- [ ] email-provisioner reads `support@codevertexafrica.com` and the `support.codevertexafrica.com` catch-all over JMAP, stores attachments through httpware/objectstore under the tenant's `attachments` prefix, publishes `email.inbound.support` once per message with the fields in [integrations.md](../integrations.md), and marks the message seen
- [x] ticketing-api consumes it on the durable `ticketing-email-ingest` (explicit ack, `MaxDeliver` 5). The stream is `INGEST_EMAIL_STREAM`, default `email`, to be set to the stream email-provisioner's publisher creates.
- [x] Tenant from the recipient: `support@codevertexafrica.com` is the codevertex tenant, `<slug>@support.codevertexafrica.com` resolves the slug through the tenant cache, and an unknown slug or address is acknowledged and logged, never retried
- [x] Threading: a Message-ID ticketing sent or received in `In-Reply-To` or `References`, then the `[TKT-n]` subject tag from the ticket's requester, else a new ticket
- [x] Every ticket event carries the thread fields (`email_message_token`, `email_thread_token`, `email_in_reply_to`, `email_references`, `email_subject_tag`, `email_reply_subject`)
- [ ] notifications-api sets the Message-ID from `email_message_token`, writes `In-Reply-To` and `References`, and keeps the `[TKT-n]` tag in the subject (the list is in [integrations.md](../integrations.md))
- [ ] The tenant's `support` address in auth-api metadata stays the Reply-To on outbound mail (already the case)
- [ ] Forwarding verify step: a test email that must arrive before the address shows as working. The settings route already returns the forwarding address.
- [ ] The Stalwart catch-all domain is a production mail change and runs through mail-admin with the exact steps confirmed by the owner first

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
