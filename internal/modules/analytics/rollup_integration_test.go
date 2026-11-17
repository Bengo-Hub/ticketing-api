//go:build integration

package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m, "it_analytics") }

// The rollup and its watermark are global across tenants, so every test here starts from empty
// tables.
func itFresh(t *testing.T) *testsupport.DB {
	t.Helper()
	db := testsupport.Open(t)
	db.Truncate(t, "tickets", "csat_responses", "ticket_rollups", "ticket_rollup_dims", "ticket_rollup_marks", "rollup_watermarks")
	return db
}

// itTicket is a ticket row as the rollup reads it.
type itTicket struct {
	ID, Tenant                  uuid.UUID
	Team                        *uuid.UUID
	Status, Priority, Channel   string
	Created, Updated            time.Time
	Resolved, FirstResponse     *time.Time
	ResponseDue, ResolutionDue  *time.Time
	ResponseBreached, ResBreach bool
	Deleted                     *time.Time
}

func itInsert(t *testing.T, db *testsupport.DB, tk *itTicket) *itTicket {
	t.Helper()
	if tk.ID == uuid.Nil {
		tk.ID = uuid.New()
	}
	if tk.Status == "" {
		tk.Status = "open"
		if tk.Resolved != nil {
			tk.Status = "resolved"
		}
	}
	if tk.Priority == "" {
		tk.Priority = "normal"
	}
	if tk.Channel == "" {
		tk.Channel = "web"
	}
	if tk.Updated.IsZero() {
		tk.Updated = tk.Created
	}
	db.Exec(t, `INSERT INTO tickets (id, tenant_id, ticket_number, customer_id, assigned_team_id, subject, description,
  status, priority, channel, sla_response_due_at, sla_resolution_due_at, sla_response_breached, sla_resolution_breached,
  first_response_at, resolved_at, created_at, updated_at, deleted_at)
VALUES ($1, $2, $3, $4, $5, 'Rollup test', 'Seeded', $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		tk.ID, tk.Tenant, "TKT-"+tk.ID.String()[:8], uuid.New(), tk.Team, tk.Status, tk.Priority, tk.Channel,
		tk.ResponseDue, tk.ResolutionDue, tk.ResponseBreached, tk.ResBreach, tk.FirstResponse, tk.Resolved,
		tk.Created, tk.Updated, tk.Deleted)
	return tk
}

func itRollup(db *testsupport.DB, now time.Time) *Rollup {
	r := NewRollup(db.SQL, zap.NewNop())
	r.Batch, r.Settle, r.Budget = 2, 0, time.Hour
	r.now = func() time.Time { return now }
	return r
}

func itRun(t *testing.T, r *Rollup, want int) {
	t.Helper()
	n, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if n != want {
		t.Fatalf("rollup read %d ticket changes, want %d", n, want)
	}
}

// itCell sums a counter over a tenant's fine cells of one bucket; itSeries the same over its
// total series row.
func itCell(t *testing.T, db *testsupport.DB, tenant uuid.UUID, granularity string, bucket time.Time, col string) int64 {
	t.Helper()
	return db.Int64(t, `SELECT COALESCE(sum(`+col+`), 0) FROM ticket_rollups
WHERE tenant_id = $1 AND granularity = $2 AND bucket_start = $3`, tenant, granularity, bucket)
}

func itSeries(t *testing.T, db *testsupport.DB, tenant uuid.UUID, granularity string, bucket time.Time, col string) int64 {
	t.Helper()
	return db.Int64(t, `SELECT COALESCE(sum(`+col+`), 0) FROM ticket_rollup_dims
WHERE tenant_id = $1 AND granularity = $2 AND bucket_start = $3 AND dim_kind = 'all'`, tenant, granularity, bucket)
}

// itCounts checks one bucket's counters in the fine cells and in the total series.
func itCounts(t *testing.T, db *testsupport.DB, tenant uuid.UUID, granularity string, bucket time.Time, want map[string]int64) {
	t.Helper()
	for col, n := range want {
		label := fmt.Sprintf("%s %s %s", granularity, bucket.Format(time.RFC3339), col)
		if got := itCell(t, db, tenant, granularity, bucket, col); got != n {
			t.Errorf("%s in ticket_rollups = %d, want %d", label, got, n)
		}
		if got := itSeries(t, db, tenant, granularity, bucket, col); got != n {
			t.Errorf("%s in ticket_rollup_dims = %d, want %d", label, got, n)
		}
	}
}

// The watermark follows (updated_at, id) of the last change read, a run with nothing new reads
// nothing, and a change inside the settle horizon waits for a later run.
func TestIntegrationRollupWatermark(t *testing.T) {
	db := itFresh(t)
	ctx := context.Background()
	tenant := uuid.New()
	base := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	var last *itTicket
	for i := 1; i <= 5; i++ {
		last = itInsert(t, db, &itTicket{Tenant: tenant, Created: base.Add(time.Duration(i) * time.Minute)})
	}

	r := itRollup(db, base.Add(time.Hour))
	itRun(t, r, 5) // three steps of at most 2
	var at time.Time
	var id uuid.UUID
	var caught *time.Time
	if err := db.SQL.QueryRowContext(ctx, `SELECT watermark_at, watermark_id, caught_up_at FROM rollup_watermarks WHERE name = $1`,
		RollupName).Scan(&at, &id, &caught); err != nil {
		t.Fatal(err)
	}
	if !at.Equal(last.Updated) || id != last.ID {
		t.Errorf("watermark = (%s, %s), want (%s, %s)", at, id, last.Updated, last.ID)
	}
	if caught == nil {
		t.Error("caught_up_at not set after a partial batch")
	}
	fresh, err := NewRepository(db.SQL).Freshness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.AsOf == nil || !fresh.AsOf.Equal(last.Updated) || !fresh.Complete {
		t.Errorf("freshness = %+v, want as of %s and complete", fresh, last.Updated)
	}
	itCounts(t, db, tenant, SourceHour, base, map[string]int64{"created_count": 5})
	itCounts(t, db, tenant, SourceDay, utcDay(base), map[string]int64{"created_count": 5})

	itRun(t, r, 0)

	itInsert(t, db, &itTicket{Tenant: tenant, Created: base.Add(10 * time.Minute)})
	itRun(t, r, 1)
	itCounts(t, db, tenant, SourceHour, base, map[string]int64{"created_count": 6})

	// Settle: a change less than Settle old is left for a later run.
	r.Settle = 2 * time.Minute
	r.now = func() time.Time { return base.Add(11 * time.Minute) }
	itInsert(t, db, &itTicket{Tenant: tenant, Created: base.Add(10*time.Minute + 30*time.Second)})
	itRun(t, r, 0)
	r.now = func() time.Time { return base.Add(13 * time.Minute) }
	itRun(t, r, 1)
	itCounts(t, db, tenant, SourceHour, base, map[string]int64{"created_count": 7})
}

// A ticket's change rebuilds the hours it counts in now and the hours it counted in before, so a
// re-resolve or a moved deadline leaves its old hour and day, and a delete leaves every bucket.
func TestIntegrationRollupIncrementalRebuild(t *testing.T) {
	db := itFresh(t)
	tenant, other := uuid.New(), uuid.New()
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, time.UTC) }
	hour := func(day, h int) time.Time { return at(day, h, 0) }
	day := func(d int) time.Time { return at(d, 0, 0) }

	tk := itInsert(t, db, &itTicket{Tenant: tenant, Created: at(3, 10, 15),
		FirstResponse: itTime(at(3, 10, 45)), ResponseDue: itTime(at(3, 11, 0)),
		Resolved: itTime(at(3, 14, 20)), ResolutionDue: itTime(at(3, 12, 0)), ResBreach: true,
		Updated: at(3, 14, 20)})
	// Another tenant's ticket in the same hours stays out of this tenant's cells.
	itInsert(t, db, &itTicket{Tenant: other, Created: at(3, 10, 5), Resolved: itTime(at(3, 14, 5))})

	r := itRollup(db, at(5, 0, 0))
	itRun(t, r, 2)
	itCounts(t, db, tenant, SourceHour, hour(3, 10), map[string]int64{"created_count": 1, "first_response_count": 1, "fr_met": 1})
	itCounts(t, db, tenant, SourceHour, hour(3, 14), map[string]int64{"resolved_count": 1})
	itCounts(t, db, tenant, SourceHour, hour(3, 12), map[string]int64{"res_breached": 1})
	itCounts(t, db, tenant, SourceDay, day(3), map[string]int64{"created_count": 1, "resolved_count": 1, "res_breached": 1, "fr_met": 1})

	// Reopened and resolved again the next day, with the resolution deadline moved.
	db.Exec(t, `UPDATE tickets SET resolved_at = $2, sla_resolution_due_at = $3, updated_at = $4 WHERE id = $1`,
		tk.ID, at(4, 9, 5), at(4, 8, 0), at(4, 9, 5))
	itRun(t, r, 1)
	itCounts(t, db, tenant, SourceHour, hour(3, 14), map[string]int64{"resolved_count": 0})
	itCounts(t, db, tenant, SourceHour, hour(3, 12), map[string]int64{"res_breached": 0})
	itCounts(t, db, tenant, SourceHour, hour(3, 10), map[string]int64{"created_count": 1, "first_response_count": 1})
	itCounts(t, db, tenant, SourceDay, day(3), map[string]int64{"created_count": 1, "resolved_count": 0, "res_breached": 0})
	itCounts(t, db, tenant, SourceHour, hour(4, 9), map[string]int64{"resolved_count": 1})
	itCounts(t, db, tenant, SourceHour, hour(4, 8), map[string]int64{"res_breached": 1})
	itCounts(t, db, tenant, SourceDay, day(4), map[string]int64{"created_count": 0, "resolved_count": 1, "res_breached": 1})
	itCounts(t, db, other, SourceDay, day(3), map[string]int64{"created_count": 1, "resolved_count": 1})

	// The mark now lists the new hours only.
	var raw []byte
	if err := db.SQL.QueryRow(`SELECT hours FROM ticket_rollup_marks WHERE id = $1`, tk.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var marked []time.Time
	if err := json.Unmarshal(raw, &marked); err != nil {
		t.Fatal(err)
	}
	has := map[time.Time]bool{}
	for _, h := range marked {
		has[h.UTC()] = true
	}
	if has[hour(3, 14)] || has[hour(3, 12)] || !has[hour(4, 9)] || !has[hour(4, 8)] || !has[hour(3, 10)] {
		t.Errorf("mark hours = %v", marked)
	}

	// Deleted: the ticket leaves every bucket.
	db.Exec(t, `UPDATE tickets SET deleted_at = $2, updated_at = $2 WHERE id = $1`, tk.ID, at(4, 10, 0))
	itRun(t, r, 1)
	for _, table := range []string{"ticket_rollups", "ticket_rollup_dims"} {
		n := db.Int64(t, `SELECT COALESCE(sum(created_count + resolved_count + first_response_count + fr_met + fr_breached
  + res_met + res_breached), 0) FROM `+table+` WHERE tenant_id = $1`, tenant)
		if n != 0 {
			t.Errorf("%s still counts %d for a deleted ticket", table, n)
		}
	}
	itCounts(t, db, other, SourceDay, day(3), map[string]int64{"created_count": 1, "resolved_count": 1})
}

func itTime(t time.Time) *time.Time { return &t }

func itWatermark(t *testing.T, db *testsupport.DB) (time.Time, uuid.UUID) {
	t.Helper()
	var at time.Time
	var id uuid.UUID
	if err := db.SQL.QueryRow(`SELECT watermark_at, watermark_id FROM rollup_watermarks WHERE name = $1`,
		RollupName).Scan(&at, &id); err != nil {
		t.Fatal(err)
	}
	return at, id
}

// A transaction held open longer than Settle commits rows whose updated_at the watermark has
// already passed. The forward steps never read them; the overlap re-read behind the watermark
// does, rebuilds their hours once, caps how many it rebuilds per run, and leaves the watermark
// where it is. A change older than the overlap window stays missed.
func TestIntegrationRollupOverlapCatchesLateCommits(t *testing.T) {
	db := itFresh(t)
	tenant := uuid.New()
	base := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	hour := base

	c := itInsert(t, db, &itTicket{Tenant: tenant, Created: base.Add(30 * time.Second)})
	a := itInsert(t, db, &itTicket{Tenant: tenant, Created: base.Add(50 * time.Minute)})
	r := itRollup(db, base.Add(2*time.Hour))
	itRun(t, r, 2)
	wmAt, wmID := itWatermark(t, db)
	if !wmAt.Equal(a.Updated) || wmID != a.ID {
		t.Fatalf("watermark = (%s, %s), want A (%s, %s)", wmAt, wmID, a.Updated, a.ID)
	}
	itCounts(t, db, tenant, SourceHour, hour, map[string]int64{"created_count": 2, "resolved_count": 0})

	// The slow transaction commits now: a new ticket B and C's resolution, both with an
	// updated_at the watermark has passed.
	b := itInsert(t, db, &itTicket{Tenant: tenant, Created: base.Add(40 * time.Minute)})
	db.Exec(t, `UPDATE tickets SET status = 'resolved', resolved_at = $2, updated_at = $2 WHERE id = $1`,
		c.ID, base.Add(45*time.Minute))

	// Without the overlap the run reads nothing and the rollups miss both.
	r.Overlap = 0
	itRun(t, r, 0)
	itCounts(t, db, tenant, SourceHour, hour, map[string]int64{"created_count": 2, "resolved_count": 0})

	// With it, the next run rebuilds them; the run after that has nothing left to do.
	r.Overlap = 15 * time.Minute
	itRun(t, r, 2)
	itCounts(t, db, tenant, SourceHour, hour, map[string]int64{"created_count": 3, "resolved_count": 1})
	itCounts(t, db, tenant, SourceDay, utcDay(base), map[string]int64{"created_count": 3, "resolved_count": 1})
	itRun(t, r, 0)
	if at, id := itWatermark(t, db); !at.Equal(wmAt) || id != wmID {
		t.Errorf("the overlap moved the watermark to (%s, %s)", at, id)
	}
	var marked time.Time
	if err := db.SQL.QueryRow(`SELECT updated_at FROM ticket_rollup_marks WHERE id = $1`, b.ID).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if !marked.Equal(b.Updated) {
		t.Errorf("B's mark holds %s, want its updated_at %s", marked, b.Updated)
	}

	// The cap: three late tickets, at most two rebuilt per run, the rest on the next run.
	r.OverlapLimit = 2
	for i := 0; i < 3; i++ {
		itInsert(t, db, &itTicket{Tenant: tenant, Created: base.Add(time.Duration(41+i) * time.Minute)})
	}
	// A rebuild recomputes the whole hour from the tickets table, and all three share this hour,
	// so the first capped run already counts the third. Its mark is still stale, so the next run
	// rebuilds it once more (1) without changing the count, and the run after has nothing to do.
	itRun(t, r, 2)
	itCounts(t, db, tenant, SourceHour, hour, map[string]int64{"created_count": 6})
	itRun(t, r, 1)
	itCounts(t, db, tenant, SourceHour, hour, map[string]int64{"created_count": 6})
	itRun(t, r, 0)

	// The scan cap bounds what one run examines: with room for only the two oldest tickets of
	// the window (B and the first capped one, both counted), a later late ticket is not examined
	// on that run. With the default scan it is.
	r.OverlapScan, r.OverlapLimit = 2, 2
	itInsert(t, db, &itTicket{Tenant: tenant, Created: base.Add(48 * time.Minute)})
	itRun(t, r, 0)
	itCounts(t, db, tenant, SourceHour, hour, map[string]int64{"created_count": 6})
	r.OverlapScan = DefaultOverlapScan
	itRun(t, r, 1)
	itCounts(t, db, tenant, SourceHour, hour, map[string]int64{"created_count": 7})

	// Older than the window: missed, as documented.
	itInsert(t, db, &itTicket{Tenant: tenant, Created: base.Add(20 * time.Minute)})
	itRun(t, r, 0)
	itCounts(t, db, tenant, SourceHour, hour, map[string]int64{"created_count": 7})
}

// The figures the analytics routes read from the rollups equal the same figures counted on the
// tickets table, over hour and day windows, filtered and broken down.
func TestIntegrationRollupSeriesMatchTickets(t *testing.T) {
	db := itFresh(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	teamA, teamB := uuid.New(), uuid.New()
	base := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	priorities := []string{"low", "normal", "high", "urgent"}
	channels := []string{"web", "email", "whatsapp"}
	for i := 0; i < 40; i++ {
		tk := &itTicket{Tenant: tenant, Priority: priorities[i%4], Channel: channels[i%3],
			Created: base.Add(time.Duration(i*97) * time.Minute)}
		switch i % 3 {
		case 0:
			tk.Team = &teamA
		case 1:
			tk.Team = &teamB
		}
		tk.Updated = tk.Created
		if i%2 == 0 {
			tk.FirstResponse = itTime(tk.Created.Add(time.Duration(5+i) * time.Minute))
			tk.Updated = *tk.FirstResponse
		}
		if i%3 != 0 {
			tk.Resolved = itTime(tk.Created.Add(time.Duration(30+(i*13)%300) * time.Minute))
			tk.Updated = *tk.Resolved
		}
		if i == 7 {
			tk.Deleted = itTime(tk.Updated)
		}
		itInsert(t, db, tk)
	}
	for i := 0; i < 10; i++ {
		itInsert(t, db, &itTicket{Tenant: other, Created: base.Add(time.Duration(i*200) * time.Minute),
			Resolved: itTime(base.Add(time.Duration(i*200+60) * time.Minute))})
	}
	r := itRollup(db, base.AddDate(0, 0, 10))
	r.Batch = 7
	itRun(t, r, 50)

	repo := NewRepository(db.SQL)
	count := func(column, extra string, from, to time.Time, args ...any) int64 {
		t.Helper()
		q := `SELECT count(*) FROM tickets WHERE tenant_id = $1 AND deleted_at IS NULL AND ` + column + ` >= $2 AND ` + column + ` < $3`
		if extra != "" {
			q += " AND " + extra
		}
		return db.Int64(t, q, append([]any{tenant, from, to}, args...)...)
	}
	type window struct {
		label    string
		source   string
		from, to time.Time
	}
	windows := []window{
		{"all hours", SourceHour, base, base.AddDate(0, 0, 4)},
		{"all days", SourceDay, base, base.AddDate(0, 0, 4)},
		{"second day by hour", SourceHour, base.AddDate(0, 0, 1), base.AddDate(0, 0, 2)},
		{"second day by day", SourceDay, base.AddDate(0, 0, 1), base.AddDate(0, 0, 2)},
		{"hours across midnight", SourceHour, base.Add(30 * time.Hour), base.Add(41 * time.Hour)},
	}
	high := "high"
	filters := []struct {
		label string
		apply func(*Filter)
		where string
		args  []any
	}{
		{"no filter", func(*Filter) {}, "", nil},
		{"priority", func(f *Filter) { f.Priority = high }, "priority = $4", []any{high}},
		{"team", func(f *Filter) { f.TeamID = &teamA }, "assigned_team_id = $4", []any{teamA}},
		{"team and priority", func(f *Filter) { f.TeamID, f.Priority = &teamB, high }, "assigned_team_id = $4 AND priority = $5", []any{teamB, high}},
		{"team, priority and channel", func(f *Filter) { f.TeamID, f.Priority, f.Channel = &teamB, high, "email" },
			"assigned_team_id = $4 AND priority = $5 AND channel = $6", []any{teamB, high, "email"}},
	}
	for _, w := range windows {
		for _, fl := range filters {
			f := Filter{TenantID: tenant, Source: w.source, From: w.from, To: w.to, Loc: time.UTC, Interval: IntervalDay}
			fl.apply(&f)
			got, err := repo.Totals(ctx, f, w.from, w.to)
			if err != nil {
				t.Fatalf("%s, %s: %v", w.label, fl.label, err)
			}
			label := w.label + ", " + fl.label
			if want := count("created_at", fl.where, w.from, w.to, fl.args...); got.Created != want {
				t.Errorf("%s: created %d, tickets %d", label, got.Created, want)
			}
			if want := count("resolved_at", fl.where, w.from, w.to, fl.args...); got.Resolved != want {
				t.Errorf("%s: resolved %d, tickets %d", label, got.Resolved, want)
			}
			if want := count("first_response_at", fl.where, w.from, w.to, fl.args...); got.FirstResponses != want {
				t.Errorf("%s: first responses %d, tickets %d", label, got.FirstResponses, want)
			}
		}
	}

	// The daily trend over hour cells matches the tickets per UTC day.
	f := Filter{TenantID: tenant, Source: SourceHour, From: base, To: base.AddDate(0, 0, 4), Loc: time.UTC, Interval: IntervalDay}
	trend, err := repo.Trend(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	for d := 0; d < 4; d++ {
		from := base.AddDate(0, 0, d)
		key := from.Format("2006-01-02T15:04:05")
		if want := count("created_at", "", from, from.AddDate(0, 0, 1)); trend[key].Created != want {
			t.Errorf("trend %s: created %d, tickets %d", key, trend[key].Created, want)
		}
		if want := count("resolved_at", "", from, from.AddDate(0, 0, 1)); trend[key].Resolved != want {
			t.Errorf("trend %s: resolved %d, tickets %d", key, trend[key].Resolved, want)
		}
	}

	// The breakdown by priority over day rows matches the tickets per priority.
	f.Source = SourceDay
	by, err := repo.Breakdown(ctx, f, f.From, f.To, DimPriority, DimTeam)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range priorities {
		if want := count("created_at", "priority = $4", f.From, f.To, p); by[DimPriority][p].Created != want {
			t.Errorf("breakdown priority %s: created %d, tickets %d", p, by[DimPriority][p].Created, want)
		}
	}
	if want := count("created_at", "assigned_team_id IS NULL", f.From, f.To); by[DimTeam][noneKey].Created != want {
		t.Errorf("breakdown no team: created %d, tickets %d", by[DimTeam][noneKey].Created, want)
	}
}

// The live backlog counts read the ticket_open_backlog partial index, and their figures match the
// tickets table. The statements below are the ones LiveStats and OpenByAgent build; keep them in
// step with repository.go.
func TestIntegrationOpenBacklogUsesPartialIndex(t *testing.T) {
	db := itFresh(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	team := uuid.New()
	// A realistic shape: most tickets resolved, a few percent open.
	seed := `INSERT INTO tickets (id, tenant_id, ticket_number, customer_id, assigned_team_id, subject, description,
  status, priority, channel, sla_resolution_due_at, created_at, updated_at)
SELECT gen_random_uuid(), $1, 'TKT-' || lpad(g::text, 7, '0'), gen_random_uuid(),
  CASE WHEN g % 3 = 0 THEN $3::uuid END, 'Backlog', 'Seeded',
  CASE WHEN g % 50 = 0 THEN 'open' WHEN g % 50 = 1 THEN 'pending_customer' WHEN g % 50 = 2 THEN 'new' ELSE 'resolved' END,
  'normal', 'web', now() + ((g % 100) - 50) * interval '1 hour', now() - g * interval '1 minute', now()
FROM generate_series(1, $2::int) g`
	db.Exec(t, seed, tenant, 50000, team)
	db.Exec(t, seed, other, 10000, team)
	db.Exec(t, `VACUUM ANALYZE tickets`)

	now := time.Now()
	for _, fl := range []struct {
		label string
		f     Filter
	}{
		{"tenant", Filter{TenantID: tenant}},
		{"tenant and team", Filter{TenantID: tenant, TeamID: &team}},
	} {
		q := ticketQuery(fl.f)
		nowArg := q.arg(now)
		live := fmt.Sprintf(`SELECT t.status::text, count(*),
  count(*) FILTER (WHERE t.sla_resolution_due_at < %s AND t.sla_paused_at IS NULL),
  count(*) FILTER (WHERE t.assigned_agent_id IS NULL)
FROM tickets t WHERE %s AND t.status IN %s GROUP BY 1`, nowArg, q.sql(), openStatuses)
		if plan := db.Explain(t, live, q.args...); !strings.Contains(plan, "ticket_open_backlog") {
			t.Errorf("%s: the backlog count does not use ticket_open_backlog:\n%s", fl.label, plan)
		}
		q = ticketQuery(fl.f)
		byAgent := fmt.Sprintf(`SELECT COALESCE(t.assigned_agent_id::text, ''), count(*) FROM tickets t
WHERE %s AND t.status IN %s GROUP BY 1`, q.sql(), openStatuses)
		if plan := db.Explain(t, byAgent, q.args...); !strings.Contains(plan, "ticket_open_backlog") {
			t.Errorf("%s: the open-by-agent count does not use ticket_open_backlog:\n%s", fl.label, plan)
		}

		stats, err := NewRepository(db.SQL).LiveStats(ctx, fl.f, now, now.Add(-24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		where := "tenant_id = $1 AND deleted_at IS NULL AND status IN " + openStatuses
		args := []any{tenant}
		if fl.f.TeamID != nil {
			where += " AND assigned_team_id = $2"
			args = append(args, team)
		}
		if want := db.Int64(t, `SELECT count(*) FROM tickets WHERE `+where, args...); stats.Open != want {
			t.Errorf("%s: open %d, tickets %d", fl.label, stats.Open, want)
		}
		overdueArgs := append(append([]any{}, args...), now)
		if want := db.Int64(t, fmt.Sprintf(`SELECT count(*) FROM tickets WHERE %s AND sla_resolution_due_at < $%d AND sla_paused_at IS NULL`,
			where, len(overdueArgs)), overdueArgs...); stats.Overdue != want {
			t.Errorf("%s: overdue %d, tickets %d", fl.label, stats.Overdue, want)
		}
	}
}
