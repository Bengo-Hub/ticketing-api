# Sprint 0: Foundation

**Status: Partial.** The service, its binaries, logging, migrations, metrics, readiness and CI are in place (production since 4f03d8e, 2026-11-16). Tracing and database integration tests are not built.

## Scope

Go service scaffolding, configuration, logging, the database and migration tooling, the HTTP server, auth-api integration, event publishing, observability and CI.

## Tasks

- [x] Go module, clean module layout under `internal/modules` and `internal/platform`, multi-stage `Dockerfile` with the api, migrate, worker and seed binaries
- [x] `envconfig` configuration with defaults (`internal/config/config.go`)
- [x] zap structured logging with request ids (`httpware.RequestID`, `httpware.Logging`, `httpware.Recover`)
- [x] chi router with Swagger UI at `/v1/docs/`
- [x] PostgreSQL with Ent, pool limits sized for the shared server
- [x] Versioned Atlas migrations embedded in `ticketing-migrate`, applied by `scripts/entrypoint.sh` at pod start under a Postgres advisory lock, with the baseline rule for databases built by auto-migrate (4f03d8e). Before that commit the service used Ent auto-migrate and the `.sql` files were never applied.
- [x] Redis through the shared cache, NATS JetStream stream setup
- [x] Transactional outbox and the shared-events background publisher
- [x] Real Prometheus `/metrics` on port 9090 with request count and latency by route pattern (4f03d8e)
- [x] `/readyz` pings Postgres, Redis and NATS; only Postgres takes the pod out (4f03d8e)
- [x] CI runs `go vet ./...` and `go test -timeout 300s ./...` before the image build (4f03d8e)
- [x] Shared libraries at auth-client v0.20.0, httpware v0.20.0, shared-events v0.11.1, cache v0.5.10, pagination v0.5.0, shared-ratelimit v0.3.1
- [ ] OpenTelemetry tracing
- [ ] Testcontainers repository tests against PostgreSQL (tickets, SLA, analytics), including the ticket-number concurrency test against a real database. Today `sequence_test.go` checks per-tenant numbering with 50 concurrent creates over a fake repository.
- [ ] Database query, cache and event publishing metrics

## Acceptance criteria

- A fresh database migrates from empty, and a database built by auto-migrate records the baseline and runs only later files. Met.
- `/readyz` answers 503 when Postgres is unreachable. Met.
- CI fails the build on a vet or test failure. Met.
- Traces reach the fleet tracing backend. Not met.
