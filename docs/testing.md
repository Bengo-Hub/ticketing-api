# Ticketing API Tests

ticketing-api has two kinds of Go tests.

- **Unit tests** run with a plain `go test ./...`. They use fakes and need no database. CI runs them before the image build.
- **Integration tests** run the real Ent repositories, the SLA scans and the analytics rollup against PostgreSQL. Every file carries the build tag `integration`, so a plain `go test ./...` neither compiles nor runs them.

The 200k ticket analytics benchmark is separate again (build tag `benchmark`, see `internal/modules/analytics/benchmark_test.go`).

## Running the integration tests

Against a local PostgreSQL 17:

```
TEST_DATABASE_URL=postgres://<user>:<password>@localhost:5432/<database>?sslmode=disable \
  go test -tags integration -timeout 10m ./...
```

Without `TEST_DATABASE_URL` the helper starts a throwaway `postgres:17-alpine` container through testcontainers-go, which needs Docker. When neither is available every integration test skips with the reason.

One package at a time works too, for example `go test -tags integration ./internal/modules/tickets/ -run Integration`.

| Variable | Effect |
|---|---|
| `TEST_DATABASE_URL` | The server to use. The tests create and drop their own schemas on it and leave everything else alone, so any local database works. |
| `TEST_KEEP_SCHEMA` | Set to anything to keep each package's schema after the run, for looking at the rows. |

## How the database is prepared

`internal/testsupport` does the setup for each test package from its `TestMain`:

1. It creates `pg_trgm` in `public` if it is missing.
2. It creates a schema named after the package (`it_tickets_<random>`) and points the pool's `search_path` at it, then `public`.
3. It checks `atlas.sum` and applies every file in `internal/ent/migrate/migrations` in order, each in its own transaction, as `cmd/migrate` does. The tests never use `Schema.Create`, so they prove the migrations as well as the code.
4. It drops the schema when the package's tests end.

Tests in one package share the schema and keep apart by using fresh tenant ids. The analytics tests empty the ticket and rollup tables first, because the rollup watermark is global.

## What they cover

| Package | Tests |
|---|---|
| `internal/testsupport` | The migrated schema matches the Ent schema (Ent finds nothing to change); every hand-written expression and partial index exists |
| `internal/modules/tickets` | Per-tenant numbering under 50 concurrent creates over two tenants; keyset paging in all three sorts, including tied `created_at`; list filters and tags; pg_trgm search on subject, description, requester and number; `source_ref` idempotency, including after a soft delete and inside a transaction; channel comment dedupe on (tenant, channel, external_id); tenant isolation of every read and write |
| `internal/modules/tickets` (SLA) | Deadlines in a business calendar with a holiday, read back from the database; pause on `pending_customer` and resume with moved deadlines; `pending_internal` without pausing; the breach scan selecting the right tickets across tenants and recording each breach once; the pre-breach warning scan and its dedupe |
| `internal/modules/analytics` | The watermark and the settle horizon; incremental rebuild when a ticket is resolved again, its deadline moves or it is deleted; totals, trend and breakdown equal to counts on the tickets table over hour and day windows with filters; the live backlog queries using `ticket_open_backlog` |
| `internal/modules/rbac` | Role grant and revoke rows, the agent profile raised, kept and deactivated with the last staff role, regrant renewing the row, the audit log with its filters and paging |

## Known failures

`TestMigrationsCreateHandWrittenIndexes` and `TestMigrationsMatchEntSchema` fail until the migration for `ticket_tenant_sla_due`, `agent_tenant_id_created_at_id` and `team_tenant_id_created_at_id` is generated (see Migrations in [architecture.md](architecture.md)). They are meant to: they are the check that the migration directory has caught up with the Ent schema.
