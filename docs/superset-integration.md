# Ticketing Service - Apache Superset Integration

## Overview

**Status: not built.** ticketing-api has no Superset client, no `SUPERSET_*` settings and no dashboard bootstrap today. This page is the target design for BI dashboards on the centralized Superset instance. In-app analytics come first, from rollup tables in [sprint 12](sprints/sprint-12-analytics-csat.md), and the dashboards below read those tables once they exist. Data sources name the real tables in [erd.md](erd.md); tables that do not exist yet are marked with their sprint.

The Ticketing service is designed to use the centralized Apache Superset instance for BI dashboards, analytics, and reporting. Superset is deployed as a centralized service accessible to all Codevertex services.

---

## Architecture

### Service Configuration

**Environment Variables**:
- `SUPERSET_BASE_URL` - Superset service URL
- `SUPERSET_ADMIN_USERNAME` - Admin username (K8s secret)
- `SUPERSET_ADMIN_PASSWORD` - Admin password (K8s secret)
- `SUPERSET_API_VERSION` - API version (default: v1)

**Authentication**:
- Admin credentials used for backend-to-Superset communication
- User authentication via JWT tokens passed to Superset for SSO
- Guest tokens generated for embedded dashboards

---

## Integration Methods

### 1. REST API Client

Backend uses Go HTTP client configured for Superset REST API calls.

**Base Configuration**:
- Base URL: `SUPERSET_BASE_URL/api/v1`
- Default headers: `Content-Type: application/json`
- Authentication: Bearer token from Superset login endpoint
- Retry policy: Exponential backoff (3 retries)
- Circuit breaker: Opens after 5 consecutive failures

**Key API Endpoints**:

**Authentication**:
- `POST /api/v1/security/login` - Login with admin credentials
- `POST /api/v1/security/refresh` - Refresh access token
- `POST /api/v1/security/guest_token/` - Generate guest token for embedding

**Data Sources**:
- `GET /api/v1/database/` - List all data sources
- `POST /api/v1/database/` - Create new data source
- `PUT /api/v1/database/{id}` - Update data source

**Dashboards**:
- `GET /api/v1/dashboard/` - List all dashboards
- `POST /api/v1/dashboard/` - Create new dashboard
- `GET /api/v1/dashboard/{id}` - Get dashboard details

### 2. Database Direct Connection

Superset connects directly to PostgreSQL database via read-only user for data access.

**Connection Configuration**:
- Database type: PostgreSQL 16+ with pgvector extension
- Connection string: Provided to Superset via data source API
- Read-only user: `superset_readonly` (created in PostgreSQL)
- Permissions: SELECT only on all tables, no write access
- SSL: Required for production connections

**Read-Only User Setup**:
- Create `superset_readonly` role in PostgreSQL
- Grant CONNECT on database
- Grant USAGE on schema
- Grant SELECT on all tables
- Set default privileges for future tables

**Connection String** (for Superset):
```
postgresql://<READONLY_USER>:<REPLACE_ME>@<POSTGRES_HOST>:5432/<TICKETING_DB>?sslmode=require
```

**Data Source Creation**:
- Data source created programmatically on application startup
- Connection tested before marking as active
- Data source updated if connection parameters change

---

## Pre-Built Dashboards

### 1. Ticket Overview Dashboard

**Charts**:
- Total tickets (metric)
- Open tickets (metric)
- Resolved today (metric)
- Tickets by status (pie chart)
- Tickets by priority (bar chart)
- Tickets by category (bar chart)
- Ticket volume trends (line chart)
- Ticket creation over time (line chart)

**Filters**:
- Date range
- Status
- Priority
- Category
- Agent

**Data Source**: `tickets`, `ticket_categories`, `agents` tables, and the ticket rollups (sprint 12)

### 2. Agent Performance Dashboard

**Charts**:
- Total agents (metric)
- Active agents (metric)
- Average response time (metric)
- Average resolution time (metric)
- Tickets per agent (bar chart)
- Response time by agent (bar chart)
- Resolution time by agent (bar chart)
- Customer satisfaction by agent (bar chart)

**Filters**:
- Date range
- Agent selection
- Team selection

**Data Source**: `agents`, `teams`, `tickets`, `ticket_comments` tables, and `csat_responses` (sprint 12)

### 3. SLA Compliance Dashboard

**Charts**:
- SLA compliance rate (metric)
- SLA breaches today (metric)
- Average response time (metric)
- Average resolution time (metric)
- SLA compliance trends (line chart)
- SLA breaches by policy (bar chart)
- Response time distribution (histogram)
- Resolution time distribution (histogram)

**Filters**:
- Date range
- SLA policy
- Priority

**Data Source**: `sla_policies` and the SLA columns on `tickets` (`sla_*_due_at`, `sla_*_breached`, `first_response_at`, `resolved_at`), plus the SLA rollups (sprint 12)

### 4. Customer Satisfaction Dashboard

**Charts**:
- Average CSAT score (metric)
- Average NPS score (metric)
- Response rate (metric)
- Satisfaction trends (line chart)
- Satisfaction by category (bar chart)
- Satisfaction by agent (bar chart)
- Satisfaction distribution (histogram)

**Filters**:
- Date range
- Category
- Agent

**Data Source**: `csat_responses` (sprint 12). NPS is not planned.

### 5. Knowledge Base Analytics Dashboard

**Charts**:
- Total articles (metric)
- Articles viewed today (metric)
- Helpful articles (metric)
- Article views trends (line chart)
- Top articles (bar chart)
- Articles by category (pie chart)
- Article helpfulness rate (line chart)

**Filters**:
- Date range
- Category
- Article

**Data Source**: `kb_articles` (`view_count`, `helpful_count`, `not_helpful_count`). There is no per-view history table, so view trends need the sprint 12 rollups.

---

## Implementation Details

### Initialization Process

1. Authenticate with Superset using admin credentials
2. Create/update data source pointing to the ticketing PostgreSQL database
3. Create/update dashboards for each module:
   - Ticket Overview Dashboard
   - Agent Performance Dashboard
   - SLA Compliance Dashboard
   - Customer Satisfaction Dashboard
   - Knowledge Base Analytics Dashboard
4. Log warnings for dashboard creation failures (non-blocking)

### Dashboard Bootstrap

**Backend Endpoint**: `GET /api/v1/dashboards/{module}/embed`

**Process**:
1. Extract tenant ID from context
2. Get dashboard ID for module from Superset
3. Generate guest token with Row-Level Security (RLS) clause filtering by tenant_id
4. Construct embed URL with dashboard ID and guest token
5. Return embed URL with expiration time (5 minutes)

### Row-Level Security (RLS)

**Implementation**:
- Guest tokens include RLS clauses
- RLS filters data by `tenant_id`
- Each tenant sees only their data

---

## Error Handling

### Retry Logic

**Retry Policy**:
- Maximum 3 retry attempts
- Exponential backoff (1s, 2s, 4s delays)
- Retry on 5xx errors or network failures
- Return response on success or after max retries

### Circuit Breaker

**Implementation**:
- Opens after 5 consecutive failures
- Half-open after 60 seconds
- Closes on successful request

### Fallback Strategies

**Superset Unavailable**:
- Return cached dashboard URLs (if available)
- Show static dashboard images
- Log error for monitoring
- Alert operations team

---

## Monitoring

### Metrics

**Integration-Specific Metrics**:
- Superset API call latency (p50, p95, p99)
- Dashboard creation/update success rates
- Guest token generation latency
- Data source connection health

**Prometheus Metrics**:
- `superset_api_call_duration_seconds` - Histogram of API call durations (labeled by endpoint, status)
- `superset_dashboard_views_total` - Counter of dashboard views (labeled by dashboard, tenant)

### Alerts

**Alert Conditions**:
- Superset service unavailability
- High API call failure rate (>5%)
- Dashboard creation failures
- Data source connection failures

---

## Security Considerations

### Authentication & Authorization

- Admin credentials stored in K8s secrets
- Guest tokens expire after 5 minutes
- RLS ensures tenant data isolation
- JWT tokens validated for SSO

### Data Privacy

- Read-only database user
- RLS filters enforce tenant isolation
- Sensitive ticket data masked in logs
- PII data excluded from dashboards (if applicable)

---

## References

- [Apache Superset REST API Documentation](https://superset.apache.org/docs/api)
- [Superset Deployment Guide](../../devops-k8s/docs/superset-deployment.md)
- [Ordering Integration](../ordering-service/ordering-backend/docs/superset-integration.md)

