# Ticketing Service - Integration Guide

## Overview

This document provides detailed integration information for all external services and systems integrated with the Ticketing service, including internal Codevertex microservices and external third-party services.

---

## Table of Contents

1. [Internal Codevertex Service Integrations](#internal-codevertex-service-integrations)
2. [External Third-Party Integrations](#external-third-party-integrations)
3. [Integration Patterns](#integration-patterns)
4. [Two-Tier Configuration Management](#two-tier-configuration-management)
5. [Event-Driven Architecture](#event-driven-architecture)
6. [Integration Security](#integration-security)
7. [Error Handling & Resilience](#error-handling--resilience)

---

## Internal Codevertex Service Integrations

### Auth Service

**Integration Type**: OAuth2/OIDC + Events + REST

**Use Cases**:
- User authentication and authorization
- JWT token validation
- User identity synchronization
- Tenant/outlet discovery

**Architecture**:
- Uses `shared/auth-client` v0.1.0 library for JWT validation
- All protected `/api/v1/{tenantID}` routes require valid Bearer tokens
- Local agent table synced with auth-service (app-specific data only)

**Events Consumed**:
- `auth.tenant.created` - Initialize tenant in ticketing system
- `auth.tenant.updated` - Update tenant metadata
- `auth.user.created` - Create agent/user reference (if needed)
- `auth.user.updated` - Update user identity fields

### Notifications Service

**Integration Type**: Events (NATS) only. Ticketing makes no direct call to notifications-api; the
old S2S client was never called and was removed on 13 November 2026.

**Use Cases** (built):
- Ticket created, sent to the requester
- Public agent reply, sent to the requester
- Status change and resolution, sent to the requester
- Assignment, sent to the agent
- SLA breach alerts, sent to the assigned agent (else the tenant's support contact)

Not built: @mention alerts and satisfaction surveys.

**Events Published** (subject `ticketing.<event type>`, outbox on the `ticketing` stream):
- `ticketing.ticket.created`
- `ticketing.ticket.assigned`
- `ticketing.ticket.comment_added` (public agent replies only; internal notes and customer comments never)
- `ticketing.ticket.status_changed` (every change except to resolved)
- `ticketing.ticket.resolved`
- `ticketing.sla.first_response_breached`, `ticketing.sla.resolution_breached` (SLA worker, event type `sla.<kind>`)

Every ticket event carries the requester's `requester_name`, `requester_email` and `requester_phone`,
so notifications-api can reach the customer (by email today; the phone is there for SMS and WhatsApp later). Payload shapes are in the
event catalogue below.

**Support routing** (owner decision, 12 November 2026): a tenant's ticket messages go out through
the tenant's support contact (auth-api tenant metadata `support` {email, phone, whatsapp}, read with
`cache.ResolveSupportContact` from the shared tenant cache), falling back to its contact email and
phone. Platform tickets (the codevertex tenant) use support@codevertexafrica.com.

### Projects Service

**Integration Type**: Events (NATS) + REST API

**Use Cases**:
- Link tickets to projects
- Project-related support requests
- Project issue tracking

**REST API Usage**:
- `GET /api/v1/projects/{id}` - Get project details
- `POST /api/v1/projects/{id}/tickets` - Link ticket to project

**Events Published**:
- `ticketing.ticket.created` - For project issue tracking

**Events Consumed**:
- `projects.project.created` - Create project reference
- `projects.issue.created` - Create ticket from project issue

### ERP Service

**Integration Type**: Events (NATS) + REST API

**Use Cases**:
- Link tickets to orders, invoices, customers
- Billing-related support
- CRM integration for customer context

**REST API Usage**:
- `GET /api/v1/erp/orders/{id}` - Get order details
- `GET /api/v1/erp/invoices/{id}` - Get invoice details
- `GET /api/v1/erp/customers/{id}` - Get customer details

**Events Published**:
- `ticketing.ticket.created` - For order/invoice support

**Events Consumed**:
- `erp.order.created` - Create ticket reference
- `erp.invoice.issue` - Create billing support ticket

### Treasury Service

**Integration Type**: Events (NATS) + REST API

**Use Cases**:
- Payment-related support
- Billing inquiries
- Refund requests

**REST API Usage**:
- `GET /api/v1/treasury/payments/{id}` - Get payment details
- `GET /api/v1/treasury/invoices/{id}` - Get invoice details

**Events Published**:
- `ticketing.ticket.created` - For payment support

**Events Consumed**:
- `treasury.payment.failed` - Create payment support ticket
- `treasury.invoice.overdue` - Create billing support ticket

### Ordering-Backend

**Integration Type**: Events (NATS) + REST API

**Use Cases**:
- Order-related support
- Delivery issues
- Menu inquiries

**REST API Usage**:
- `GET /api/v1/ordering/orders/{id}` - Get order details

**Events Published**:
- `ticketing.ticket.created` - For order support

**Events Consumed**:
- `cafe.order.issue` - Create order support ticket

### POS Service

**Integration Type**: Events (NATS) + REST API

**Use Cases**:
- POS-related support
- Device issues
- Transaction inquiries

**REST API Usage**:
- `GET /api/v1/pos/orders/{id}` - Get POS order details

**Events Published**:
- `ticketing.ticket.created` - For POS support

**Events Consumed**:
- `pos.device.error` - Create device support ticket

### Inventory Service

**Integration Type**: Events (NATS) + REST API

**Use Cases**:
- Stock-related inquiries
- Inventory issues

**REST API Usage**:
- `GET /api/v1/inventory/items/{id}` - Get item details

**Events Published**:
- `ticketing.ticket.created` - For inventory support

### Logistics Service

**Integration Type**: Events (NATS) + REST API

**Use Cases**:
- Delivery-related support
- Shipping inquiries
- Tracking issues

**REST API Usage**:
- `GET /api/v1/logistics/shipments/{id}` - Get shipment details

**Events Published**:
- `ticketing.ticket.created` - For delivery support

**Events Consumed**:
- `logistics.delivery.issue` - Create delivery support ticket

### MarketFlow CRM: erasure link check

When a person is erased in another service, marketflow asks every service whether it still links the CRM contact before it deletes it. Ticketing answers `GET /api/v1/s2s/{tenant}/crm-links/{contactID}` with `{"referenced": true|false}`, sent with the shared `X-API-Key` (`INTERNAL_SERVICE_KEY`). The answer is true when any ticket carries the contact id in `tickets.crm_contact_id`, in any tenant: CRM contact ids are random UUIDs minted by marketflow, so a match anywhere is the same person, and tickets held under a mismatched tenant id can only answer "referenced", which keeps the contact. Ticketing keeps no tenant table, so a tenant UUID is accepted as given and a slug is resolved through auth-api (an unknown slug answers 404). Only the flag is returned and logs carry ids only. A bad contact id answers 400 and a missing or wrong key 401; marketflow treats every answer other than a clean 200 as "cannot answer" and keeps the contact.

---

## External Third-Party Integrations

### Email Providers

**Purpose**: Ticket notifications (built); email-to-ticket (designed, not built)

**Built**: outbound only. Ticketing publishes the ticket events above and notifications-api sends
the email from the tenant's own account (or the platform account) with the tenant's support email
as Reply-To. Ticketing holds no SMTP, IMAP or POP3 credentials.

**Designed, not built** (owner decision, 12 November 2026: outbound only for now):
- Email-to-ticket: a message to a support address opening a ticket
- Ticket reply by email: a reply to a ticket email adding a comment
- WhatsApp-to-ticket: a WhatsApp message to the support number opening a ticket

Until these are built, a customer replies through the public ticket page (OTP verified) or opens a
ticket through the public form, Vera or a service's S2S call.

### Chat Widgets

**Purpose**: Live chat integration

**Configuration** (Tier 1):
- Chat provider API keys: Stored encrypted
- Widget configuration: Configured

**Use Cases**:
- Live chat to ticket conversion
- Real-time chat support

### Knowledge Base Search

**Purpose**: Full-text search for knowledge base

**Configuration** (Tier 1):
- Search provider credentials: Stored encrypted

**Use Cases**:
- Article search
- Related article recommendations

---

## Integration Patterns

### 1. REST API Pattern (Synchronous)

**Use Case**: Data retrieval, immediate operations

**Implementation**:
- HTTP client with retry logic
- Circuit breaker pattern
- Request timeout (5 seconds default)
- Idempotency keys for mutations

### 2. Event-Driven Pattern (Asynchronous)

**Use Case**: Cross-service communication, notifications

**Transport**: NATS JetStream

**Flow**:
1. Service publishes event to NATS
2. Subscriber services consume event
3. Process event and update local state
4. Publish response events if needed

**Reliability**:
- At-least-once delivery
- Event deduplication via event_id
- Retry on failure
- Dead letter queue for failed events

### 3. Webhook Pattern (Callbacks)

**Use Case**: External provider callbacks, ticket events

**Implementation**:
- Webhook endpoints in ticketing service
- Signature verification (HMAC-SHA256)
- Retry logic for failed deliveries
- Idempotency handling

### 4. WebSocket/SSE Pattern (Real-time)

**Use Case**: Real-time ticket updates, live chat

**Implementation**:
- WebSocket connections for bidirectional communication
- SSE for server-to-client updates
- Connection management and reconnection logic

---

## Two-Tier Configuration Management

### Tier 1: Developer/Superuser Configuration

**Visibility**: Only developers and superusers

**Configuration Items**:
- Database credentials
- External API credentials
- Encryption keys
- Service-to-service API keys

**Storage**:
- Encrypted at rest in database (AES-256-GCM)
- K8s secrets for runtime
- Vault for production secrets

### Tier 2: Business User Configuration

**Visibility**: Normal system users (tenant admins, supervisors)

**Configuration Items**:
- SLA policies
- Assignment rules
- Escalation workflows
- Email templates
- Knowledge base settings

**Storage**:
- Plain text in database (non-sensitive)
- Tenant-specific configuration tables

---

## Event-Driven Architecture

### Event Catalog

#### Outbound Events (Published by Ticketing Service)

All ticket events use the shared-events envelope (`id`, `event_type`, `aggregate_type`
`ticketing`, `tenant_id`, `payload`) and the subject `ticketing.<event_type>`. Payloads are built in
`internal/modules/tickets/events.go` and tested in `events_test.go`.

Every ticket event's payload carries these keys, always present and empty when unknown:

| Key | Meaning |
|-----|---------|
| `ticket_id`, `ticket_number`, `subject` | The ticket |
| `status`, `priority`, `channel` | Its state; `status` is the new status on a status event |
| `source` | Who opened it (`public_form`, `vera`, ...), from ticket metadata |
| `agent_id` | The assigned agent, if any |
| `requester_name`, `requester_email`, `requester_phone` | The requester's contact (phone in `+<digits>` form) |

**ticketing.ticket.created** (event type `ticket.created`)
```json
{
  "id": "uuid",
  "event_type": "ticket.created",
  "aggregate_type": "ticketing",
  "tenant_id": "tenant-uuid",
  "payload": {
    "ticket_id": "ticket-uuid",
    "ticket_number": "TKT-0001",
    "subject": "Payout not received",
    "status": "new",
    "priority": "normal",
    "channel": "web",
    "source": "public_form",
    "agent_id": "",
    "requester_name": "Amina",
    "requester_email": "amina@example.com",
    "requester_phone": "+254712345678",
    "description": "First 500 characters of the description",
    "created_at": "2026-11-13T09:00:00Z"
  }
}
```

**ticketing.ticket.assigned**: the shared keys plus `agent_id` (the new assignee) and `agent_email`.

**ticketing.ticket.comment_added**: published for a public agent reply only. The shared keys plus
`comment_id`, `comment` (first 500 characters), `author_id`, `author_type` (`agent`) and
`agent_email` (the replying agent).

**ticketing.ticket.status_changed**: every status change except to resolved. The shared keys plus
`old_status` and `new_status`.

**ticketing.ticket.resolved**: the shared keys (so the customer is told, not only the tenant) plus
`old_status`, `new_status` (`resolved`) and `resolved_at`.

**ticketing.sla.first_response_breached** and **ticketing.sla.resolution_breached**

Published by the SLA worker (`cmd/worker`) on the `ticketing` stream once per breach (another
replica that already recorded it publishes nothing). notifications-api's ticketing consumer
alerts the assigned agent (`agent_email`), or the tenant's support contact when there is none. The
payload never carries the requester's contact.
```json
{
  "id": "uuid",
  "event_type": "sla.first_response_breached",
  "aggregate_type": "ticketing",
  "tenant_id": "tenant-uuid",
  "payload": {
    "ticket_id": "ticket-uuid",
    "ticket_number": "TKT-1042",
    "subject": "Printer down",
    "priority": "high",
    "status": "open",
    "agent_id": "agent-uuid",
    "agent_email": "agent@example.com",
    "team_id": "team-uuid",
    "response_breached": true,
    "resolution_breached": false,
    "event_type": "sla.first_response_breached"
  }
}
```

#### Inbound Events (Consumed by Ticketing Service)

**projects.issue.created**
```json
{
  "event_id": "uuid",
  "event_type": "projects.issue.created",
  "tenant_id": "tenant-uuid",
  "timestamp": "2024-12-05T10:30:00Z",
  "data": {
    "issue_id": "issue-uuid",
    "project_id": "project-uuid",
    "title": "Issue title",
    "description": "Issue description"
  }
}
```

**treasury.payment.failed**
```json
{
  "event_id": "uuid",
  "event_type": "treasury.payment.failed",
  "tenant_id": "tenant-uuid",
  "timestamp": "2024-12-05T10:30:00Z",
  "data": {
    "payment_id": "payment-uuid",
    "customer_id": "customer-uuid",
    "amount": 1000.00
  }
}
```

---

## Integration Security

### Authentication

**JWT Tokens**:
- Validated via `shared/auth-client` library
- JWKS from auth-service
- Token claims include tenant_id for scoping

**API Keys** (Service-to-Service):
- Stored in K8s secrets
- Rotated quarterly

### Authorization

**Tenant Isolation**:
- All requests scoped by tenant_id
- Ticket access isolated per tenant
- Data isolation enforced at database level

### Secrets Management

**Encryption**:
- Secrets encrypted at rest (AES-256-GCM)
- Decrypted only when used
- Key rotation every 90 days

---

## Error Handling & Resilience

### Retry Policies

**Exponential Backoff**:
- Initial delay: 1 second
- Max delay: 30 seconds
- Max retries: 3

### Circuit Breaker

**Implementation**:
- Opens after 5 consecutive failures
- Half-open after 60 seconds
- Closes on successful request

### Monitoring

**Metrics**:
- API call latency (p50, p95, p99)
- API call success/failure rates
- Event publishing success rates
- Ticket creation rate
- SLA compliance rate

**Alerts**:
- High failure rate (>5%)
- Service unavailability
- Event delivery failures
- SLA breach threshold exceeded

---

## References

- [Auth Service Integration](../auth-service/auth-api/docs/integrations.md)
- [Notifications Service Integration](../../../../notifications-service/notifications-api/docs/integrations.md) (its ticketing consumer turns the ticket events above into email)
- [Projects Service Integration](../projects-service/projects-api/docs/integrations.md)
- [Treasury Service Integration](../finance-service/treasury-api/docs/integrations.md)

