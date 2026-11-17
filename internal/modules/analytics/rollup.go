package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RollupName is the ticket rollup's row in rollup_watermarks.
const RollupName = "ticket_rollups"

// Rollup fills ticket_rollups. Each step reads the next batch of tickets changed since the
// watermark, in (updated_at, id) order across tenants, and recomputes every hour cell any of them
// counts in now or counted in at the last step (ticket_rollup_marks), then the UTC days of those
// hours from their hour cells. After each set of fine cells it derives the compact series rows of
// the same buckets (ticket_rollup_dims), which the common query paths read. A step is one transaction that also moves the watermark, under a
// row lock on it, so two runs never interleave and a failed step changes nothing. Recomputing a
// cell replaces it whole, so running a step twice gives the same rows.
//
// The first run starts at the epoch: that is the backfill, bounded per tick by Budget. Tickets
// changed in the last Settle are left for a later run, so a transaction that commits a moment
// after its updated_at is not skipped.
//
// A transaction held open longer than Settle (a bulk update, a slow request) commits rows whose
// updated_at the watermark has already passed. So each run ends with one overlap step: it
// re-reads the tickets with updated_at in the last Overlap behind the watermark, keeps those
// whose mark does not hold their current updated_at (the rollup has not counted that version),
// and rebuilds their hours like any step. The overlap step takes the same row lock and its own
// transaction, and leaves the watermark where it is. It examines at most OverlapScan tickets,
// oldest first, and rebuilds at most OverlapLimit of them, so a busy window costs one bounded
// index read per tick; what is left over is rebuilt by the next runs.
type Rollup struct {
	db  *sql.DB
	log *zap.Logger
	// Batch is the most tickets read per step.
	Batch int
	// Budget bounds one Run; the next tick carries on from the watermark.
	Budget time.Duration
	// Settle is how far behind now the watermark stays.
	Settle time.Duration
	// HourChunk is the most hours recomputed per statement.
	HourChunk int
	// Overlap is how far behind the watermark the overlap step re-reads; zero turns it off.
	Overlap time.Duration
	// OverlapScan is the most tickets the overlap step examines per run.
	OverlapScan int
	// OverlapLimit is the most tickets the overlap step rebuilds per run.
	OverlapLimit int
	now          func() time.Time
}

// Overlap defaults. DefaultOverlap is also the default of ANALYTICS_ROLLUP_OVERLAP.
const (
	DefaultOverlap      = 15 * time.Minute
	DefaultOverlapScan  = 20000
	DefaultOverlapLimit = 2000
)

// NewRollup creates the job with its defaults: 2000 tickets per step, 40 seconds per run (the
// worker ticks every minute), a two-minute settle, 500 hours per statement and a 15-minute
// overlap that examines at most 20000 tickets and rebuilds at most 2000 per run.
func NewRollup(db *sql.DB, log *zap.Logger) *Rollup {
	return &Rollup{db: db, log: log.Named("analytics-rollup"), Batch: 2000, Budget: 40 * time.Second,
		Settle: 2 * time.Minute, HourChunk: 500, Overlap: DefaultOverlap, OverlapScan: DefaultOverlapScan,
		OverlapLimit: DefaultOverlapLimit, now: time.Now}
}

// Run steps until it has caught up to the settle horizon or used its budget, then runs the
// overlap step once, and returns how many ticket changes it read and rebuilt.
func (r *Rollup) Run(ctx context.Context) (int, error) {
	start := r.now()
	total := 0
	for {
		n, more, err := r.step(ctx)
		total += n
		if err != nil {
			return total, err
		}
		if !more || ctx.Err() != nil || r.now().Sub(start) >= r.Budget {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return total, err
	}
	n, err := r.overlap(ctx)
	total += n
	return total, err
}

// changedTicket is what the rollup reads of a changed ticket: its tenant and the moments it can
// count at.
type changedTicket struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	UpdatedAt       time.Time
	CreatedAt       time.Time
	ResolvedAt      *time.Time
	FirstResponseAt *time.Time
	ResponseDueAt   *time.Time
	ResolutionDueAt *time.Time
	Deleted         bool
}

// mark is a ticket's hours as stored in ticket_rollup_marks. UpdatedAt is the ticket's
// updated_at the rollup counted, stored in the mark's updated_at column, which the overlap step
// compares with the ticket's current updated_at.
type mark struct {
	ID        uuid.UUID   `json:"id"`
	TenantID  uuid.UUID   `json:"tenant_id"`
	Hours     []time.Time `json:"hours"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// overlapCandidate is a ticket in the overlap window: its updated_at now and the updated_at its
// mark holds, nil when it has no mark.
type overlapCandidate struct {
	ID        uuid.UUID
	UpdatedAt time.Time
	MarkedAt  *time.Time
}

// pickOverlap keeps the candidates the rollup has not counted at their current updated_at, in
// the order given, at most limit of them.
func pickOverlap(cands []overlapCandidate, limit int) []uuid.UUID {
	var out []uuid.UUID
	for _, c := range cands {
		if len(out) >= limit {
			break
		}
		if c.MarkedAt != nil && c.MarkedAt.Equal(c.UpdatedAt) {
			continue
		}
		out = append(out, c.ID)
	}
	return out
}

// batchPlan is what one step recomputes: the hours per tenant and the new marks.
type batchPlan struct {
	Hours map[uuid.UUID][]time.Time
	Marks []mark
}

func hourOf(t time.Time) time.Time { return t.UTC().Truncate(time.Hour) }

// ticketHours are the hours a ticket counts in: creation, resolution, first response, both SLA
// deadlines and each CSAT response. A deleted ticket counts nowhere.
func ticketHours(t changedTicket, csat []time.Time) []time.Time {
	if t.Deleted {
		return []time.Time{}
	}
	set := map[time.Time]bool{hourOf(t.CreatedAt): true}
	for _, p := range []*time.Time{t.ResolvedAt, t.FirstResponseAt, t.ResponseDueAt, t.ResolutionDueAt} {
		if p != nil {
			set[hourOf(*p)] = true
		}
	}
	for _, c := range csat {
		set[hourOf(c)] = true
	}
	return sortedHours(set)
}

func sortedHours(set map[time.Time]bool) []time.Time {
	out := make([]time.Time, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// planBatch works out the hours to recompute per tenant (each ticket's hours now and at its
// last mark) and the marks to store.
func planBatch(changed []changedTicket, csat map[uuid.UUID][]time.Time, old map[uuid.UUID][]time.Time) batchPlan {
	touched := map[uuid.UUID]map[time.Time]bool{}
	plan := batchPlan{Hours: map[uuid.UUID][]time.Time{}}
	for _, t := range changed {
		hours := ticketHours(t, csat[t.ID])
		set := touched[t.TenantID]
		if set == nil {
			set = map[time.Time]bool{}
			touched[t.TenantID] = set
		}
		for _, h := range hours {
			set[h] = true
		}
		for _, h := range old[t.ID] {
			set[hourOf(h)] = true
		}
		plan.Marks = append(plan.Marks, mark{ID: t.ID, TenantID: t.TenantID, Hours: hours, UpdatedAt: t.UpdatedAt})
	}
	for tenant, set := range touched {
		if len(set) > 0 {
			plan.Hours[tenant] = sortedHours(set)
		}
	}
	return plan
}

// daysOf returns the UTC days the hours fall in.
func daysOf(hours []time.Time) []time.Time {
	set := map[time.Time]bool{}
	for _, h := range hours {
		set[utcDay(h)] = true
	}
	return sortedHours(set)
}

// chunks splits hours into slices of at most n.
func chunks(hours []time.Time, n int) [][]time.Time {
	if n < 1 {
		n = len(hours)
	}
	var out [][]time.Time
	for len(hours) > n {
		out = append(out, hours[:n])
		hours = hours[n:]
	}
	if len(hours) > 0 {
		out = append(out, hours)
	}
	return out
}

func jsonParam(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// step processes one batch in one transaction. more reports a full batch, so more may wait.
func (r *Rollup) step(ctx context.Context) (n int, more bool, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, fmt.Errorf("rollup: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := r.now().UTC()
	if _, err = tx.ExecContext(ctx, ensureWatermarkSQL, uuid.New(), RollupName, time.Unix(0, 0).UTC(), uuid.Nil, now); err != nil {
		return 0, false, fmt.Errorf("rollup: ensure watermark: %w", err)
	}
	var wmAt time.Time
	var wmID uuid.UUID
	if err = tx.QueryRowContext(ctx, lockWatermarkSQL, RollupName).Scan(&wmAt, &wmID); err != nil {
		return 0, false, fmt.Errorf("rollup: lock watermark: %w", err)
	}
	changed, err := loadChanged(ctx, tx, wmAt, wmID, now.Add(-r.Settle), r.Batch)
	if err != nil {
		return 0, false, err
	}
	if len(changed) == 0 {
		if _, err = tx.ExecContext(ctx, advanceWatermarkSQL, RollupName, wmAt, wmID, true, now); err != nil {
			return 0, false, fmt.Errorf("rollup: mark caught up: %w", err)
		}
		return 0, false, tx.Commit()
	}
	if err = r.apply(ctx, tx, changed); err != nil {
		return 0, false, err
	}
	last := changed[len(changed)-1]
	full := len(changed) == r.Batch
	if _, err = tx.ExecContext(ctx, advanceWatermarkSQL, RollupName, last.UpdatedAt, last.ID, !full, now); err != nil {
		return 0, false, fmt.Errorf("rollup: advance watermark: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("rollup: commit: %w", err)
	}
	return len(changed), full, nil
}

// apply recomputes the hours of the changed tickets, now and at their last marks, and stores
// their new marks.
func (r *Rollup) apply(ctx context.Context, tx *sql.Tx, changed []changedTicket) error {
	ids := make([]string, len(changed))
	for i, t := range changed {
		ids[i] = t.ID.String()
	}
	idsJSON, err := jsonParam(ids)
	if err != nil {
		return err
	}
	csat, err := loadHourLists(ctx, tx, csatHoursSQL, idsJSON, false)
	if err != nil {
		return fmt.Errorf("rollup: csat hours: %w", err)
	}
	old, err := loadHourLists(ctx, tx, marksSQL, idsJSON, true)
	if err != nil {
		return fmt.Errorf("rollup: marks: %w", err)
	}
	plan := planBatch(changed, csat, old)
	for tenant, hours := range plan.Hours {
		if err := r.recompute(ctx, tx, tenant, hours); err != nil {
			return err
		}
	}
	marksJSON, err := jsonParam(plan.Marks)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, saveMarksSQL, marksJSON); err != nil {
		return fmt.Errorf("rollup: save marks: %w", err)
	}
	return nil
}

// overlap is the overlap step: in one transaction under the watermark's row lock, it re-reads
// the tickets with updated_at in the last Overlap up to the watermark, oldest first and at most
// OverlapScan of them, and rebuilds those whose mark does not hold their current updated_at, at
// most OverlapLimit. It leaves the watermark as it is and returns how many tickets it rebuilt.
func (r *Rollup) overlap(ctx context.Context) (n int, err error) {
	if r.Overlap <= 0 || r.OverlapLimit <= 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("rollup overlap: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var wmAt time.Time
	var wmID uuid.UUID
	if err = tx.QueryRowContext(ctx, lockWatermarkSQL, RollupName).Scan(&wmAt, &wmID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
			return 0, tx.Rollback()
		}
		return 0, fmt.Errorf("rollup overlap: lock watermark: %w", err)
	}
	scan := max(r.OverlapScan, r.OverlapLimit)
	cands, err := loadOverlap(ctx, tx, wmAt.Add(-r.Overlap), wmAt, wmID, scan)
	if err != nil {
		return 0, err
	}
	picked := pickOverlap(cands, r.OverlapLimit)
	if len(picked) == 0 {
		return 0, tx.Commit()
	}
	ids := make([]string, len(picked))
	for i, id := range picked {
		ids[i] = id.String()
	}
	idsJSON, err := jsonParam(ids)
	if err != nil {
		return 0, err
	}
	changed, err := loadTickets(ctx, tx, ticketsByIDSQL, idsJSON)
	if err != nil {
		return 0, fmt.Errorf("rollup overlap: tickets: %w", err)
	}
	if len(changed) == 0 {
		return 0, tx.Commit()
	}
	if err = r.apply(ctx, tx, changed); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("rollup overlap: commit: %w", err)
	}
	if r.log != nil {
		r.log.Info("analytics rollup overlap rebuilt tickets committed behind the watermark",
			zap.Int("tickets", len(changed)), zap.Time("watermark", wmAt))
	}
	return len(changed), nil
}

func loadOverlap(ctx context.Context, tx *sql.Tx, from, wmAt time.Time, wmID uuid.UUID, limit int) ([]overlapCandidate, error) {
	rows, err := tx.QueryContext(ctx, overlapCandidatesSQL, from, wmAt, wmID, limit)
	if err != nil {
		return nil, fmt.Errorf("rollup overlap: candidates: %w", err)
	}
	defer rows.Close()
	var out []overlapCandidate
	for rows.Next() {
		var c overlapCandidate
		var marked sql.NullTime
		if err := rows.Scan(&c.ID, &c.UpdatedAt, &marked); err != nil {
			return nil, err
		}
		c.MarkedAt = nullable(marked)
		out = append(out, c)
	}
	return out, rows.Err()
}

// recompute replaces a tenant's fine hour cells for the hours and their compact rows, then the
// fine and compact day rows of their UTC days. Each statement is a delete and a rebuild of whole
// buckets, so a repeat gives the same rows.
func (r *Rollup) recompute(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, hours []time.Time) error {
	if err := r.rebuild(ctx, tx, tenant, SourceHour, hours, recomputeHoursSQL, hourDimsSQL); err != nil {
		return err
	}
	return r.rebuild(ctx, tx, tenant, SourceDay, daysOf(hours), rebuildDaysSQL, dayDimsSQL)
}

// rebuild clears and rebuilds the fine cells, then the compact rows, of the buckets, in chunks.
// The compact rows are derived from the fine cells just written, so the two always agree.
func (r *Rollup) rebuild(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, granularity string, buckets []time.Time, cellsSQL, dimsSQL string) error {
	for _, chunk := range chunks(buckets, r.HourChunk) {
		p, err := jsonParam(chunk)
		if err != nil {
			return err
		}
		for _, s := range []struct {
			label string
			query string
			args  []any
		}{
			{"clear cells", deleteCellsSQL, []any{tenant, granularity, p}},
			{"rebuild cells", cellsSQL, []any{tenant, p}},
			{"clear series", deleteDimsSQL, []any{tenant, granularity, p}},
			{"rebuild series", dimsSQL, []any{tenant, p}},
		} {
			if _, err := tx.ExecContext(ctx, s.query, s.args...); err != nil {
				return fmt.Errorf("rollup: %s (%s): %w", s.label, granularity, err)
			}
		}
	}
	return nil
}

func loadChanged(ctx context.Context, tx *sql.Tx, wmAt time.Time, wmID uuid.UUID, horizon time.Time, limit int) ([]changedTicket, error) {
	out, err := loadTickets(ctx, tx, changedTicketsSQL, wmAt, wmID, horizon, limit)
	if err != nil {
		return nil, fmt.Errorf("rollup: changed tickets: %w", err)
	}
	return out, nil
}

// loadTickets reads tickets as the rollup sees them, from a query selecting the columns of
// changedTicketsSQL.
func loadTickets(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]changedTicket, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []changedTicket
	for rows.Next() {
		var t changedTicket
		var resolved, first, respDue, resDue sql.NullTime
		if err := rows.Scan(&t.ID, &t.TenantID, &t.UpdatedAt, &t.CreatedAt, &resolved, &first, &respDue, &resDue, &t.Deleted); err != nil {
			return nil, err
		}
		t.ResolvedAt, t.FirstResponseAt = nullable(resolved), nullable(first)
		t.ResponseDueAt, t.ResolutionDueAt = nullable(respDue), nullable(resDue)
		out = append(out, t)
	}
	return out, rows.Err()
}

func nullable(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

// loadHourLists reads (ticket id, time) rows, or (ticket id, JSON list of times) rows when list
// is set, grouped by ticket.
func loadHourLists(ctx context.Context, tx *sql.Tx, query, idsJSON string, list bool) (map[uuid.UUID][]time.Time, error) {
	rows, err := tx.QueryContext(ctx, query, idsJSON)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]time.Time{}
	for rows.Next() {
		var id uuid.UUID
		if !list {
			var at time.Time
			if err := rows.Scan(&id, &at); err != nil {
				return nil, err
			}
			out[id] = append(out[id], at)
			continue
		}
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var hours []time.Time
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &hours); err != nil {
				return nil, errors.Join(errors.New("rollup: bad mark"), err)
			}
		}
		out[id] = append(out[id], hours...)
	}
	return out, rows.Err()
}

const (
	ensureWatermarkSQL = `INSERT INTO rollup_watermarks (id, name, watermark_at, watermark_id, updated_at)
VALUES ($1, $2, $3, $4, $5) ON CONFLICT (name) DO NOTHING`

	lockWatermarkSQL = `SELECT watermark_at, watermark_id FROM rollup_watermarks WHERE name = $1 FOR UPDATE`

	// caught_up_at is set when a step reads less than a full batch, that is when it reached the
	// settle horizon.
	advanceWatermarkSQL = `UPDATE rollup_watermarks
SET watermark_at = $2, watermark_id = $3, caught_up_at = CASE WHEN $4::boolean THEN $5 ELSE caught_up_at END, updated_at = $5
WHERE name = $1`

	// Served by the tickets (updated_at, id) index. Deleted tickets are read too, so their cells
	// lose them.
	changedTicketsSQL = `SELECT id, tenant_id, updated_at, created_at, resolved_at, first_response_at,
  sla_response_due_at, sla_resolution_due_at, deleted_at IS NOT NULL
FROM tickets
WHERE (updated_at, id) > ($1, $2) AND updated_at < $3
ORDER BY updated_at, id
LIMIT $4`

	// overlapCandidatesSQL reads the tickets at or behind the watermark with updated_at from $1,
	// oldest first and at most $4, through the tickets (updated_at, id) index, with the
	// updated_at their mark holds (a primary key read of ticket_rollup_marks).
	overlapCandidatesSQL = `SELECT w.id, w.updated_at, m.updated_at
FROM (
  SELECT id, updated_at FROM tickets
  WHERE updated_at >= $1 AND (updated_at, id) <= ($2, $3)
  ORDER BY updated_at, id
  LIMIT $4
) w
LEFT JOIN ticket_rollup_marks m ON m.id = w.id
ORDER BY w.updated_at, w.id`

	// ticketsByIDSQL reads the overlap step's tickets as they are now.
	ticketsByIDSQL = `SELECT id, tenant_id, updated_at, created_at, resolved_at, first_response_at,
  sla_response_due_at, sla_resolution_due_at, deleted_at IS NOT NULL
FROM tickets
WHERE id IN (SELECT jsonb_array_elements_text($1::jsonb)::uuid)
ORDER BY updated_at, id`

	csatHoursSQL = `SELECT ticket_id, submitted_at FROM csat_responses
WHERE ticket_id IN (SELECT jsonb_array_elements_text($1::jsonb)::uuid)`

	marksSQL = `SELECT id, hours FROM ticket_rollup_marks
WHERE id IN (SELECT jsonb_array_elements_text($1::jsonb)::uuid)`

	// The mark's updated_at is the ticket's updated_at the rollup counted, not the time of the
	// write: the overlap step skips a ticket whose mark holds its current updated_at.
	saveMarksSQL = `INSERT INTO ticket_rollup_marks (id, tenant_id, hours, updated_at)
SELECT (m ->> 'id')::uuid, (m ->> 'tenant_id')::uuid, m -> 'hours', (m ->> 'updated_at')::timestamptz
FROM jsonb_array_elements($1::jsonb) AS m
ON CONFLICT (id) DO UPDATE SET hours = EXCLUDED.hours, updated_at = EXCLUDED.updated_at`

	deleteCellsSQL = `DELETE FROM ticket_rollups
WHERE tenant_id = $1 AND granularity = $2
  AND bucket_start IN (SELECT jsonb_array_elements_text($3::jsonb)::timestamptz)`

	cellColumns = `id, tenant_id, granularity, bucket_start, team_id, agent_id, category_id, channel, priority,
  created_count, resolved_count, first_response_count, first_response_minutes, resolution_minutes,
  fr_met, fr_breached, res_met, res_breached, csat_1, csat_2, csat_3, csat_4, csat_5, updated_at`

	onCellConflict = `ON CONFLICT (tenant_id, granularity, bucket_start, team_id, agent_id, category_id, channel, priority)
DO UPDATE SET created_count = EXCLUDED.created_count, resolved_count = EXCLUDED.resolved_count,
  first_response_count = EXCLUDED.first_response_count, first_response_minutes = EXCLUDED.first_response_minutes,
  resolution_minutes = EXCLUDED.resolution_minutes, fr_met = EXCLUDED.fr_met, fr_breached = EXCLUDED.fr_breached,
  res_met = EXCLUDED.res_met, res_breached = EXCLUDED.res_breached, csat_1 = EXCLUDED.csat_1,
  csat_2 = EXCLUDED.csat_2, csat_3 = EXCLUDED.csat_3, csat_4 = EXCLUDED.csat_4, csat_5 = EXCLUDED.csat_5,
  updated_at = EXCLUDED.updated_at`

	// evDims is a ticket's cell key; a missing team, agent or category is the zero UUID.
	evDims = `COALESCE(t.assigned_team_id, '00000000-0000-0000-0000-000000000000'::uuid) AS team_id,
  COALESCE(t.assigned_agent_id, '00000000-0000-0000-0000-000000000000'::uuid) AS agent_id,
  COALESCE(t.category_id, '00000000-0000-0000-0000-000000000000'::uuid) AS category_id,
  t.channel::text AS channel, t.priority::text AS priority`

	// recomputeHoursSQL rebuilds whole hour cells from tickets: one branch per moment a ticket
	// counts at, each a range scan of its (tenant_id, moment) index per hour, then one GROUP BY.
	recomputeHoursSQL = `WITH hrs AS (
  SELECT DISTINCT jsonb_array_elements_text($2::jsonb)::timestamptz AS h
), ev AS (
  SELECT hrs.h, ` + evDims + `,
    1 AS created, 0 AS resolved, 0 AS fr_n, 0::bigint AS fr_min, 0::bigint AS res_min,
    0 AS fr_met, 0 AS fr_br, 0 AS res_met, 0 AS res_br, 0 AS s1, 0 AS s2, 0 AS s3, 0 AS s4, 0 AS s5
  FROM hrs JOIN tickets t ON t.tenant_id = $1 AND t.created_at >= hrs.h AND t.created_at < hrs.h + interval '1 hour'
  WHERE t.deleted_at IS NULL
  UNION ALL
  SELECT hrs.h, ` + evDims + `,
    0, 1, 0, 0::bigint, GREATEST(0, floor(extract(epoch FROM t.resolved_at - t.created_at) / 60))::bigint,
    0, 0, CASE WHEN t.sla_resolution_due_at IS NOT NULL AND NOT t.sla_resolution_breached THEN 1 ELSE 0 END, 0,
    0, 0, 0, 0, 0
  FROM hrs JOIN tickets t ON t.tenant_id = $1 AND t.resolved_at >= hrs.h AND t.resolved_at < hrs.h + interval '1 hour'
  WHERE t.deleted_at IS NULL
  UNION ALL
  SELECT hrs.h, ` + evDims + `,
    0, 0, 1, GREATEST(0, floor(extract(epoch FROM t.first_response_at - t.created_at) / 60))::bigint, 0::bigint,
    CASE WHEN t.sla_response_due_at IS NOT NULL AND NOT t.sla_response_breached THEN 1 ELSE 0 END, 0, 0, 0,
    0, 0, 0, 0, 0
  FROM hrs JOIN tickets t ON t.tenant_id = $1 AND t.first_response_at >= hrs.h AND t.first_response_at < hrs.h + interval '1 hour'
  WHERE t.deleted_at IS NULL
  UNION ALL
  SELECT hrs.h, ` + evDims + `,
    0, 0, 0, 0::bigint, 0::bigint, 0, 1, 0, 0, 0, 0, 0, 0, 0
  FROM hrs JOIN tickets t ON t.tenant_id = $1 AND t.sla_response_due_at >= hrs.h AND t.sla_response_due_at < hrs.h + interval '1 hour'
  WHERE t.deleted_at IS NULL AND t.sla_response_breached
  UNION ALL
  SELECT hrs.h, ` + evDims + `,
    0, 0, 0, 0::bigint, 0::bigint, 0, 0, 0, 1, 0, 0, 0, 0, 0
  FROM hrs JOIN tickets t ON t.tenant_id = $1 AND t.sla_resolution_due_at >= hrs.h AND t.sla_resolution_due_at < hrs.h + interval '1 hour'
  WHERE t.deleted_at IS NULL AND t.sla_resolution_breached
  UNION ALL
  SELECT hrs.h, ` + evDims + `,
    0, 0, 0, 0::bigint, 0::bigint, 0, 0, 0, 0,
    (c.score = 1)::int, (c.score = 2)::int, (c.score = 3)::int, (c.score = 4)::int, (c.score = 5)::int
  FROM hrs JOIN csat_responses c ON c.tenant_id = $1 AND c.submitted_at >= hrs.h AND c.submitted_at < hrs.h + interval '1 hour'
  JOIN tickets t ON t.id = c.ticket_id AND t.tenant_id = $1
  WHERE t.deleted_at IS NULL
)
INSERT INTO ticket_rollups (` + cellColumns + `)
SELECT gen_random_uuid(), $1, 'hour', ev.h, ev.team_id, ev.agent_id, ev.category_id, ev.channel, ev.priority,
  sum(ev.created), sum(ev.resolved), sum(ev.fr_n), sum(ev.fr_min), sum(ev.res_min),
  sum(ev.fr_met), sum(ev.fr_br), sum(ev.res_met), sum(ev.res_br),
  sum(ev.s1), sum(ev.s2), sum(ev.s3), sum(ev.s4), sum(ev.s5), now()
FROM ev
GROUP BY ev.h, ev.team_id, ev.agent_id, ev.category_id, ev.channel, ev.priority
` + onCellConflict

	// rebuildDaysSQL sums a UTC day's hour cells into its day cells.
	rebuildDaysSQL = `WITH days AS (
  SELECT DISTINCT jsonb_array_elements_text($2::jsonb)::timestamptz AS d
)
INSERT INTO ticket_rollups (` + cellColumns + `)
SELECT gen_random_uuid(), $1, 'day', days.d, r.team_id, r.agent_id, r.category_id, r.channel, r.priority,
  sum(r.created_count), sum(r.resolved_count), sum(r.first_response_count), sum(r.first_response_minutes),
  sum(r.resolution_minutes), sum(r.fr_met), sum(r.fr_breached), sum(r.res_met), sum(r.res_breached),
  sum(r.csat_1), sum(r.csat_2), sum(r.csat_3), sum(r.csat_4), sum(r.csat_5), now()
FROM days JOIN ticket_rollups r ON r.tenant_id = $1 AND r.granularity = 'hour'
  AND r.bucket_start >= days.d AND r.bucket_start < days.d + interval '24 hours'
GROUP BY days.d, r.team_id, r.agent_id, r.category_id, r.channel, r.priority
` + onCellConflict
)

// The compact series (ticket_rollup_dims).
const (
	// deleteDimsSQL goes through ticketrollupdim_bucket.
	deleteDimsSQL = `DELETE FROM ticket_rollup_dims
WHERE tenant_id = $1 AND granularity = $2
  AND bucket_start IN (SELECT jsonb_array_elements_text($3::jsonb)::timestamptz)`

	dimColumns = `id, tenant_id, granularity, bucket_start, dim_kind, team_id, dim_value,
  created_count, resolved_count, first_response_count, first_response_minutes, resolution_minutes,
  fr_met, fr_breached, res_met, res_breached, csat_1, csat_2, csat_3, csat_4, csat_5, updated_at`

	sumCounters = `sum(r.created_count), sum(r.resolved_count), sum(r.first_response_count),
  sum(r.first_response_minutes), sum(r.resolution_minutes), sum(r.fr_met), sum(r.fr_breached),
  sum(r.res_met), sum(r.res_breached), sum(r.csat_1), sum(r.csat_2), sum(r.csat_3), sum(r.csat_4), sum(r.csat_5)`

	onDimConflict = `ON CONFLICT (tenant_id, granularity, dim_kind, team_id, dim_value, bucket_start)
DO UPDATE SET created_count = EXCLUDED.created_count, resolved_count = EXCLUDED.resolved_count,
  first_response_count = EXCLUDED.first_response_count, first_response_minutes = EXCLUDED.first_response_minutes,
  resolution_minutes = EXCLUDED.resolution_minutes, fr_met = EXCLUDED.fr_met, fr_breached = EXCLUDED.fr_breached,
  res_met = EXCLUDED.res_met, res_breached = EXCLUDED.res_breached, csat_1 = EXCLUDED.csat_1,
  csat_2 = EXCLUDED.csat_2, csat_3 = EXCLUDED.csat_3, csat_4 = EXCLUDED.csat_4, csat_5 = EXCLUDED.csat_5,
  updated_at = EXCLUDED.updated_at`

	zeroUUIDSQL = `'00000000-0000-0000-0000-000000000000'::uuid`
)

var (
	// hourDimsSQL derives the hour series rows (total and single dimensions) of the hours.
	hourDimsSQL = dimsFromCellsSQL(SourceHour, false)
	// dayDimsSQL derives the day series rows of the UTC days, with the team pairs.
	dayDimsSQL = dimsFromCellsSQL(SourceDay, true)
)

// dimsFromCellsSQL derives the series rows of the listed buckets ($2, a JSON list of bucket
// starts) from the fine cells of the same granularity, just rebuilt in the same transaction, in
// one GROUPING SETS pass read through ticketrollup_cell by (tenant_id, granularity, bucket_start).
// In each set, the columns it does not group by are null: the CASE names the set, and the
// COALESCE picks the value of its last grouped dimension (for a pair, the dimension beside the
// team).
func dimsFromCellsSQL(granularity string, pairs bool) string {
	sets := "(), (r.team_id), (r.agent_id), (r.category_id), (r.channel), (r.priority)"
	kind := `CASE WHEN GROUPING(r.team_id) = 0 THEN 'team'
       WHEN GROUPING(r.agent_id) = 0 THEN 'agent'
       WHEN GROUPING(r.category_id) = 0 THEN 'category'
       WHEN GROUPING(r.channel) = 0 THEN 'channel'
       WHEN GROUPING(r.priority) = 0 THEN 'priority'
       ELSE 'all' END`
	team := zeroUUIDSQL
	if pairs {
		sets += ", (r.team_id, r.agent_id), (r.team_id, r.category_id), (r.team_id, r.channel), (r.team_id, r.priority)"
		kind = `CASE WHEN GROUPING(r.team_id) = 0 AND GROUPING(r.agent_id) = 0 THEN 'team_agent'
       WHEN GROUPING(r.team_id) = 0 AND GROUPING(r.category_id) = 0 THEN 'team_category'
       WHEN GROUPING(r.team_id) = 0 AND GROUPING(r.channel) = 0 THEN 'team_channel'
       WHEN GROUPING(r.team_id) = 0 AND GROUPING(r.priority) = 0 THEN 'team_priority'
       ELSE ` + kind + ` END`
		// GROUPING over the four other columns is 15 when none of them is grouped, that is on
		// the total and the single-team rows.
		team = `CASE WHEN GROUPING(r.team_id) = 0 AND GROUPING(r.agent_id, r.category_id, r.channel, r.priority) <> 15
       THEN r.team_id ELSE ` + zeroUUIDSQL + ` END`
	}
	return `WITH b AS (
  SELECT DISTINCT jsonb_array_elements_text($2::jsonb)::timestamptz AS start
)
INSERT INTO ticket_rollup_dims (` + dimColumns + `)
SELECT gen_random_uuid(), $1, '` + granularity + `', r.bucket_start,
  ` + kind + `,
  ` + team + `,
  COALESCE(r.agent_id::text, r.category_id::text, r.channel, r.priority, r.team_id::text, ''),
  ` + sumCounters + `, now()
FROM b JOIN ticket_rollups r ON r.tenant_id = $1 AND r.granularity = '` + granularity + `' AND r.bucket_start = b.start
GROUP BY r.bucket_start, GROUPING SETS (` + sets + `)
` + onDimConflict
}
