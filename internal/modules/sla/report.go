package sla

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MaxReportWindow bounds a report's created_at window.
const MaxReportWindow = 366 * 24 * time.Hour

// ReportFilter selects the tickets an SLA report covers: those of the tenant created in
// [From, To), optionally of one team, agent or priority.
type ReportFilter struct {
	TenantID uuid.UUID
	From     time.Time
	To       time.Time
	TeamID   *uuid.UUID
	AgentID  *uuid.UUID
	Priority *Priority
}

// TargetStats is one SLA target's outcome. A target counts once it is met (answered or resolved
// before its deadline) or breached; tickets still inside their deadline are in neither count.
// Minutes are in each ticket's policy clock, net of pauses.
type TargetStats struct {
	Met           int      `json:"met"`
	Breached      int      `json:"breached"`
	CompliancePct *float64 `json:"compliance_pct"`
	AvgMinutes    *float64 `json:"avg_minutes"`
	P90Minutes    *float64 `json:"p90_minutes"`
}

// ReportStats are the SLA figures of one group of tickets.
type ReportStats struct {
	Tickets       int         `json:"tickets"`
	FirstResponse TargetStats `json:"first_response"`
	Resolution    TargetStats `json:"resolution"`
}

// PriorityStats are the figures of one priority.
type PriorityStats struct {
	Priority Priority `json:"priority"`
	ReportStats
}

// TeamStats are the figures of one team; a null team_id groups the unassigned tickets.
type TeamStats struct {
	TeamID *uuid.UUID `json:"team_id"`
	ReportStats
}

// Report is the GET /sla/reports response.
type Report struct {
	From       time.Time       `json:"from"`
	To         time.Time       `json:"to"`
	Overall    ReportStats     `json:"overall"`
	ByPriority []PriorityStats `json:"by_priority"`
	ByTeam     []TeamStats     `json:"by_team"`
}

// reportSelect aggregates the window with one GROUP BY GROUPING SETS pass: one row per priority,
// one per team and the overall row, told apart by GROUPING(). The window predicate
// (tenant_id, created_at) is served by the ticket_tenant_id_created_at_id index.
const reportSelect = `SELECT GROUPING(t.priority) AS g_priority, GROUPING(t.assigned_team_id) AS g_team,
  t.priority, t.assigned_team_id, count(*) AS tickets,
  count(*) FILTER (WHERE t.sla_response_due_at IS NOT NULL AND t.first_response_at IS NOT NULL AND NOT t.sla_response_breached) AS fr_met,
  count(*) FILTER (WHERE t.sla_response_breached) AS fr_breached,
  avg(t.sla_first_response_minutes)::float8 AS fr_avg,
  percentile_cont(0.9) WITHIN GROUP (ORDER BY t.sla_first_response_minutes) AS fr_p90,
  count(*) FILTER (WHERE t.sla_resolution_due_at IS NOT NULL AND t.resolved_at IS NOT NULL AND NOT t.sla_resolution_breached) AS res_met,
  count(*) FILTER (WHERE t.sla_resolution_breached) AS res_breached,
  avg(t.sla_resolution_minutes)::float8 AS res_avg,
  percentile_cont(0.9) WITHIN GROUP (ORDER BY t.sla_resolution_minutes) AS res_p90
FROM tickets AS t
WHERE t.tenant_id = $1 AND t.created_at >= $2 AND t.created_at < $3 AND t.deleted_at IS NULL`

const reportGroup = `
GROUP BY GROUPING SETS ((t.priority), (t.assigned_team_id), ())`

// buildReportQuery returns the report SQL and its arguments for the filter.
func buildReportQuery(f ReportFilter) (string, []any) {
	var b strings.Builder
	b.WriteString(reportSelect)
	args := []any{f.TenantID, f.From, f.To}
	if f.TeamID != nil {
		args = append(args, *f.TeamID)
		fmt.Fprintf(&b, " AND t.assigned_team_id = $%d", len(args))
	}
	if f.AgentID != nil {
		args = append(args, *f.AgentID)
		fmt.Fprintf(&b, " AND t.assigned_agent_id = $%d", len(args))
	}
	if f.Priority != nil {
		args = append(args, string(*f.Priority))
		fmt.Fprintf(&b, " AND t.priority = $%d", len(args))
	}
	b.WriteString(reportGroup)
	return b.String(), args
}

// ReportRepository runs the SLA report against the tickets table.
type ReportRepository struct {
	db *sql.DB
}

// NewReportRepository creates the report repository on the service's connection pool.
func NewReportRepository(db *sql.DB) *ReportRepository {
	return &ReportRepository{db: db}
}

// reportRow is one GROUPING SETS row.
type reportRow struct {
	gPriority, gTeam    int
	priority            sql.NullString
	team                uuid.NullUUID
	tickets             int
	frMet, frBreached   int
	frAvg, frP90        sql.NullFloat64
	resMet, resBreached int
	resAvg, resP90      sql.NullFloat64
}

// Report runs the report query and shapes its rows.
func (r *ReportRepository) Report(ctx context.Context, f ReportFilter) (*Report, error) {
	query, args := buildReportQuery(f)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scanned []reportRow
	for rows.Next() {
		var x reportRow
		if err := rows.Scan(&x.gPriority, &x.gTeam, &x.priority, &x.team, &x.tickets,
			&x.frMet, &x.frBreached, &x.frAvg, &x.frP90,
			&x.resMet, &x.resBreached, &x.resAvg, &x.resP90); err != nil {
			return nil, err
		}
		scanned = append(scanned, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return shapeReport(f, scanned), nil
}

// shapeReport sorts GROUPING SETS rows into the overall, per-priority and per-team figures.
func shapeReport(f ReportFilter, rows []reportRow) *Report {
	rep := &Report{From: f.From, To: f.To, ByPriority: []PriorityStats{}, ByTeam: []TeamStats{}}
	for _, x := range rows {
		stats := ReportStats{
			Tickets:       x.tickets,
			FirstResponse: targetStats(x.frMet, x.frBreached, x.frAvg, x.frP90),
			Resolution:    targetStats(x.resMet, x.resBreached, x.resAvg, x.resP90),
		}
		switch {
		case x.gPriority == 1 && x.gTeam == 1:
			rep.Overall = stats
		case x.gPriority == 0:
			rep.ByPriority = append(rep.ByPriority, PriorityStats{Priority: Priority(x.priority.String), ReportStats: stats})
		default:
			var team *uuid.UUID
			if x.team.Valid {
				id := x.team.UUID
				team = &id
			}
			rep.ByTeam = append(rep.ByTeam, TeamStats{TeamID: team, ReportStats: stats})
		}
	}
	return rep
}

func targetStats(met, breached int, avg, p90 sql.NullFloat64) TargetStats {
	ts := TargetStats{Met: met, Breached: breached, AvgMinutes: rounded(avg), P90Minutes: rounded(p90)}
	if total := met + breached; total > 0 {
		pct := math.Round(float64(met)*1000/float64(total)) / 10
		ts.CompliancePct = &pct
	}
	return ts
}

func rounded(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	r := math.Round(v.Float64*10) / 10
	return &r
}

// ValidateReportWindow fills the default window (the last 30 days to now) and refuses an empty,
// inverted or over-long one.
func ValidateReportWindow(f *ReportFilter, now time.Time) error {
	if f.To.IsZero() {
		f.To = now
	}
	if f.From.IsZero() {
		f.From = f.To.AddDate(0, 0, -30)
	}
	if !f.From.Before(f.To) {
		return fmt.Errorf("%w: from must be before to", ErrInvalidReport)
	}
	if f.To.Sub(f.From) > MaxReportWindow {
		return fmt.Errorf("%w: the window is at most 366 days", ErrInvalidReport)
	}
	return nil
}
