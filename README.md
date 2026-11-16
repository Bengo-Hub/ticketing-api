# ticketing-api

Multi-tenant help desk API for the Codevertex platform: tickets, comments, categories, agents and teams, SLA policies, routing rules, a knowledge base and public support routes. Go, chi, Ent and PostgreSQL, with NATS JetStream for events and Redis for caches and limits.

## Binaries

| Binary | Purpose |
|---|---|
| `cmd/api` | HTTP API (port 4009) and Prometheus metrics (port 9090) |
| `cmd/migrate` | Versioned Atlas migrations under a Postgres advisory lock |
| `cmd/worker` | SLA breach scan, the only process that runs it |
| `cmd/seed` | Per-tenant seed of categories, SLA policies and knowledge base sets |

In the container, `scripts/entrypoint.sh` runs the migrations and then starts the API. The worker runs as its own Deployment. The seed is a manual step.

## Running locally

```sh
export POSTGRES_URL="postgres://<USER>:<REPLACE_ME>@localhost:5432/ticketing?sslmode=disable"
export AUTH_SERVICE_URL="http://localhost:8080"
export INTERNAL_SERVICE_KEY="<REPLACE_ME>"
go run ./cmd/migrate
go run ./cmd/api
go run ./cmd/worker
```

Redis defaults to `localhost:6379` and NATS to `nats://localhost:4222`. The full variable list is in [docs/architecture.md](docs/architecture.md). Tests run with `go vet ./...` and `go test -timeout 300s ./...`, the same commands CI runs.

## Shared libraries

| Library | Version |
|---|---|
| `github.com/Bengo-Hub/auth-client` (imported as `shared-auth-client`) | v0.20.0 |
| `github.com/Bengo-Hub/httpware` | v0.20.0 |
| `github.com/Bengo-Hub/shared-events` | v0.11.1 |
| `github.com/Bengo-Hub/cache` | v0.5.10 |
| `github.com/Bengo-Hub/pagination` | v0.5.0 |
| `github.com/Bengo-Hub/shared-ratelimit` | v0.3.1 |

## Docs

- [Plan, Find Flip Fix and sprint status](docs/plan.md)
- [Architecture](docs/architecture.md)
- [ERD](docs/erd.md)
- [Integrations and the internal create contract](docs/integrations.md)
- [Sprints](docs/sprints/)
- API reference: Swagger UI at `/v1/docs/`
