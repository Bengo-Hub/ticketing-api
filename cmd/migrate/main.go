// Command migrate applies the versioned Atlas migrations in internal/ent/migrate/migrations
// (embedded in the binary) under a Postgres advisory lock. Each file runs in its own transaction
// and is recorded in atlas_schema_revisions.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"time"

	atlasmigrate "ariga.io/atlas/sql/migrate"
	"ariga.io/atlas/sql/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bengobox/ticketing-service/internal/ent/migrate"
)

// migrationLockKey serializes migrations across pods and jobs. Stable and unique per service
// ("TKTM", ticketing migrate).
const migrationLockKey int64 = 0x544B_544D

// baselineVersion is the migration that describes the schema ent auto-migrate built before
// versioned migrations were adopted. A database that already has the tickets table but no
// revision history records it as applied without running it.
const baselineVersion = "20261116090000"

const operatorVersion = "ticketing-migrate"

func maskPassword(url string) string {
	re := regexp.MustCompile(`://([^:]+):([^@]+)@`)
	return re.ReplaceAllString(url, "://$1:****@")
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// The direct DSN is required: a session advisory lock does not survive PgBouncer transaction pooling.
	dbURL := os.Getenv("POSTGRES_MIGRATE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("POSTGRES_URL")
	}
	if dbURL == "" {
		log.Fatalln("POSTGRES_URL or POSTGRES_MIGRATE_URL is required")
	}
	log.Printf("connecting to database: %s", maskPassword(dbURL))

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	// One connection: the advisory lock, the revision reads and every migration share one session.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		log.Fatalf("acquire migration lock: %v", err)
	}
	applied, migrateErr := run(ctx, db)
	if _, err := db.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockKey); err != nil {
		log.Printf("release migration lock: %v", err)
	}
	if migrateErr != nil {
		log.Fatalf("migrate: %v", migrateErr)
	}
	log.Printf("migrations complete, %d file(s) applied", applied)
}

// run records the baseline when needed and applies every pending file, each in its own transaction.
func run(ctx context.Context, db *sql.DB) (int, error) {
	if os.Getenv("RESET_DB") == "true" {
		log.Println("RESET_DB=true: dropping and recreating the public schema")
		if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
			return 0, fmt.Errorf("reset schema: %w", err)
		}
	}
	if err := ensureRevisionTable(ctx, db); err != nil {
		return 0, fmt.Errorf("create revision table: %w", err)
	}
	opts, err := firstRunOptions(ctx, db)
	if err != nil {
		return 0, err
	}
	drv, err := postgres.Open(db)
	if err != nil {
		return 0, fmt.Errorf("open atlas driver: %w", err)
	}
	ex, err := atlasmigrate.NewExecutor(drv, migrate.Dir, &revisions{q: db},
		append(opts, atlasmigrate.WithOperatorVersion(operatorVersion))...)
	if err != nil {
		return 0, fmt.Errorf("new executor: %w", err)
	}
	pending, err := ex.Pending(ctx)
	if errors.Is(err, atlasmigrate.ErrNoPendingFiles) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("pending migrations: %w", err)
	}
	for i, f := range pending {
		log.Printf("applying %s", f.Name())
		if err := applyInTx(ctx, db, f); err != nil {
			return i, fmt.Errorf("apply %s: %w", f.Name(), err)
		}
	}
	return len(pending), nil
}

// firstRunOptions decides how a database with no revision history starts. A database that
// ent auto-migrate already built (it has the tickets table) is baselined at baselineVersion, so
// only later files run. Any other database without history is treated as new and runs every file;
// unrelated schemas on the same server (a local ent_dev, for example) are not a reason to refuse.
func firstRunOptions(ctx context.Context, db *sql.DB) ([]atlasmigrate.ExecutorOption, error) {
	n, err := countRevisions(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("count revisions: %w", err)
	}
	if n > 0 {
		return nil, nil
	}
	existing, err := tableExists(ctx, db, "tickets")
	if err != nil {
		return nil, fmt.Errorf("inspect existing schema: %w", err)
	}
	if existing {
		log.Printf("tables built by auto-migrate found: recording %s as the baseline", baselineVersion)
		return []atlasmigrate.ExecutorOption{atlasmigrate.WithBaselineVersion(baselineVersion)}, nil
	}
	log.Println("new database: applying every migration")
	return []atlasmigrate.ExecutorOption{atlasmigrate.WithAllowDirty(true)}, nil
}

// applyInTx runs one migration file and its revision record in a single transaction, so a failed
// statement leaves neither partial DDL nor a partial revision behind.
func applyInTx(ctx context.Context, db *sql.DB, f atlasmigrate.File) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	drv, err := postgres.Open(tx)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("open atlas driver on transaction: %w", err)
	}
	ex, err := atlasmigrate.NewExecutor(drv, migrate.Dir, &revisions{q: tx},
		atlasmigrate.WithOperatorVersion(operatorVersion))
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := ex.Execute(ctx, f); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
