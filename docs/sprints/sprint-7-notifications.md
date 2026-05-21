# Sprint 7 — Notification Preferences & Deep Integration

**Duration**: Week 8  
**Status**: 🔲 Pending

---

## Overview

Implements per-agent and per-customer notification preference management, and wires all ticket lifecycle events to fire the appropriate notifications through the notifications-api with the correct templates.

---

## Deliverables

### 1. Agent Notification Preferences

```
GET  /api/v1/{tenant}/agents/{agentId}/notification-preferences
PUT  /api/v1/{tenant}/agents/{agentId}/notification-preferences
```

Per-agent channels and events:
- Email: ticket_assigned, ticket_comment_added, sla_breach_warning, daily_summary
- Push: ticket_assigned, mention_in_comment
- Preferences stored as JSONB on the agent row

### 2. Ticket Lifecycle Notifications

Wired into service layer (currently notifications client exists but isn't called from service):

| Event | Recipient | Template |
|-------|-----------|----------|
| Ticket created | Customer | `ticket_created` |
| Ticket assigned | Agent | `ticket_assigned` |
| Public comment added (by agent) | Customer | `ticket_comment_added` |
| Public comment added (by customer) | Agent | `ticket_customer_replied` |
| Ticket resolved | Customer | `ticket_resolved` |
| SLA first-response breach | Supervisor | `sla_breach_first_response` |
| SLA resolution breach | Supervisor | `sla_breach_resolution` |

All calls are fire-and-forget (`go notifClient.Send...`) — never block the main handler.

### 3. Notification Template Registration
- `docs/templates/` — YAML definitions for all ticketing templates
- Seeded via `cmd/seed/main.go` on startup using notifications-api `POST /v1/{tenant}/templates`

### 4. @mention in Comments
- Parse `@agentName` in comment content
- Resolve agent by display name within the tenant
- Send push notification to mentioned agent

---

## Definition of Done

- [ ] Agent notification preferences CRUD
- [ ] Ticket lifecycle notifications wired in service layer
- [ ] Template YAML definitions created
- [ ] @mention detection and push notification
- [ ] All notifications are fire-and-forget (non-blocking)

---

## Next Sprint

**Sprint 8**: Advanced Routing — skills-based routing, auto-assignment, round-robin, AI classification.
