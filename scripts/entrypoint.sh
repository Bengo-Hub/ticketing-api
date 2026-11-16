#!/bin/sh
# Entrypoint for the ticketing-api container: apply the versioned migrations, then start the API.
#
# Migrations run here on every pod start, as in every fleet service. ticketing-migrate takes a
# Postgres advisory lock, so two pods starting together apply each file once, and a pod that finds
# nothing pending starts at once. The SLA breach scan runs only in the ticketing-worker
# Deployment, never inside API pods. Seeding is a manual step (ticketing-seed needs a tenant).

set -e

# The direct PostgreSQL URL bypasses PgBouncer transaction mode, which drops the advisory lock.
MIGRATE_URL="${POSTGRES_MIGRATE_URL:-$POSTGRES_URL}"

echo "Applying migrations..."
MAX_RETRIES=60
RETRY_COUNT=0

# Captured, not swallowed, so a real migration failure is visible on every attempt.
until MIGRATE_OUTPUT=$(POSTGRES_MIGRATE_URL="$MIGRATE_URL" /app/ticketing-migrate 2>&1) || [ $RETRY_COUNT -eq $MAX_RETRIES ]; do
  RETRY_COUNT=$((RETRY_COUNT+1))
  echo "Migration attempt $RETRY_COUNT/$MAX_RETRIES failed:"
  echo "$MIGRATE_OUTPUT"
  sleep 5
done

if [ $RETRY_COUNT -eq $MAX_RETRIES ]; then
  echo "Migration failed after $MAX_RETRIES attempts. Last error:"
  echo "$MIGRATE_OUTPUT"
  exit 1
fi

echo "$MIGRATE_OUTPUT"
echo "Starting ticketing-api"
exec /app/ticketing-api
