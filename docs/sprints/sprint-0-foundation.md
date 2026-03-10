# Sprint 0 - Foundation

**Duration**: Week 1  
**Status**: ⏳ Pending

---

## Overview

Sprint 0 establishes the foundational infrastructure, project structure, and core identity management capabilities for the Ticketing backend service.

---

## Objectives

1. Set up Go project scaffolding with clean architecture
2. Configure CI/CD pipeline with centralized devops-k8s workflows
3. Implement observability baseline (logging, metrics, tracing)
4. Deliver identity bootstrap with RBAC, OAuth2, and JWT session management
5. Define domain models, ERD, API guidelines, and service interface contracts

---

## Technology Stack

### Core Framework

**Language**: Go 1.22+  
**Architecture Pattern**: Clean/Hexagonal Architecture

**Project Structure**:
```
ticketing-api/
├── cmd/
│   ├── api/              # Main application entry point
│   ├── migrate/          # Database migration tool
│   └── seed/             # Database seeding tool
├── internal/
│   ├── app/              # Application initialization
│   ├── config/           # Configuration management
│   ├── ent/               # Ent ORM generated code
│   ├── http/              # HTTP handlers and routing
│   │   ├── handlers/      # Request handlers
│   │   └── router/        # Route definitions
│   ├── modules/           # Domain modules
│   │   ├── identity/      # Identity module
│   │   ├── tickets/       # Ticket management module
│   │   └── knowledge/     # Knowledge base module
│   ├── platform/          # Platform services
│   │   ├── cache/         # Redis cache
│   │   ├── database/      # PostgreSQL connection
│   │   └── events/        # NATS event bus
│   ├── shared/            # Shared utilities
│   │   ├── logger/        # Structured logging
│   │   └── middleware/    # HTTP middleware
│   └── validators/        # Input validation
├── docs/                  # Documentation
├── config/                # Configuration files
├── go.mod                 # Go module definition
├── go.sum                 # Go module checksums
├── Dockerfile             # Container image definition
└── Makefile               # Build automation
```

**HTTP Framework**: chi router  
**API Documentation**: OpenAPI/Swagger (Swagger UI)

### Data Layer

**Database**: PostgreSQL 16+  
**ORM**: Ent (schema-as-code)  
**Connection Pool**: pgx/v5  
**Migrations**: Ent migrations

**Database Connection**:
- PostgreSQL connection pool using pgx/v5
- Ent ORM client for type-safe database operations
- Connection pooling with configurable limits

**Caching**: Redis 7+
- Redis client for caching and ephemeral state
- Connection pooling and health checks

**Message Broker**: NATS JetStream
- NATS connection for event publishing
- JetStream for reliable event delivery

---

## Deliverables

### 1. Project Scaffolding

**Tasks**:
- Initialize Go module (`go mod init`)
- Create directory structure
- Set up Makefile with common commands
- Configure `.gitignore`
- Set up Dockerfile (multi-stage)
- Create `README.md` with setup instructions

**Acceptance Criteria**:
- Project structure follows clean architecture
- Dockerfile builds successfully
- Makefile commands work (build, test, run, migrate)

### 2. Configuration Management

**Tasks**:
- Implement configuration loader using `envconfig`
- Define configuration struct with all environment variables
- Set up configuration validation
- Create `.env.example` file

**Configuration Variables**:
- Database connection string
- Redis connection string
- NATS connection string
- Auth service URL
- JWT validation settings
- Server port and host
- Log level
- Environment (dev/staging/prod)

**Acceptance Criteria**:
- Configuration loads from environment variables
- Validation errors are clear and actionable
- Default values are sensible

### 3. Structured Logging

**Tasks**:
- Set up zap logger with structured output
- Configure log levels (debug, info, warn, error)
- Add request ID middleware for correlation
- Set up log rotation (if file-based)

**Acceptance Criteria**:
- All logs are structured JSON
- Request IDs are included in all log entries
- Log levels are configurable

### 4. Observability

**Tasks**:
- Set up Prometheus metrics exporter
- Configure OpenTelemetry tracing
- Add health check endpoint
- Add readiness/liveness probes

**Metrics**:
- HTTP request duration
- HTTP request count
- Database query duration
- Cache hit/miss rates
- Event publishing success/failure

**Acceptance Criteria**:
- Metrics are exported to Prometheus
- Traces are sent to Jaeger/Tempo
- Health endpoints return correct status

### 5. Database Setup

**Tasks**:
- Set up PostgreSQL connection pool
- Initialize Ent schemas (basic identity tables)
- Create migration tool
- Set up database seeding tool
- Configure connection pooling

**Initial Schemas**:
- `teams` - Support teams
- `agents` - Agent profiles
- `ticket_categories` - Ticket categories
- `ticket_priorities` - Ticket priorities

**Acceptance Criteria**:
- Database migrations run successfully
- Connection pool is configured correctly
- Seed data can be loaded

### 6. HTTP Server Setup

**Tasks**:
- Set up chi router
- Configure middleware (CORS, logging, recovery, auth)
- Set up route groups
- Create health check endpoint
- Set up OpenAPI/Swagger documentation

**Middleware Stack**:
- Request ID middleware
- Logging middleware
- Recovery middleware
- CORS middleware
- Authentication middleware (JWT validation)

**Acceptance Criteria**:
- Server starts and responds to health checks
- Routes are properly organized
- Swagger UI is accessible

### 7. Auth Service Integration

**Tasks**:
- Integrate `shared/auth-client` library
- Set up JWT validation middleware
- Implement user sync endpoint
- Configure JWKS caching

**Integration Points**:
- JWT token validation
- User identity sync
- Tenant/outlet discovery webhooks

**Acceptance Criteria**:
- JWT tokens are validated correctly
- Protected routes require valid tokens
- User sync works correctly

### 8. Event Publishing

**Tasks**:
- Set up NATS JetStream connection
- Implement event publisher
- Set up outbox pattern for reliable events
- Create event schemas

**Initial Events**:
- `ticketing.ticket.created`
- `ticketing.ticket.updated`

**Acceptance Criteria**:
- Events are published to NATS
- Outbox pattern ensures reliability
- Event schemas are documented

### 9. CI/CD Pipeline

**Tasks**:
- Set up GitHub Actions workflow
- Configure build and test steps
- Set up Docker image building
- Configure ArgoCD deployment manifests

**Pipeline Steps**:
- Lint code
- Run tests
- Build Docker image
- Push to registry
- Deploy via ArgoCD

**Acceptance Criteria**:
- CI pipeline runs on PR and merge
- Docker images are built and pushed
- Deployment manifests are ready

### 10. Documentation

**Tasks**:
- Create API documentation (OpenAPI)
- Document project structure
- Create setup guide
- Document environment variables

**Acceptance Criteria**:
- API documentation is complete
- Setup guide is clear and actionable
- All environment variables are documented

---

## Testing Strategy

### Unit Tests
- Test all business logic functions
- Mock external dependencies
- Achieve >80% code coverage

### Integration Tests
- Test database operations
- Test API endpoints
- Use Testcontainers for database

### Acceptance Criteria
- All tests pass
- Code coverage >80%
- Integration tests run in CI

---

## Definition of Done

- [ ] Project structure is set up
- [ ] Configuration management works
- [ ] Logging is structured and configurable
- [ ] Observability is set up (metrics, tracing)
- [ ] Database connection and migrations work
- [ ] HTTP server starts and responds
- [ ] Auth service integration works
- [ ] Event publishing works
- [ ] CI/CD pipeline is configured
- [ ] Documentation is complete
- [ ] All tests pass
- [ ] Code review is complete

---

## Next Sprint

**Sprint 1**: Identity & Access Management
- Complete RBAC implementation
- Agent management
- Team management
- Permission system

