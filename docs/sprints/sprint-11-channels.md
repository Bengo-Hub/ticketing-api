# Sprint 11: Channels

**Status: Pending.** Every channel opens tickets through one ingest service in ticketing-api. ticketing-ui sprint 10 consumes it. The email design is the owner decision of 2026-11-16, which replaces the 12 November decision to stay outbound only.

## Tasks

### Ingest service

- [ ] One ingest path for every channel: resolve the tenant, resolve the requester to a marketflow contact (ticketing stores no PII copy beyond the ticket's requester fields), thread or open, dedupe, store attachments on R2
- [ ] Ingest rules: no ticket from no-reply senders, auto-replies (`Auto-Submitted`, `X-Autoreply`, bounce DSNs), mail flagged by ClamAV or a WhatsApp STOP reply

### WhatsApp

- [ ] notifications-api publishes `notifications.whatsapp.tenant_inbound` once per inbound message to a tenant's own number, through its outbox, with the tenant id, WhatsApp message id, sender, contact name, text or media reference and time received. This is a notifications-api change, agreed with its owner first.
- [ ] ticketing-api consumes it on a durable queue, threads it onto the requester's latest open WhatsApp ticket or opens one, and dedupes on the WhatsApp message id
- [ ] Tenant setting `channels.whatsapp.auto_ticket` in metadata, off by default, so a tenant that answers from the notifications inbox is not flooded
- [ ] Convert to ticket action in the notifications inbox, calling the internal create with `source: "notifications.whatsapp"`
- [ ] Agent replies on a WhatsApp ticket go out through the notifications send path inside the 24-hour window, or as an approved template outside it

### Email

- [ ] Tenant forwarding address `<slug>@support.codevertexafrica.com` on our Stalwart server, a catch-all support domain. The tenant auto-forwards its own support mailbox there, and I never store its mailbox password.
- [ ] JMAP reader following the pattern mail-service uses with Stalwart. It maps the local part to the tenant, threads on `In-Reply-To`, then `References`, then a `[TKT-n]` subject tag, and dedupes on the Message-ID.
- [ ] The tenant's `support` address in auth-api metadata stays the Reply-To on outbound mail
- [ ] Forwarding verify step: a test email that must arrive before the address shows as working
- [ ] Platform tenant: read `support@codevertexafrica.com`, already its own Stalwart mailbox, over JMAP for the codevertex tenant on the same path. info@ stays a human inbox.
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
