//go:build benchmark

package analytics

// The Fix 5 benchmark: 200k tickets, every /analytics route under 300 ms p95.
//
//	ANALYTICS_BENCH_DATABASE_URL=postgres://.../ticketing_bench?sslmode=disable \
//	  go test -tags benchmark -run TestAnalyticsP95 -timeout 60m ./internal/modules/analytics/
//
// The database must be a throwaway one: the test creates the schema with ent and deletes every
// ticket, rollup and CSAT row of its tenant before seeding.

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/platform/access"
)

const (
	benchTickets = 200_000
	benchRuns    = 60
	benchP95     = 300 * time.Millisecond
)

var benchTenant = uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000012")

// seedStatements write teams, agents and categories, then 200k tickets spread over the last 365
// days with every status, priority and channel, SLA deadlines (a fifth breached), first responses
// on most and resolutions on about 70%, and CSAT responses on a fifth of the resolved ones. $1 is
// the tenant; $2, where used, the ticket count. No moment lies in the future.
var seedStatements = []struct {
	sql       string
	withCount bool
}{
	{sql: `INSERT INTO teams (id, tenant_id, name, is_active, created_at, updated_at)
SELECT md5('team' || g)::uuid, $1, 'Team ' || g, true, now(), now() FROM generate_series(1, 5) g
ON CONFLICT DO NOTHING`},
	{sql: `INSERT INTO ticket_categories (id, tenant_id, name, slug, sort_order, is_active, created_at, updated_at)
SELECT md5('cat' || g)::uuid, $1, 'Category ' || g, 'category-' || g, g, true, now(), now() FROM generate_series(1, 8) g
ON CONFLICT DO NOTHING`},
	{sql: `INSERT INTO agents (id, tenant_id, user_id, team_id, display_name, email, role, status,
  max_concurrent_tickets, current_ticket_count, is_active, created_at, updated_at)
SELECT md5('agent' || g)::uuid, $1, md5('user' || g)::uuid, md5('team' || (g % 5 + 1))::uuid,
  'Agent ' || g, 'agent' || g || '@bench.test', 'agent', 'available', 10, 0, true, now(), now()
FROM generate_series(1, 20) g
ON CONFLICT DO NOTHING`},
	{withCount: true, sql: `INSERT INTO tickets (id, tenant_id, ticket_number, customer_id, category_id, assigned_agent_id,
  assigned_team_id, subject, description, status, priority, channel, sla_response_due_at, sla_resolution_due_at,
  sla_response_breached, sla_resolution_breached, sla_paused_minutes, first_response_at, resolved_at,
  escalation_level, created_at, updated_at)
SELECT gen_random_uuid(), $1, 'TKT-' || lpad(g::text, 7, '0'), gen_random_uuid(),
  CASE WHEN g % 9 = 0 THEN NULL ELSE md5('cat' || (g % 8 + 1))::uuid END,
  CASE WHEN g % 7 = 0 THEN NULL ELSE md5('agent' || (g % 20 + 1))::uuid END,
  md5('team' || ((g % 20 + 1) % 5 + 1))::uuid,
  'Benchmark ticket ' || g, 'Seeded for the analytics benchmark',
  CASE WHEN g % 10 < 7 THEN 'resolved' WHEN g % 10 = 7 THEN 'open' WHEN g % 10 = 8 THEN 'pending_customer' ELSE 'closed' END,
  (ARRAY['low','normal','normal','high','urgent','critical'])[g % 6 + 1],
  (ARRAY['web','email','email','whatsapp','api','chat','phone'])[g % 7 + 1],
  c.created + interval '4 hours', c.created + interval '2 days',
  g % 5 = 0, g % 5 = 1, 0,
  CASE WHEN g % 10 < 9 THEN LEAST(now(), c.created + (g % 300) * interval '1 minute') END,
  CASE WHEN g % 10 < 7 THEN LEAST(now(), c.created + (g % 4000) * interval '1 minute') END,
  0, c.created, LEAST(now(), c.created + (g % 4000) * interval '1 minute')
FROM generate_series(1, $2::int) g
CROSS JOIN LATERAL (SELECT now() - (g::float8 / $2::int) * interval '365 days' AS created) c`},
	{sql: `INSERT INTO csat_responses (id, tenant_id, ticket_id, score, comment, token_hash, submitted_at)
SELECT gen_random_uuid(), t.tenant_id, t.id, (abs(hashtext(t.ticket_number)) % 5) + 1,
  CASE WHEN abs(hashtext(t.ticket_number)) % 3 = 0 THEN 'Benchmark comment' END,
  md5(t.id::text) || md5(t.ticket_number), LEAST(now(), t.resolved_at + interval '1 hour')
FROM tickets t
WHERE t.tenant_id = $1 AND t.resolved_at IS NOT NULL AND abs(hashtext(t.id::text)) % 5 = 0`},
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestAnalyticsP95At200kTickets(t *testing.T) {
	url := os.Getenv("ANALYTICS_BENCH_DATABASE_URL")
	if url == "" {
		t.Skip("ANALYTICS_BENCH_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	for _, stmt := range []string{
		`DELETE FROM ticket_rollups WHERE tenant_id = $1`,
		`DELETE FROM ticket_rollup_dims WHERE tenant_id = $1`,
		`DELETE FROM ticket_rollup_marks WHERE tenant_id = $1`,
		`DELETE FROM csat_responses WHERE tenant_id = $1`,
		`DELETE FROM tickets WHERE tenant_id = $1`,
	} {
		if _, err := db.ExecContext(ctx, stmt, benchTenant); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM rollup_watermarks WHERE name = $1`, RollupName); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i, s := range seedStatements {
		args := []any{benchTenant}
		if s.withCount {
			args = append(args, benchTickets)
		}
		if _, err := db.ExecContext(ctx, s.sql, args...); err != nil {
			t.Fatalf("seed statement %d: %v", i, err)
		}
	}
	// A bulk load leaves the visibility map empty until autovacuum runs; a live table is vacuumed
	// continuously, and the backlog counts rely on index-only scans that need it.
	for _, table := range []string{"tickets", "csat_responses"} {
		if _, err := db.ExecContext(ctx, "VACUUM ANALYZE "+table); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("seeded %d tickets in %s", benchTickets, time.Since(start).Round(time.Millisecond))

	// The backfill, run to the end without the per-tick budget.
	rollup := NewRollup(db, zap.NewNop())
	rollup.Batch, rollup.Budget, rollup.Settle = 10_000, 24*time.Hour, 0
	rollup.now = func() time.Time { return time.Now().Add(time.Hour) }
	start = time.Now()
	n, err := rollup.Run(ctx)
	if err != nil {
		t.Fatalf("rollup: %v", err)
	}
	backfill := time.Since(start).Round(time.Millisecond)
	for _, table := range []string{"ticket_rollups", "ticket_rollup_dims"} {
		if _, err := db.ExecContext(ctx, "VACUUM ANALYZE "+table); err != nil {
			t.Fatal(err)
		}
	}
	var cells, series int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM ticket_rollups WHERE tenant_id = $1`, benchTenant).Scan(&cells)
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM ticket_rollup_dims WHERE tenant_id = $1`, benchTenant).Scan(&series)
	t.Logf("backfill read %d tickets into %d fine cells and %d series rows in %s", n, cells, series, backfill)

	// The rollups must agree with the tickets table and with each other: every resolution counted
	// once, by resolved_at, in the fine cells, the total series and each dimension's series.
	sum := func(query string) int64 {
		var v int64
		if err := db.QueryRowContext(ctx, query, benchTenant).Scan(&v); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return v
	}
	want := sum(`SELECT count(*) FROM tickets WHERE tenant_id = $1 AND resolved_at IS NOT NULL AND deleted_at IS NULL`)
	for _, q := range []string{
		`SELECT COALESCE(sum(resolved_count), 0) FROM ticket_rollups WHERE tenant_id = $1 AND granularity = 'day'`,
		`SELECT COALESCE(sum(resolved_count), 0) FROM ticket_rollups WHERE tenant_id = $1 AND granularity = 'hour'`,
		`SELECT COALESCE(sum(resolved_count), 0) FROM ticket_rollup_dims WHERE tenant_id = $1 AND granularity = 'day' AND dim_kind = 'all'`,
		`SELECT COALESCE(sum(resolved_count), 0) FROM ticket_rollup_dims WHERE tenant_id = $1 AND granularity = 'hour' AND dim_kind = 'all'`,
		`SELECT COALESCE(sum(resolved_count), 0) FROM ticket_rollup_dims WHERE tenant_id = $1 AND granularity = 'day' AND dim_kind = 'team'`,
		`SELECT COALESCE(sum(resolved_count), 0) FROM ticket_rollup_dims WHERE tenant_id = $1 AND granularity = 'day' AND dim_kind = 'agent'`,
		`SELECT COALESCE(sum(resolved_count), 0) FROM ticket_rollup_dims WHERE tenant_id = $1 AND granularity = 'day' AND dim_kind = 'priority'`,
		`SELECT COALESCE(sum(resolved_count), 0) FROM ticket_rollup_dims WHERE tenant_id = $1 AND granularity = 'day' AND dim_kind = 'team_agent'`,
		`SELECT COALESCE(sum(resolved_count), 0) FROM ticket_rollup_dims WHERE tenant_id = $1 AND granularity = 'day' AND dim_kind = 'team_priority'`,
	} {
		if got := sum(q); got != want {
			t.Fatalf("%s = %d, tickets resolved %d", q, got, want)
		}
	}

	h := NewHandler(NewService(NewRepository(db)), nil, zap.NewNop())
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			a := &access.Access{TenantID: benchTenant, TenantSlug: "bench", Perms: []string{permAnalyticsView}}
			next.ServeHTTP(w, req.WithContext(access.With(req.Context(), a)))
		})
	})
	r.Route("/{tenant}", h.RegisterRoutes)

	// Every route over: the default 30 days, one year, and filtered windows of both lengths. One
	// filter reads its series plus fine cells for the other breakdowns; two filters read only fine
	// cells.
	year := "from=" + time.Now().AddDate(-1, 0, 1).Format("2006-01-02")
	team := "team_id=" + uuidMD5("team1")
	agent := "agent_id=" + uuidMD5("agent3")
	paths := []string{}
	for _, view := range []string{"tickets", "agents", "sla", "csat"} {
		base := "/bench/analytics/" + view
		paths = append(paths,
			base,
			base+"?"+year,
			fmt.Sprintf("%s?%s&priority=high", base, team),
			fmt.Sprintf("%s?%s&%s", base, year, team),
			fmt.Sprintf("%s?%s&%s", base, year, agent),
			fmt.Sprintf("%s?%s&%s&priority=high", base, year, team),
		)
	}
	for _, p := range paths {
		var took []time.Duration
		for i := 0; i < benchRuns; i++ {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			began := time.Now()
			r.ServeHTTP(rec, req)
			took = append(took, time.Since(began))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", p, rec.Code, rec.Body.String())
			}
		}
		sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
		p95 := took[int(float64(len(took))*0.95)-1]
		t.Logf("%-70s p50 %8s  p95 %8s", p, took[len(took)/2].Round(time.Microsecond), p95.Round(time.Microsecond))
		if p95 > benchP95 {
			t.Errorf("%s: p95 %s is over %s", p, p95, benchP95)
		}
	}
}

// uuidMD5 is the seed's md5(text)::uuid, so a filter matches a seeded row.
func uuidMD5(s string) string {
	sum := md5Hex(s)
	return sum[0:8] + "-" + sum[8:12] + "-" + sum[12:16] + "-" + sum[16:20] + "-" + sum[20:32]
}
