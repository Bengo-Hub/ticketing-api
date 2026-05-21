# Sprint 3 — SLA Management

**Duration**: Week 4  
**Status**: 🔲 Pending

---

## Overview

Implements Service Level Agreement policies: first-response and resolution time targets by priority tier, breach detection, and escalation workflows. An Ent schema already exists (`sla_policies` table); this sprint wires the business logic, API, and breach evaluation into the ticket lifecycle.

---

## Deliverables

### 1. SLA Policy API
- `POST /api/v1/{tenant}/sla/policies` — create SLA policy
- `GET /api/v1/{tenant}/sla/policies` — list policies (paginated)
- `GET /api/v1/{tenant}/sla/policies/{policyId}` — get policy
- `PUT /api/v1/{tenant}/sla/policies/{policyId}` — update
- `DELETE /api/v1/{tenant}/sla/policies/{policyId}` — delete (blocked if tickets reference it)

### 2. SLA Domain Model

```go
// SLAPolicy defines response time targets per priority.
type SLAPolicy struct {
    ID                     uuid.UUID
    TenantID               uuid.UUID
    Name                   string
    Description            string
    Priority               TicketPriority
    FirstResponseTargetMin int    // minutes
    ResolutionTargetMin    int    // minutes
    BusinessHoursOnly      bool
    IsActive               bool
    CreatedAt              time.Time
    UpdatedAt              time.Time
}

// SLAStatus on a ticket.
type SLAStatus struct {
    PolicyID               uuid.UUID
    FirstResponseBreached  bool
    ResolutionBreached     bool
    FirstResponseDueAt     *time.Time
    ResolutionDueAt        *time.Time
    FirstResponseAt        *time.Time
    ResolvedAt             *time.Time
}
```

### 3. SLA Evaluation in Ticket Lifecycle
- On ticket creation: look up active SLA policy matching ticket priority → compute due times → store on ticket
- On first agent comment (public): record `first_response_at`, mark breach if overdue
- On ticket resolved: record `resolved_at`, mark breach if overdue

### 4. SLA Background Worker
- `cmd/worker/main.go` — standalone worker binary
- Polls every 5 minutes for tickets with SLA targets past due
- Publishes `ticketing.sla.first_response_breached` and `ticketing.sla.resolution_breached` events
- Triggers escalation: assigns to supervisor team, updates priority to urgent

### 5. SLA Reporting Endpoint
- `GET /api/v1/{tenant}/sla/reports` — breach counts, avg response times by priority and date range

---

## Files to Create

```
internal/modules/sla/
├── domain.go          # SLAPolicy, SLAStatus types
├── errors.go          # ErrPolicyNotFound, ErrPolicyInUse
├── repository.go      # Repository interface
├── repository_ent.go  # Ent implementation
├── service.go         # Business logic
└── handler.go         # HTTP handlers

cmd/worker/
└── main.go            # Worker entry point
```

---

## Ent Schema Status

`internal/ent/schema/slapolicy.go` already exists with fields:
- `name`, `description`, `priority`
- `first_response_target_minutes`, `resolution_target_minutes`
- `business_hours_only`, `is_active`, `tenant_id`
- `created_at`, `updated_at`

Fields to add via migration: `escalation_policy_id`, `notify_agent_before_breach_min`.

---

## Definition of Done

- [ ] SLA policy CRUD API
- [ ] SLA due times computed on ticket creation
- [ ] First response and resolution breach tracking
- [ ] Background worker for breach notifications
- [ ] SLA reporting endpoint
- [ ] Atlas versioned migration for new fields
- [ ] Go build clean

---

## Next Sprint

**Sprint 4**: Public API — unauthenticated ticket submission, OTP verification, public status tracking.
