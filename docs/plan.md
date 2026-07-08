# Ticketing Service - Implementation Plan

## Executive Summary

**System Purpose**: Multi-tenant customer support and helpdesk ticketing system for Codevertex ecosystem. Provides comprehensive ticket management, SLA tracking, knowledge base integration, and customer satisfaction measurement. Enables support teams to efficiently handle customer inquiries, technical issues, and service requests across all Codevertex services.

**Key Capabilities**:
- Multi-channel ticket creation (web, email, API, chat)
- Ticket lifecycle management with state machine
- SLA tracking and escalation workflows
- Agent assignment and routing
- Knowledge base integration
- Customer satisfaction surveys
- Ticket analytics and reporting
- Integration with all Codevertex services for context-aware support

**Entity Ownership**: This service owns all ticketing-specific entities: tickets, ticket comments, attachments, ticket assignments, SLA records, knowledge base articles, and satisfaction surveys. **Ticketing does NOT own**: users (references auth-service via `user_id`), tenants (references from auth-service), orders (references from ordering/POS services), projects (references from projects-service), financial transactions (references from treasury-api).

---

## Technology Stack

### Core Framework
- **Language**: Go 1.22+
- **Architecture**: Clean/Hexagonal architecture
- **HTTP Router**: chi
- **API Documentation**: OpenAPI-first contracts
- **gRPC**: ConnectRPC gateway for high-throughput integrations

### Data & Caching
- **Primary Database**: PostgreSQL 16+ (pgx/v5) with Ent ORM
- **Caching**: Redis 7+ for caching, rate limiting, and real-time updates
- **Message Broker**: NATS JetStream for event backbone
- **Storage**: S3-compatible storage for ticket attachments

### Supporting Libraries
- **ORM**: Ent (schema-as-code migrations)
- **Validation**: Custom validators
- **Resilience**: Circuit breaker patterns, retry policies
- **Logging**: zap (structured logging)
- **Tracing**: OpenTelemetry instrumentation
- **Metrics**: Prometheus

### DevOps & Observability
- **Containerization**: Multi-stage Docker builds
- **Orchestration**: Kubernetes (via centralized devops-k8s)
- **CI/CD**: GitHub Actions → ArgoCD
- **Monitoring**: Prometheus + Grafana, OpenTelemetry
- **APM**: Jaeger distributed tracing

---

## Domain Modules & Features

### 1. Identity & Access Management

**Ticketing-Specific Features**:
- Multi-tenant support with RBAC (customer, agent, supervisor, admin)
- Agent profiles and availability management
- Team and department management
- Agent performance metrics

**Entities Owned**:
- `teams` - Support team groupings
- `agents` - Agent profiles (references auth-service users)
- `agent_availability` - Agent availability schedules
- `agent_performance` - Agent performance metrics

**Integration Points**:
- **auth-service**: JWT validation, user identity sync, MFA enforcement
- **Tenant Sync**: Webhook events for tenant/outlet discovery

### 2. Ticket Management

**Ticketing-Specific Features**:
- Multi-channel ticket creation (web form, email, API, chat)
- Ticket lifecycle with state machine (new → open → in-progress → resolved → closed)
- Ticket categories and types (support, bug, feature request, billing, technical)
- Priority levels (low, medium, high, urgent, critical)
- Ticket tags and labels
- Ticket merging and splitting
- Ticket templates

**Entities Owned**:
- `tickets` - Canonical ticket records
- `ticket_categories` - Category definitions
- `ticket_types` - Type definitions
- `ticket_priorities` - Priority definitions
- `ticket_tags` - Tag definitions
- `ticket_tag_assignments` - Ticket-tag relationships
- `ticket_templates` - Pre-defined ticket templates
- `ticket_merges` - Ticket merge history
- `ticket_splits` - Ticket split history

**Integration Points**:
- **auth-service**: User identity (references only)
- **notifications-service**: Ticket notifications, status updates
- **projects-service**: Link tickets to projects
- **ERP**: Link tickets to orders, invoices, customers

### 3. Ticket Communication

**Ticketing-Specific Features**:
- Ticket comments and replies
- Internal notes (agent-only)
- Email threading
- @mentions and notifications
- File attachments
- Rich text formatting

**Entities Owned**:
- `ticket_comments` - Comment records
- `ticket_attachments` - File attachments
- `ticket_mentions` - @mention tracking
- `ticket_email_threads` - Email threading

**Integration Points**:
- **notifications-service**: Comment notifications, @mention alerts
- **S3 Storage**: Attachment storage

### 4. Assignment & Routing

**Ticketing-Specific Features**:
- Automatic ticket assignment (round-robin, skill-based, load-based)
- Manual assignment
- Team assignment
- Escalation workflows
- Reassignment history

**Entities Owned**:
- `ticket_assignments` - Assignment records
- `assignment_rules` - Auto-assignment rules
- `escalation_rules` - Escalation workflow rules
- `escalation_history` - Escalation audit trail

**Integration Points**:
- **auth-service**: Agent identity (references only)

### 5. SLA Management

**Ticketing-Specific Features**:
- SLA policy definitions (response time, resolution time)
- SLA tracking per ticket
- SLA breach detection and alerts
- SLA reports and analytics

**Entities Owned**:
- `sla_policies` - SLA policy definitions
- `sla_records` - SLA tracking per ticket
- `sla_breaches` - SLA breach records
- `sla_metrics` - SLA performance metrics

**Integration Points**:
- **notifications-service**: SLA breach alerts

### 6. Knowledge Base

**Ticketing-Specific Features**:
- Knowledge base articles
- Article categories and tags
- Article search and recommendations
- Article views and helpfulness tracking
- Article versioning

**Entities Owned**:
- `kb_categories` - Knowledge base categories
- `kb_articles` - Article records
- `kb_article_tags` - Article tags
- `kb_article_versions` - Article version history
- `kb_article_views` - View tracking
- `kb_article_feedback` - Helpfulness feedback

**Integration Points**:
- **Search**: Full-text search with PostgreSQL or Elasticsearch

### 7. Customer Satisfaction

**Ticketing-Specific Features**:
- Satisfaction surveys (CSAT, NPS)
- Survey responses and analytics
- Feedback collection
- Response tracking

**Entities Owned**:
- `satisfaction_surveys` - Survey definitions
- `satisfaction_responses` - Survey responses
- `satisfaction_metrics` - Satisfaction analytics

**Integration Points**:
- **notifications-service**: Survey delivery

### 8. Analytics & Reporting

**Ticketing-Specific Features**:
- Ticket volume analytics
- Agent performance metrics
- Response time analytics
- Resolution time analytics
- Customer satisfaction trends
- SLA compliance reports

**Entities Owned**:
- `ticket_analytics` - Pre-aggregated analytics
- `agent_metrics` - Agent performance data

**Integration Points**:
- **Superset**: BI dashboards and reports

---

## Cross-Cutting Concerns

### Testing
- Go test suites with table-driven tests
- Testcontainers for integration testing
- Pact for contract tests
- Performance testing for high-volume scenarios

### Observability
- Structured logging (zap)
- Tracing via OpenTelemetry
- Metrics exported via Prometheus
- Distributed tracing via Tempo/Jaeger

### Security
- OWASP ASVS baseline
- TLS everywhere
- Secrets via Vault/Parameter Store
- Rate limiting & anomaly detection middleware
- JWT validation via auth-service
- Row-level security with tenant_id

### Scalability
- Stateless HTTP layer
- Background workers via NATS/Redis streams
- Database connection pooling
- Caching strategy for frequently accessed data

### Data Modelling
- Ent schemas as single source of truth
- Tenant/outlet discovery webhooks
- Outbox pattern for reliable domain events
- Vector columns for semantic search (pgvector)

---

## API & Protocol Strategy

- **REST-first**: Versioned routes (`/api/v1/{tenantID}/tickets`), documented via OpenAPI
- **WebSocket/SSE**: Real-time ticket updates
- **Webhooks**: External provider callbacks, ticket events
- **Idempotency**: Keys, correlation IDs, distributed tracing context propagation

---

## Compliance & Risk Controls

- Align with Kenya Data Protection Act: explicit consent flows, user data export/delete endpoints, audit logging
- SLA compliance tracking and reporting
- Disaster recovery playbook, RTO/RPO targets (<1 hour)

---

## Sprint Delivery Plan

See `docs/sprints/` folder for detailed sprint plans:
- Sprint 0: Foundations
- Sprint 1: Identity & Access Management
- Sprint 2: Ticket Management Core
- Sprint 3: Communication & Attachments
- Sprint 4: Assignment & Routing
- Sprint 5: SLA Management
- Sprint 6: Knowledge Base
- Sprint 7: Analytics & Hardening
- Sprint 8: Launch & Handover

---

## Runtime Ports & Environments

- **Local development**: Service runs on port **4107**
- **Cloud deployment**: All backend services listen on **port 4000** for consistency behind ingress controllers

---

## References

- [Integration Guide](docs/integrations.md)
- [Entity Relationship Diagram](docs/erd.md)
- [Superset Integration](docs/superset-integration.md)
- [Sprint Plans](docs/sprints/)

