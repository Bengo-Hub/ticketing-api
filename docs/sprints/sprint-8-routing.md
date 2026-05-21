# Sprint 8 — Advanced Routing & AI Classification

**Duration**: Week 9  
**Status**: 🔲 Pending

---

## Overview

Implements intelligent ticket routing: skills-based assignment, round-robin workload balancing, priority escalation rules, and AI-powered category classification via the marketflow-ai service.

---

## Deliverables

### 1. Routing Rules API

```
POST /api/v1/{tenant}/routing/rules          create rule
GET  /api/v1/{tenant}/routing/rules          list rules
PUT  /api/v1/{tenant}/routing/rules/{id}     update
DELETE /api/v1/{tenant}/routing/rules/{id}   delete
POST /api/v1/{tenant}/routing/rules/evaluate test rule against a ticket
```

Rule schema:
```go
type RoutingRule struct {
    ID          uuid.UUID
    TenantID    uuid.UUID
    Name        string
    Priority    int        // evaluation order (lower = first)
    Conditions  []RuleCondition  // channel=web, priority=urgent, category=billing
    Actions     []RuleAction     // assign_to_team, assign_to_agent, set_priority, add_tag
    IsActive    bool
}
```

### 2. Auto-Assignment on Ticket Create
When a new ticket is created:
1. Evaluate routing rules in priority order
2. If rule matches → execute actions (assign team/agent, set priority)
3. If no rule matches → use least-loaded available agent in default team
4. Increment agent `current_ticket_count`

### 3. AI Category Classification
- `POST /api/v1/{tenant}/tickets/{ticketId}/classify` — trigger AI classification
- Calls `POST /api/v1/ai/classify` on marketflow-ai with ticket subject + description
- Updates ticket category and priority based on AI suggestion (requires agent confirmation)
- Optionally runs automatically on ticket create if tenant has AI routing enabled

### 4. Round-Robin & Load Balancing
- `GetLeastLoadedAgent` — already implemented in identity service
- `IncrementAgentTicketCount` / `DecrementAgentTicketCount` — called on assign/resolve
- Fair round-robin within a team when all agents have equal load

### 5. Escalation Rules
- Auto-escalate to supervisor after N hours without first response (configurable per SLA policy)
- Auto-escalate to admin after M hours past resolution SLA breach
- Escalation publishes `ticketing.ticket.escalated` NATS event

---

## Definition of Done

- [ ] Routing rules CRUD API
- [ ] Auto-assignment on ticket create
- [ ] AI classification endpoint
- [ ] Agent ticket count tracking on assign/resolve
- [ ] Escalation rules via background worker
- [ ] Atlas migration for routing_rules table

---

## Notes

- AI routing requires `MARKETFLOW_AI_URL` + `INTERNAL_SERVICE_KEY` config
- Round-robin state stored in Redis to survive restarts
