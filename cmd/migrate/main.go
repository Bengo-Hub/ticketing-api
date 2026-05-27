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

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	if err := client.Schema.Create(ctx,
		schema.WithDir(migrate.Dir),
	); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	log.Println("database migrations applied successfully")
}
