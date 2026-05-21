# Sprint 2 — Ticket Management

**Duration**: Week 3  
**Status**: ✅ Complete

---

## Overview

Implements the full ticket lifecycle: creation, assignment, status transitions, comments, attachments, categories, and ticket statistics. Includes the S2S internal endpoint for Vera AI tool calls and outbox-pattern event publishing.

---

## Deliverables

### 1. Ticket CRUD
- `POST /api/v1/{tenant}/tickets` — create ticket (sets defaults: status=new, priority=normal, channel=web)
- `GET /api/v1/{tenant}/tickets` — list with filters (status, priority, customerId, agentId, teamId, categoryId, orderId, channel) + pagination
- `GET /api/v1/{tenant}/tickets/stats` — counts grouped by status
- `GET /api/v1/{tenant}/tickets/{ticketId}` — get ticket
- `GET /api/v1/{tenant}/tickets/number/{ticketNumber}` — get by ticket number (e.g. TKT-00042)
- `PUT /api/v1/{tenant}/tickets/{ticketId}` — update subject/description/priority/tags/metadata
- `PUT /api/v1/{tenant}/tickets/{ticketId}/status` — status transition with validation
- `PUT /api/v1/{tenant}/tickets/{ticketId}/assign` — assign agent and/or team
- `DELETE /api/v1/{tenant}/tickets/{ticketId}` — soft-delete

### 2. Comments
- `POST /api/v1/{tenant}/tickets/{ticketId}/comments` — add comment (public/internal/system)
- `GET /api/v1/{tenant}/tickets/{ticketId}/comments` — list comments
- `PUT /api/v1/{tenant}/tickets/{ticketId}/comments/{commentId}` — update content
- `DELETE /api/v1/{tenant}/tickets/{ticketId}/comments/{commentId}` — soft-delete
- First agent public response marks `first_response_at` on the ticket

### 3. Attachments
- `GET /api/v1/{tenant}/tickets/{ticketId}/attachments` — list attachments

### 4. Categories
- `POST /api/v1/{tenant}/categories` — create category (supports hierarchy via parentId)
- `GET /api/v1/{tenant}/categories` — list with filters (parentId, active) + pagination
- `GET /api/v1/{tenant}/categories/{categoryId}` — get category
- `PUT /api/v1/{tenant}/categories/{categoryId}` — update
- `DELETE /api/v1/{tenant}/categories/{categoryId}` — delete (blocked if has children or tickets)

### 5. S2S Internal Endpoint
- `POST /api/v1/internal/tickets` — create ticket without JWT auth; validated by `X-API-Key: $INTERNAL_SERVICE_KEY`
- Used by Vera AI `create_ticket` tool to open support tickets on behalf of chat widget users
- Registered outside the JWT middleware group in the router

### 6. Event Publishing (Outbox Pattern)
- `ticketing.ticket.assigned` — published when agent is assigned
- `ticketing.ticket.resolved` — published when status changes to resolved
- Uses `github.com/Bengo-Hub/shared-events v0.2.0` outbox + background NATS publisher

### 7. Notifications Client
- `internal/clients/notifications.go` — S2S client to notifications-api
- `SendTicketCreated`, `SendTicketAssigned`, `SendTicketResolved`, `SendAgentComment`
- Wired via `WithNotificationsClient` option on the tickets handler

---

## Status State Machine

```
new → open | in_progress | cancelled
open → in_progress | pending_customer | pending_internal | resolved | closed | cancelled
in_progress → open | pending_customer | pending_internal | resolved | closed | cancelled
pending_customer → open | in_progress | resolved | closed | cancelled
pending_internal → open | in_progress | resolved | closed | cancelled
resolved → open | closed
closed → open
cancelled → (terminal)
```

---

## Shared Libs Used

| Library | Version | Usage |
|---------|---------|-------|
| `github.com/Bengo-Hub/pagination` | v0.2.0 | Ticket and category list endpoints |
| `github.com/Bengo-Hub/shared-events` | v0.2.0 | Outbox event publishing |
| `github.com/Bengo-Hub/shared-auth-client` | v0.6.1 | JWT claims, S2S key validation |

---

## Definition of Done

- [x] Full ticket CRUD with state machine
- [x] Comments with first-response tracking
- [x] Ticket categories with hierarchy
- [x] S2S internal endpoint for Vera AI
- [x] Outbox pattern for NATS events
- [x] Notifications client wired
- [x] Pagination on all list endpoints

---

## Next Sprint

**Sprint 3**: SLA Management — SLA policies, breach detection, escalation timers.
