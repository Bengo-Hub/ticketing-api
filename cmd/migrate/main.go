package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"regexp"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/migrate"
)

func maskPassword(url string) string {
	re := regexp.MustCompile(`://([^:]+):([^@]+)@`)
	return re.ReplaceAllString(url, "://$1:****@")
}

// migrationLockKey serializes migrations across pods (every replica's entrypoint runs this
// binary); concurrent schema runs can race on the same DDL. Stable and unique per service
// ("TKTM", ticketing migrate).
const migrationLockKey int64 = 0x544B_544D

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

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
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()
	// One connection: the advisory lock and every migration statement share one session.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		log.Fatalf("acquire migration lock: %v", err)
	}

	if os.Getenv("RESET_DB") == "true" {
		log.Println("resetting database...")
		if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS public CASCADE"); err != nil {
			log.Fatalf("failed to drop schema: %v", err)
		}
		if _, err := db.ExecContext(ctx, "CREATE SCHEMA public"); err != nil {
			log.Fatalf("failed to create schema: %v", err)
		}
		log.Println("database reset successful")
	}

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	migrateErr := client.Schema.Create(ctx, schema.WithDir(migrate.Dir))
	if _, err := db.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockKey); err != nil {
		log.Printf("release migration lock: %v", err)
	}
	if migrateErr != nil {
		log.Fatalf("failed to run migrations: %v", migrateErr)
	}

	log.Println("database migrations applied successfully")
}
