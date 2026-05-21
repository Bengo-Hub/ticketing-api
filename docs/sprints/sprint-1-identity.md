# Sprint 1 — Identity & Access Management

**Duration**: Week 2  
**Status**: ✅ Complete

---

## Overview

Implements the full agent identity layer: team and agent CRUD, availability status, skill-based routing helpers, and JIT agent provisioning from auth.user.created NATS events.

---

## Deliverables

### 1. Team Management
- `POST /api/v1/{tenant}/teams` — create team
- `GET /api/v1/{tenant}/teams` — list with pagination
- `GET /api/v1/{tenant}/teams/{teamId}` — get team
- `PUT /api/v1/{tenant}/teams/{teamId}` — update team
- `DELETE /api/v1/{tenant}/teams/{teamId}` — soft-delete

### 2. Agent Management
- `POST /api/v1/{tenant}/agents` — create agent
- `GET /api/v1/{tenant}/agents` — list with filters (role, status, teamId, active) + pagination
- `GET /api/v1/{tenant}/agents/available` — available agents for assignment
- `GET /api/v1/{tenant}/agents/{agentId}` — get agent
- `GET /api/v1/{tenant}/agents/user/{userId}` — lookup by auth-service user ID
- `PUT /api/v1/{tenant}/agents/{agentId}` — update profile, skills, capacity
- `PUT /api/v1/{tenant}/agents/{agentId}/status` — update availability (available/busy/away/offline)
- `DELETE /api/v1/{tenant}/agents/{agentId}` — deactivate

### 3. JIT Agent Provisioning
- `internal/modules/identity/auth_events.go` — NATS subscriber on `auth.user.created` and `auth.user.updated`
- When auth event payload contains role = `agent`, `supervisor`, or `admin`, auto-creates the agent record
- Idempotent — skips if agent already exists for the user_id
- Agent email and display_name synced on `auth.user.updated`

### 4. Pagination
All list endpoints now use `pagination.Parse(r)` from `github.com/Bengo-Hub/pagination v0.2.0`.  
Response shape: `{ data, total, limit, page, hasMore }`.

---

## Shared Libs Used

| Library | Version | Usage |
|---------|---------|-------|
| `github.com/Bengo-Hub/pagination` | v0.2.0 | All list endpoints |
| `github.com/Bengo-Hub/shared-auth-client` | v0.6.1 | JWT claims extraction |

---

## Definition of Done

- [x] Team CRUD API working
- [x] Agent CRUD API with skill/capacity fields
- [x] Availability status transitions
- [x] JIT agent provisioning from auth.user.created
- [x] Pagination on all list endpoints
- [x] Go build passes clean

---

## Next Sprint

**Sprint 2**: Ticket Management — full ticket lifecycle, comments, attachments, categories.
