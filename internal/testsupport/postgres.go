//go:build integration

// Package testsupport gives the integration tests a real PostgreSQL with the versioned migrations
// applied. Every file in it, and every test that uses it, carries the integration build tag, so a
// plain `go test ./...` neither compiles nor runs any of it.
//
// The server comes from TEST_DATABASE_URL when it is set, otherwise from a throwaway
// testcontainers postgres:17 container when Docker is reachable. When neither is available the
// tests skip. Each test package gets its own schema on that server (Main), migrated with the real
// files in internal/ent/migrate/migrations, so the tests prove the migrations as well as the code.
package testsupport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	atlasmigrate "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // the pgx database/sql driver

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/migrate"
)

const (
	// EnvURL names the server the tests use. The schema per package is created on it and dropped
	// afterwards; the database itself is left alone.
	EnvURL = "TEST_DATABASE_URL"
	// EnvKeep, when set to anything, keeps each package's schema after the run for inspection.
	EnvKeep = "TEST_KEEP_SCHEMA"
	// maxConns bounds each package's pool so the concurrency tests stay under max_connections.
	maxConns = 30
)

// DB is one migrated schema: the pool (search_path set to the schema, then public for pg_trgm)
// and an ent client over the same pool.
type DB struct {
	SQL    *sql.DB
	Ent    *ent.Client
	Schema string
}

var (
	shared  *DB
	skipWhy = "integration database not set up: TestMain must call testsupport.Main"
)

// Main is the body of a test package's TestMain. It finds a server, creates and migrates a fresh
// schema named prefix_<random>, runs the package's tests and drops the schema. A prefix is
// lower-case letters, digits and underscores (it_tickets).
func Main(m *testing.M, prefix string) {
	os.Exit(run(m, prefix))
}

func run(m *testing.M, prefix string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	serverURL, stop, err := server(ctx)
	if err != nil {
		// No server: the tests run and skip with the reason.
		skipWhy = err.Error()
		return m.Run()
	}
	defer stop()
	db, drop, err := Setup(ctx, serverURL, prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testsupport: %v\n", err)
		return 1
	}
	defer drop()
	shared = db
	return m.Run()
}

// server returns TEST_DATABASE_URL, or starts a container when it is unset.
func server(ctx context.Context) (string, func(), error) {
	if u := strings.TrimSpace(os.Getenv(EnvURL)); u != "" {
		return u, func() {}, nil
	}
	u, stop, err := startContainer(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("%s is not set and no Postgres container could start (%v)", EnvURL, err)
	}
	return u, stop, nil
}

// Open returns the package's migrated schema, or skips the test when there is no server.
func Open(t testing.TB) *DB {
	t.Helper()
	if shared == nil {
		t.Skip(skipWhy)
	}
	return shared
}

// Setup creates a fresh schema on the server, applies every migration to it and returns it with a
// function that closes the pool and drops the schema.
func Setup(ctx context.Context, serverURL, prefix string) (*DB, func(), error) {
	admin, err := sql.Open("pgx", serverURL)
	if err != nil {
		return nil, nil, err
	}
	if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		return nil, nil, fmt.Errorf("connect to %s: %w", EnvURL, err)
	}
	// The trigram indexes need pg_trgm. It lives in public, which stays on every search_path, so
	// the migration's own CREATE EXTENSION IF NOT EXISTS is a no-op inside the test schema.
	// IF NOT EXISTS is not race-safe across packages starting together: the loser fails on
	// pg_extension_name_index although the extension now exists.
	if _, err := admin.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil &&
		!strings.Contains(err.Error(), "pg_extension_name_index") && !strings.Contains(err.Error(), "already exists") {
		_ = admin.Close()
		return nil, nil, fmt.Errorf("create pg_trgm: %w", err)
	}
	schema := prefix + "_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close()
		return nil, nil, fmt.Errorf("create schema %s: %w", schema, err)
	}
	dropSchema := func() {
		if os.Getenv(EnvKeep) == "" {
			_, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		}
		_ = admin.Close()
	}
	dsn, err := withSearchPath(serverURL, schema+",public")
	if err != nil {
		dropSchema()
		return nil, nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		dropSchema()
		return nil, nil, err
	}
	db.SetMaxOpenConns(maxConns)
	if err := Migrate(ctx, db); err != nil {
		_ = db.Close()
		dropSchema()
		return nil, nil, err
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	return &DB{SQL: db, Ent: client, Schema: schema}, func() {
		_ = client.Close() // closes db as well
		dropSchema()
	}, nil
}

// withSearchPath adds search_path to a URL or a key=value DSN. pgx sends unknown parameters to the
// server as run-time parameters, so every pooled connection starts in the schema.
func withSearchPath(dsn, path string) (string, error) {
	if !strings.Contains(dsn, "://") {
		return dsn + " search_path=" + path, nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", EnvURL, err)
	}
	q := u.Query()
	q.Set("search_path", path)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Migrate applies every versioned migration in order, each file in its own transaction as
// cmd/migrate does. It checks atlas.sum first, as the atlas executor in cmd/migrate does, so a
// file edited without rehashing fails here too.
func Migrate(ctx context.Context, db *sql.DB) error {
	if err := atlasmigrate.Validate(migrate.Dir); err != nil {
		return fmt.Errorf("migration directory does not match atlas.sum: %w", err)
	}
	files, err := migrate.Dir.Files()
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if len(files) == 0 {
		return errors.New("no migration files found")
	}
	for _, f := range files {
		stmts, err := f.Stmts()
		if err != nil {
			return fmt.Errorf("parse %s: %w", f.Name(), err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply %s: %w\nstatement: %s", f.Name(), err, stmt)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", f.Name(), err)
		}
	}
	return nil
}

// Exec runs one statement and fails the test on error.
func (d *DB) Exec(t testing.TB, query string, args ...any) {
	t.Helper()
	if _, err := d.SQL.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// Int64 runs a query returning one integer and fails the test on error.
func (d *DB) Int64(t testing.TB, query string, args ...any) int64 {
	t.Helper()
	var n sql.NullInt64
	if err := d.SQL.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n.Int64
}

// Truncate empties the tables (and whatever references them) in the package's schema. Tests that
// read global state, such as the rollup watermark, start from it.
func (d *DB) Truncate(t testing.TB, tables ...string) {
	t.Helper()
	d.Exec(t, "TRUNCATE "+strings.Join(tables, ", ")+" CASCADE")
}

// Explain returns the text plan of a query with its arguments.
func (d *DB) Explain(t testing.TB, query string, args ...any) string {
	t.Helper()
	rows, err := d.SQL.QueryContext(context.Background(), "EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, query)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
