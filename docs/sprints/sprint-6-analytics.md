# Sprint 6 — Analytics & Reporting

**Duration**: Week 7  
**Status**: 🔲 Pending

---

## Overview

Provides ticket statistics, agent performance metrics, SLA compliance reports, and CSAT (customer satisfaction) survey infrastructure.

---

## Deliverables

### 1. Ticket Analytics API

```
GET /api/v1/{tenant}/analytics/tickets
    ?from=&to=&groupBy=day|week|status|priority|channel|agent|team
```

Response: `{ labels: [], series: [{ name, data }] }` — ready for charting.

**Metrics computed**:
- Total tickets by period (created, resolved, closed)
- Average first-response time by priority
- Average resolution time by priority
- Ticket volume by channel
- Status distribution at point-in-time

### 2. Agent Performance API

```
GET /api/v1/{tenant}/analytics/agents
    ?from=&to=&agentId=
```

Per-agent metrics:
- Tickets assigned, resolved, reopened
- Average first-response time
- Average resolution time
- CSAT score average
- SLA breach count

### 3. SLA Compliance API

```
GET /api/v1/{tenant}/analytics/sla
    ?from=&to=&priority=
```

- Breach count and breach rate by priority
- Average time to breach (when breached)
- Policy compliance percentage

### 4. CSAT Surveys

```
POST /api/v1/{tenant}/surveys                    create survey trigger rule
GET  /api/v1/{tenant}/surveys                    list rules
POST /api/v1/{tenant}/public/surveys/{token}     customer submits rating (1-5 + comment)
GET  /api/v1/{tenant}/analytics/csat             CSAT scores over time
```

Survey flow:
- Triggered 1 hour after ticket resolved
- Notifications-api sends email with unique survey link (`/survey/<token>`)
- Customer submits 1-5 rating + optional comment
- Token is single-use, expires 7 days

### 5. Report Export
- `GET /api/v1/{tenant}/analytics/export?type=tickets|agents&format=csv&from=&to=` — CSV download

---

## Definition of Done

- [ ] Ticket volume analytics endpoint
- [ ] Agent performance endpoint
- [ ] SLA compliance report
- [ ] CSAT survey creation and submission
- [ ] CSAT analytics endpoint
- [ ] CSV export

---

## Next Sprint

**Sprint 7**: Notification Preferences — per-agent and per-customer notification settings.
