package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Dim is a dimension of the rollup cube, named by its ticket_rollups column.
type Dim string

const (
	DimTeam     Dim = "team_id"
	DimAgent    Dim = "agent_id"
	DimCategory Dim = "category_id"
	DimChannel  Dim = "channel"
	DimPriority Dim = "priority"
)

// ticketColumn is the tickets column a dimension filter applies to on the live figures.
func (d Dim) ticketColumn() string {
	switch d {
	case DimTeam:
		return "assigned_team_id"
	case DimAgent:
		return "assigned_agent_id"
	default:
		return string(d)
	}
}

// noneKey is the key of the zero UUID cells (no team, no agent, no category).
const noneKey = ""

var zeroUUID = uuid.Nil.String()

// openStatuses are the statuses of the live backlog.
const openStatuses = `('new', 'open', 'in_progress', 'pending_customer', 'pending_internal')`

// aggColumns sums the counters of the selected cells, in the order scanTotals reads them.
const aggColumns = `COALESCE(sum(r.created_count), 0), COALESCE(sum(r.resolved_count), 0),
  COALESCE(sum(r.first_response_count), 0), COALESCE(sum(r.first_response_minutes), 0),
  COALESCE(sum(r.resolution_minutes), 0), COALESCE(sum(r.fr_met), 0), COALESCE(sum(r.fr_breached), 0),
  COALESCE(sum(r.res_met), 0), COALESCE(sum(r.res_breached), 0), COALESCE(sum(r.csat_1), 0),
  COALESCE(sum(r.csat_2), 0), COALESCE(sum(r.csat_3), 0), COALESCE(sum(r.csat_4), 0), COALESCE(sum(r.csat_5), 0)`

func totalsDest(t *Totals) []any {
	return []any{&t.Created, &t.Resolved, &t.FirstResponses, &t.FirstResponseMinutes, &t.ResolutionMinutes,
		&t.FRMet, &t.FRBreached, &t.ResMet, &t.ResBreached,
		&t.CSAT[0], &t.CSAT[1], &t.CSAT[2], &t.CSAT[3], &t.CSAT[4]}
}

// query is a WHERE clause under construction with its positional arguments.
type query struct {
	where []string
	args  []any
}

func (q *query) add(cond string, arg any) {
	q.args = append(q.args, arg)
	q.where = append(q.where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(q.args))))
}

func (q *query) arg(v any) string {
	q.args = append(q.args, v)
	return fmt.Sprintf("$%d", len(q.args))
}

func (q *query) sql() string { return strings.Join(q.where, " AND ") }

// dims adds the dimension filters on columns named by col.
func (q *query) dims(f Filter, alias string, col func(Dim) string) {
	if f.TeamID != nil {
		q.add(alias+col(DimTeam)+" = ?", *f.TeamID)
	}
	if f.AgentID != nil {
		q.add(alias+col(DimAgent)+" = ?", *f.AgentID)
	}
	if f.CategoryID != nil {
		q.add(alias+col(DimCategory)+" = ?", *f.CategoryID)
	}
	if f.Channel != "" {
		q.add(alias+col(DimChannel)+" = ?", f.Channel)
	}
	if f.Priority != "" {
		q.add(alias+col(DimPriority)+" = ?", f.Priority)
	}
}

// rollupQuery selects the filter's cells of [from, to). The cell index (tenant_id, granularity,
// bucket_start, ...) serves it, or the filtered dimension's own index.
func rollupQuery(f Filter, from, to time.Time) *query {
	q := &query{}
	q.add("r.tenant_id = ?", f.TenantID)
	q.add("r.granularity = ?", f.Source)
	q.add("r.bucket_start >= ?", from)
	q.add("r.bucket_start < ?", to)
	q.dims(f, "r.", func(d Dim) string { return string(d) })
	return q
}

// ticketQuery selects the tenant's live tickets matching the dimension filters.
func ticketQuery(f Filter) *query {
	q := &query{}
	q.add("t.tenant_id = ?", f.TenantID)
	q.where = append(q.where, "t.deleted_at IS NULL")
	q.dims(f, "t.", Dim.ticketColumn)
	return q
}

// Repository runs the analytics queries.
type Repository struct {
	db *sql.DB
}

// NewRepository creates the repository on the service's pool (a read replica works too).
func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// Which table a query reads. The repository picks the cheapest from the filters:
//
//   - Totals and trend read one series of ticket_rollup_dims through ticketrollupdim_key: the
//     total with no filter, the filter's value with one, or on day rows the team pair with a team
//     filter and one other filter. Anything else reads the fine cells.
//   - A breakdown by D reads the series too: every value of kind D with no filter
//     (ticketrollupdim_kind); the filter's own value when D is the filtered dimension; on day rows
//     every value of the team pair kind for D under a team filter (ticketrollupdim_pair), or the
//     pair row itself when D is the team or the other filtered dimension. A breakdown by a third
//     dimension under two filters, or by another dimension under a filter on an hour window or a
//     filter other than team, reads the fine cells in one GROUPING SETS pass, through the
//     narrowest filter index.
//
// The series reads are a few hundred to a few thousand index entries; the fine-cell reads run
// only under a filter whose index has already narrowed them.

// dimFilter is one applied dimension filter with its value as stored in dim_value.
type dimFilter struct {
	dim   Dim
	value string
}

// dimFilters lists the applied filters with the team first.
func (f Filter) dimFilters() []dimFilter {
	var out []dimFilter
	for _, x := range []struct {
		dim Dim
		id  *uuid.UUID
	}{{DimTeam, f.TeamID}, {DimAgent, f.AgentID}, {DimCategory, f.CategoryID}} {
		if x.id != nil {
			out = append(out, dimFilter{x.dim, x.id.String()})
		}
	}
	if f.Channel != "" {
		out = append(out, dimFilter{DimChannel, f.Channel})
	}
	if f.Priority != "" {
		out = append(out, dimFilter{DimPriority, f.Priority})
	}
	return out
}

// kind is the dimension's dim_kind in ticket_rollup_dims.
func (d Dim) kind() string {
	switch d {
	case DimTeam:
		return "team"
	case DimAgent:
		return "agent"
	case DimCategory:
		return "category"
	default:
		return string(d)
	}
}

// pairKind is the dim_kind of the team pair with d (d is not the team).
func (d Dim) pairKind() string { return "team_" + d.kind() }

const (
	tableCells  = "ticket_rollups"
	tableSeries = "ticket_rollup_dims"
	kindAll     = "all"
)

// series selects rows of ticket_rollup_dims: one kind, one team (the zero UUID except on pair
// rows) and either one value or, when value is nil, every value.
type series struct {
	kind  string
	team  string
	value *string
}

func strPtr(s string) *string { return &s }

// teamPair reports whether the filter is a team filter, alone or with one other filter, on a
// window read from day rows, where the team pair rows exist.
func teamPair(f Filter, dfs []dimFilter) bool {
	return f.Source == SourceDay && len(dfs) >= 1 && len(dfs) <= 2 && dfs[0].dim == DimTeam
}

// windowSeries is the series the filter's totals and trend read, or false for the fine cells.
func windowSeries(f Filter) (series, bool) {
	dfs := f.dimFilters()
	switch {
	case len(dfs) == 0:
		return series{kind: kindAll, team: zeroUUID, value: strPtr("")}, true
	case len(dfs) == 1:
		return series{kind: dfs[0].dim.kind(), team: zeroUUID, value: strPtr(dfs[0].value)}, true
	case len(dfs) == 2 && teamPair(f, dfs):
		return series{kind: dfs[1].dim.pairKind(), team: dfs[0].value, value: strPtr(dfs[1].value)}, true
	}
	return series{}, false
}

// breakdownSeries is the series a breakdown by d reads, or false for the fine cells. byTeam is
// set when the rows are pair rows whose group key is the team rather than dim_value.
func breakdownSeries(f Filter, d Dim) (s series, byTeam bool, ok bool) {
	dfs := f.dimFilters()
	switch {
	case len(dfs) == 0:
		return series{kind: d.kind(), team: zeroUUID}, false, true
	case len(dfs) == 1 && dfs[0].dim == d:
		return series{kind: d.kind(), team: zeroUUID, value: strPtr(dfs[0].value)}, false, true
	case len(dfs) == 1 && teamPair(f, dfs):
		return series{kind: d.pairKind(), team: dfs[0].value}, false, true
	case len(dfs) == 2 && teamPair(f, dfs) && d == dfs[1].dim:
		return series{kind: d.pairKind(), team: dfs[0].value, value: strPtr(dfs[1].value)}, false, true
	case len(dfs) == 2 && teamPair(f, dfs) && d == DimTeam:
		return series{kind: dfs[1].dim.pairKind(), team: dfs[0].value, value: strPtr(dfs[1].value)}, true, true
	}
	return series{}, false, false
}

// seriesQuery selects the series rows over [from, to): every equality, then the bucket range,
// in the order of ticketrollupdim_key (one value) or ticketrollupdim_kind and _pair (every value).
func seriesQuery(f Filter, from, to time.Time, s series) *query {
	q := &query{}
	q.add("r.tenant_id = ?", f.TenantID)
	q.add("r.granularity = ?", f.Source)
	q.add("r.dim_kind = ?", s.kind)
	q.add("r.team_id = ?", s.team)
	if s.value != nil {
		q.add("r.dim_value = ?", *s.value)
	}
	q.add("r.bucket_start >= ?", from)
	q.add("r.bucket_start < ?", to)
	return q
}

// windowQuery picks the cheapest table for the filter's figures over [from, to).
func windowQuery(f Filter, from, to time.Time) (string, *query) {
	if s, ok := windowSeries(f); ok {
		return tableSeries, seriesQuery(f, from, to, s)
	}
	return tableCells, rollupQuery(f, from, to)
}

// Totals sums the filter's figures over [from, to).
func (r *Repository) Totals(ctx context.Context, f Filter, from, to time.Time) (Totals, error) {
	table, q := windowQuery(f, from, to)
	var t Totals
	err := r.db.QueryRowContext(ctx, "SELECT "+aggColumns+" FROM "+table+" r WHERE "+q.sql(), q.args...).Scan(totalsDest(&t)...)
	return t, err
}

// Trend sums the filter's figures per interval bucket in the filter's time zone, keyed by the
// bucket's local wall-clock start (bucketLabel).
func (r *Repository) Trend(ctx context.Context, f Filter) (map[string]Totals, error) {
	table, q := windowQuery(f, f.From, f.To)
	interval, tz := q.arg(f.Interval), q.arg(f.Loc.String())
	stmt := fmt.Sprintf(`SELECT to_char(date_trunc(%s::text, r.bucket_start AT TIME ZONE %s::text), 'YYYY-MM-DD"T"HH24:MI:SS'), %s
FROM %s r WHERE %s GROUP BY 1`, interval, tz, aggColumns, table, q.sql())
	rows, err := r.db.QueryContext(ctx, stmt, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Totals{}
	for rows.Next() {
		var label string
		var t Totals
		if err := rows.Scan(append([]any{&label}, totalsDest(&t)...)...); err != nil {
			return nil, err
		}
		out[label] = t
	}
	return out, rows.Err()
}

// planBreakdown splits the requested dimensions between the series and the fine cells.
func planBreakdown(f Filter, dims []Dim) (seriesDims, cells []Dim) {
	for _, d := range dims {
		if _, _, ok := breakdownSeries(f, d); ok {
			seriesDims = append(seriesDims, d)
		} else {
			cells = append(cells, d)
		}
	}
	return seriesDims, cells
}

// Breakdown sums the filter's figures over [from, to) by each dimension. Keys are the dimension's
// value; the none key ("") holds the tickets without a team, agent or category.
func (r *Repository) Breakdown(ctx context.Context, f Filter, from, to time.Time, dims ...Dim) (map[Dim]map[string]Totals, error) {
	out := map[Dim]map[string]Totals{}
	for _, d := range dims {
		out[d] = map[string]Totals{}
	}
	seriesDims, cells := planBreakdown(f, dims)
	for _, d := range seriesDims {
		if err := r.seriesBreakdown(ctx, f, from, to, d, out[d]); err != nil {
			return nil, err
		}
	}
	if len(cells) > 0 {
		if err := r.cellBreakdown(ctx, f, from, to, cells, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func addKeyed(out map[string]Totals, key string, t Totals) {
	if key == zeroUUID {
		key = noneKey
	}
	cur := out[key]
	cur.Add(t)
	out[key] = cur
}

// seriesBreakdown reads one dimension's series rows, grouped by value.
func (r *Repository) seriesBreakdown(ctx context.Context, f Filter, from, to time.Time, d Dim, out map[string]Totals) error {
	s, byTeam, _ := breakdownSeries(f, d)
	q := seriesQuery(f, from, to, s)
	key := "r.dim_value"
	if byTeam {
		key = "r.team_id::text"
	}
	stmt := "SELECT " + key + ", " + aggColumns + " FROM " + tableSeries + " r WHERE " + q.sql() + " GROUP BY 1"
	rows, err := r.db.QueryContext(ctx, stmt, q.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		var t Totals
		if err := rows.Scan(append([]any{&value}, totalsDest(&t)...)...); err != nil {
			return err
		}
		addKeyed(out, value, t)
	}
	return rows.Err()
}

// cellBreakdown sums the fine cells by each dimension in one GROUPING SETS pass, under the
// filters, through the filtered dimensions' indexes.
func (r *Repository) cellBreakdown(ctx context.Context, f Filter, from, to time.Time, dims []Dim, out map[Dim]map[string]Totals) error {
	q := rollupQuery(f, from, to)
	sets := make([]string, len(dims))
	cols := make([]string, 0, 2*len(dims))
	for i, d := range dims {
		sets[i] = "(r." + string(d) + ")"
		cols = append(cols, "GROUPING(r."+string(d)+")", "COALESCE(r."+string(d)+"::text, '')")
	}
	stmt := fmt.Sprintf(`SELECT %s, %s FROM %s r WHERE %s GROUP BY GROUPING SETS (%s)`,
		strings.Join(cols, ", "), aggColumns, tableCells, q.sql(), strings.Join(sets, ", "))
	rows, err := r.db.QueryContext(ctx, stmt, q.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		grouping := make([]int, len(dims))
		keys := make([]string, len(dims))
		dest := make([]any, 0, 2*len(dims)+14)
		for i := range dims {
			dest = append(dest, &grouping[i], &keys[i])
		}
		var t Totals
		if err := rows.Scan(append(dest, totalsDest(&t)...)...); err != nil {
			return err
		}
		for i, d := range dims {
			if grouping[i] == 0 {
				addKeyed(out[d], keys[i], t)
				break
			}
		}
	}
	return rows.Err()
}

// Stats are the live figures, counted on tickets at request time.
type Stats struct {
	Open          int64            `json:"open"`
	ByStatus      map[string]int64 `json:"by_status"`
	Overdue       int64            `json:"overdue"`
	Unassigned    int64            `json:"unassigned"`
	CreatedToday  int64            `json:"created_today"`
	ResolvedToday int64            `json:"resolved_today"`
	TodayStart    time.Time        `json:"today_start"`
}

// LiveStats counts the open backlog by status (with the overdue and unassigned among it) through
// the (tenant_id, status) index, and today's created and resolved through the (tenant_id,
// created_at) and (tenant_id, resolved_at) indexes. A ticket resolved today counts today whatever
// day it was created. Overdue is open, past its resolution deadline and not paused.
func (r *Repository) LiveStats(ctx context.Context, f Filter, now, todayStart time.Time) (Stats, error) {
	s := Stats{ByStatus: map[string]int64{}, TodayStart: todayStart}
	q := ticketQuery(f)
	nowArg := q.arg(now)
	stmt := fmt.Sprintf(`SELECT t.status::text, count(*),
  count(*) FILTER (WHERE t.sla_resolution_due_at < %s AND t.sla_paused_at IS NULL),
  count(*) FILTER (WHERE t.assigned_agent_id IS NULL)
FROM tickets t WHERE %s AND t.status IN %s GROUP BY 1`, nowArg, q.sql(), openStatuses)
	rows, err := r.db.QueryContext(ctx, stmt, q.args...)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n, overdue, unassigned int64
		if err := rows.Scan(&status, &n, &overdue, &unassigned); err != nil {
			return s, err
		}
		s.ByStatus[status] = n
		s.Open += n
		s.Overdue += overdue
		s.Unassigned += unassigned
	}
	if err := rows.Err(); err != nil {
		return s, err
	}

	q = ticketQuery(f)
	start := q.arg(todayStart)
	stmt = fmt.Sprintf(`SELECT
  (SELECT count(*) FROM tickets t WHERE %[1]s AND t.created_at >= %[2]s),
  (SELECT count(*) FROM tickets t WHERE %[1]s AND t.resolved_at >= %[2]s)`, q.sql(), start)
	err = r.db.QueryRowContext(ctx, stmt, q.args...).Scan(&s.CreatedToday, &s.ResolvedToday)
	return s, err
}

// OpenByAgent counts the live open backlog per assigned agent (none key for unassigned).
func (r *Repository) OpenByAgent(ctx context.Context, f Filter) (map[string]int64, error) {
	q := ticketQuery(f)
	stmt := fmt.Sprintf(`SELECT COALESCE(t.assigned_agent_id::text, ''), count(*) FROM tickets t
WHERE %s AND t.status IN %s GROUP BY 1`, q.sql(), openStatuses)
	rows, err := r.db.QueryContext(ctx, stmt, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var key string
		var n int64
		if err := rows.Scan(&key, &n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, rows.Err()
}

// Label is a display name for a dimension key.
type Label struct {
	Name  string
	Email string
}

// Labels returns the tenant's team, agent or category names by id. These are small tables read
// by tenant; ticket rows are never read here.
func (r *Repository) Labels(ctx context.Context, tenantID uuid.UUID, d Dim) (map[string]Label, error) {
	var stmt string
	switch d {
	case DimTeam:
		stmt = `SELECT id::text, name, '' FROM teams WHERE tenant_id = $1`
	case DimAgent:
		stmt = `SELECT id::text, display_name, email FROM agents WHERE tenant_id = $1`
	case DimCategory:
		stmt = `SELECT id::text, name, '' FROM ticket_categories WHERE tenant_id = $1`
	default:
		return map[string]Label{}, nil
	}
	rows, err := r.db.QueryContext(ctx, stmt, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Label{}
	for rows.Next() {
		var id string
		var l Label
		if err := rows.Scan(&id, &l.Name, &l.Email); err != nil {
			return nil, err
		}
		out[id] = l
	}
	return out, rows.Err()
}

// Comment is one recent CSAT response with a comment.
type Comment struct {
	TicketID     uuid.UUID `json:"ticket_id"`
	TicketNumber string    `json:"ticket_number"`
	Score        int       `json:"score"`
	Comment      string    `json:"comment"`
	SubmittedAt  time.Time `json:"submitted_at"`
}

// RecentComments returns the latest CSAT responses with a comment in the window, newest first,
// through the (tenant_id, submitted_at) index; each joins its ticket by primary key for the
// number and the dimension filters.
func (r *Repository) RecentComments(ctx context.Context, f Filter, limit int) ([]Comment, error) {
	q := ticketQuery(f)
	q.add("c.tenant_id = ?", f.TenantID)
	q.add("c.submitted_at >= ?", f.From)
	q.add("c.submitted_at < ?", f.To)
	q.where = append(q.where, "c.comment IS NOT NULL", "c.comment <> ''")
	stmt := fmt.Sprintf(`SELECT c.ticket_id, t.ticket_number, c.score, c.comment, c.submitted_at
FROM csat_responses c JOIN tickets t ON t.id = c.ticket_id
WHERE %s ORDER BY c.submitted_at DESC LIMIT %s`, q.sql(), q.arg(limit))
	rows, err := r.db.QueryContext(ctx, stmt, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.TicketID, &c.TicketNumber, &c.Score, &c.Comment, &c.SubmittedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Freshness reports how far the rollup has read: every ticket change before AsOf is in the cells,
// and Complete is false until the first backfill has caught up.
type Freshness struct {
	AsOf     *time.Time
	Complete bool
}

// Freshness reads the ticket rollup watermark.
func (r *Repository) Freshness(ctx context.Context) (Freshness, error) {
	var at time.Time
	var caught sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT watermark_at, caught_up_at FROM rollup_watermarks WHERE name = $1`, RollupName).Scan(&at, &caught)
	if err == sql.ErrNoRows {
		return Freshness{}, nil
	}
	if err != nil {
		return Freshness{}, err
	}
	out := Freshness{Complete: caught.Valid}
	if !at.IsZero() && at.Unix() > 0 {
		out.AsOf = &at
	}
	return out, nil
}
