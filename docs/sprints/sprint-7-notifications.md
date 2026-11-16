# Sprint 7: Notifications

**Status: Partial.** Every ticket lifecycle email goes out through events to notifications-api. Preferences, @mentions and SMS or WhatsApp delivery are not built.

## Tasks

- [x] Ticket events on `ticketing.>` with the requester's contact on every event: created, assigned, comment added (public agent replies only), status changed, resolved
- [x] SLA breach events to the assigned agent, else the tenant's support contact
- [x] notifications-api's ticketing consumer sends the mail under the tenant's name with the tenant's support address as Reply-To; templates live in notifications-api
- [x] The direct notifications client is gone; ticketing makes no S2S call to notifications-api
- [ ] Per-agent notification preferences
- [ ] @mentions that fill `mentions` and publish an event (sprint 9)
- [ ] SMS and WhatsApp to the requester (sprint 11, WhatsApp templates approved by Meta first)
- [ ] CSAT survey on resolve (sprint 12)

## Acceptance criteria

- A public agent reply emails the requester once and an internal note emails no one. Met.
