# Sprint 2: Ticket Management

**Status: Partial.** The ticket lifecycle, comments, categories, per-tenant numbers, the outbox and the internal create are built and live. Attachment upload and ticket search are missing.

## Tasks

- [x] `POST /api/v1/{tenant}/tickets`, list with filters `status`, `priority`, `channel`, `customerId`, `agentId`, `teamId`, `categoryId`, `orderId`
- [x] `GET /tickets/{ticketId}`, `GET /tickets/number/{ticketNumber}`, `PUT /tickets/{ticketId}`, `PUT /tickets/{ticketId}/status`, `PUT /tickets/{ticketId}/assign`, `DELETE /tickets/{ticketId}` (soft delete)
- [x] Status state machine (table below), invalid moves answer 400
- [x] Comments: add, list, update, delete; the first public agent reply sets `first_response_at`; authors come from the token
- [x] Categories with hierarchy; delete is refused while a category has children or tickets
- [x] Ticket numbers unique per tenant from `ticket_sequences` inside the create transaction (4f03d8e). The old `COUNT(*)+1` generator, which collided across tenants, is gone.
- [x] Outbox rows written in the same transaction as the ticket change (4f03d8e)
- [x] `tenant_id` on comments and attachments, backfilled from the parent ticket (4f03d8e)
- [x] Internal create with `X-API-Key` and `X-Tenant-ID`, idempotent on (tenant, source, source_ref.type, source_ref.id), 201 or 200 (4f03d8e). Contract in [integrations.md](../integrations.md).
- [x] Notifications through events only; the direct notifications client was removed on 13 November 2026
- [x] `GET /tickets/{ticketId}/attachments` lists attachment records
- [ ] Attachment upload, signed download URLs and delete (sprint 9)
- [ ] `search` on the ticket list. ticketing-ui sends it and `ListTickets` does not read it yet (sprint 9)
- [ ] Update requests that can clear a field. `UpdateTicketRequest` uses pointers with `omitempty`, so a null cannot unset a category, agent or team.

## Status transitions

| From | Allowed next statuses |
|---|---|
| `new` | `open`, `in_progress`, `cancelled` |
| `open` | `in_progress`, `pending_customer`, `pending_internal`, `resolved`, `closed`, `cancelled` |
| `in_progress` | `open`, `pending_customer`, `pending_internal`, `resolved`, `closed`, `cancelled` |
| `pending_customer`, `pending_internal` | `open`, `in_progress`, `resolved`, `closed`, `cancelled` |
| `resolved` | `open`, `closed` |
| `closed` | `open` |
| `cancelled` | none |

## Acceptance criteria

- Two tenants each get `TKT-000001` as their first ticket, and 50 concurrent creates across two tenants never share a number. Met over a fake repository in `sequence_test.go`; the database version is a sprint 0 item.
- A repeated internal create for the same QA defect returns the first ticket with 200. Met, covered by `handler_internal_test.go`.
- A ticket change whose transaction rolls back leaves no event behind. Met by design of the outbox write.
