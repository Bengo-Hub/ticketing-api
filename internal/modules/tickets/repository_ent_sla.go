package tickets

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent/ticket"
	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

// BreachWarning is one SLA target the worker should warn about.
type BreachWarning struct {
	TenantID uuid.UUID
	TicketID uuid.UUID
	Target   string
	DueAt    time.Time
}

// EscalationDue is a ticket whose next escalation rule (index Level) is due.
type EscalationDue struct {
	TenantID uuid.UUID
	TicketID uuid.UUID
	Level    int
	Rule     clock.EscalationRule
}

// MetaSLAWarnings is the ticket metadata object that records each warned target and when.
const MetaSLAWarnings = "sla_warnings"

// activeStatusesSQL are the statuses whose SLA can still breach.
const activeStatusesSQL = `('new', 'open', 'in_progress', 'pending_customer', 'pending_internal')`

// minutesSQL reads a whole-minute count from JSON text, treating anything else as 0. The CASE
// guards the cast, so a malformed legacy value never fails the scan.
func minutesSQL(expr string) string {
	return fmt.Sprintf("(CASE WHEN %[1]s ~ '^[0-9]{1,5}$' THEN (%[1]s)::int ELSE 0 END)", expr)
}

// breachWarningsSQL lists unwarned targets due in (now, now + the policy's lead]. $1 is now, $2 is
// now plus clock.MaxLeadMinutes (an index range on each deadline column), $3 the batch size.
var breachWarningsSQL = `SELECT t.tenant_id, t.id, w.target, w.due_at
FROM tickets AS t
JOIN sla_policies AS p ON p.id = t.sla_policy_id AND p.tenant_id = t.tenant_id
CROSS JOIN LATERAL (VALUES ('first_response', t.sla_response_due_at), ('resolution', t.sla_resolution_due_at)) AS w(target, due_at)
WHERE t.deleted_at IS NULL
  AND t.status IN ` + activeStatusesSQL + `
  AND t.sla_paused_at IS NULL
  AND ((t.sla_response_due_at > $1 AND t.sla_response_due_at <= $2) OR (t.sla_resolution_due_at > $1 AND t.sla_resolution_due_at <= $2))
  AND w.due_at > $1
  AND w.due_at <= $1 + make_interval(mins => ` + minutesSQL("p.metadata ->> 'notify_before_breach_min'") + `)
  AND ((w.target = 'resolution' AND NOT t.sla_resolution_breached)
    OR (w.target = 'first_response' AND t.first_response_at IS NULL AND NOT t.sla_response_breached))
  AND (t.metadata -> '` + MetaSLAWarnings + `' ->> w.target) IS NULL
ORDER BY w.due_at
LIMIT $3`

// escalationOffsetSQL moves a deadline by the rule's minutes, earlier for before_breach.
var escalationOffsetSQL = `make_interval(mins => (CASE WHEN r.rule ->> 'when' = 'before_breach' THEN -1 ELSE 1 END) * ` +
	minutesSQL("r.rule ->> 'minutes'") + `)`

// EscalationLookback is how long after a deadline a ticket can still be escalated. It bounds the
// scan's index range from below, so old resolved tickets are never read.
const EscalationLookback = 30 * 24 * time.Hour

// escalationsDueSQL lists tickets whose rule at index escalation_level has fired. $1 is now, $2
// is now plus clock.MaxLeadMinutes, $4 is now less EscalationLookback (together an index range on
// each deadline column), $3 the batch size.
var escalationsDueSQL = `SELECT t.tenant_id, t.id, t.escalation_level, r.rule
FROM tickets AS t
JOIN sla_policies AS p ON p.id = t.sla_policy_id AND p.tenant_id = t.tenant_id
CROSS JOIN LATERAL (SELECT p.metadata -> 'escalation_rules' -> t.escalation_level::int AS rule) AS r
WHERE t.deleted_at IS NULL
  AND t.status IN ` + activeStatusesSQL + `
  AND t.sla_paused_at IS NULL
  AND ((t.sla_response_due_at > $4 AND t.sla_response_due_at <= $2) OR (t.sla_resolution_due_at > $4 AND t.sla_resolution_due_at <= $2))
  AND jsonb_typeof(r.rule) = 'object'
  AND CASE r.rule ->> 'target'
    WHEN 'first_response' THEN t.first_response_at IS NULL AND t.sla_response_due_at + ` + escalationOffsetSQL + ` <= $1
    WHEN 'resolution' THEN t.sla_resolution_due_at + ` + escalationOffsetSQL + ` <= $1
    ELSE false END
ORDER BY t.escalation_level, t.sla_resolution_due_at
LIMIT $3`

// markWarnedSQL records a warned target once; a second call matches no row.
const markWarnedSQL = `UPDATE tickets
SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('` + MetaSLAWarnings + `',
    COALESCE(metadata -> '` + MetaSLAWarnings + `', '{}'::jsonb) || jsonb_build_object($3::text, $4::text)),
  updated_at = $5
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
  AND (metadata -> '` + MetaSLAWarnings + `' ->> $3::text) IS NULL`

// sqlRunner is the part of *sql.DB and *sql.Tx the raw SLA statements need.
type sqlRunner interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (r *EntRepository) runner() sqlRunner {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func (r *EntRepository) UpdateSLA(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, u SLAUpdate) error {
	if u.empty() {
		return nil
	}
	update := r.updateOpen(tenantID, ticketID)
	if u.ResponseDueAt != nil {
		update = update.SetSLAResponseDueAt(*u.ResponseDueAt)
	}
	if u.ResolutionDueAt != nil {
		update = update.SetSLAResolutionDueAt(*u.ResolutionDueAt)
	}
	if u.PausedAt != nil {
		update = update.SetSLAPausedAt(*u.PausedAt)
	}
	if u.Resume {
		update = update.ClearSLAPausedAt()
	}
	if u.PausedMinutes != nil {
		update = update.SetSLAPausedMinutes(*u.PausedMinutes)
	}
	if u.FirstResponseMinutes != nil {
		update = update.SetSLAFirstResponseMinutes(*u.FirstResponseMinutes)
	}
	if u.ResolutionMinutes != nil {
		update = update.SetSLAResolutionMinutes(*u.ResolutionMinutes)
	}
	return savedOne(update.Save(ctx))
}

func leadBound(now time.Time) time.Time {
	return now.Add(time.Duration(clock.MaxLeadMinutes) * time.Minute)
}

func (r *EntRepository) ListBreachWarnings(ctx context.Context, now time.Time, limit int) ([]BreachWarning, error) {
	rows, err := r.runner().QueryContext(ctx, breachWarningsSQL, now, leadBound(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BreachWarning
	for rows.Next() {
		var w BreachWarning
		if err := rows.Scan(&w.TenantID, &w.TicketID, &w.Target, &w.DueAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *EntRepository) MarkSLAWarned(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, target string, at time.Time) (bool, error) {
	res, err := r.runner().ExecContext(ctx, markWarnedSQL, ticketID, tenantID, target, at.UTC().Format(time.RFC3339), at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (r *EntRepository) ListEscalationsDue(ctx context.Context, now time.Time, limit int) ([]EscalationDue, error) {
	rows, err := r.runner().QueryContext(ctx, escalationsDueSQL, now, leadBound(now), limit, now.Add(-EscalationLookback))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EscalationDue
	for rows.Next() {
		var (
			e    EscalationDue
			rule []byte
		)
		if err := rows.Scan(&e.TenantID, &e.TicketID, &e.Level, &rule); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rule, &e.Rule); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *EntRepository) Escalate(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, fromLevel int, priority *TicketPriority, teamID *uuid.UUID, at time.Time) (bool, error) {
	update := r.client.Ticket.Update().
		Where(ticket.ID(ticketID), ticket.TenantID(tenantID), ticket.DeletedAtIsNil(), ticket.EscalationLevel(fromLevel)).
		SetEscalationLevel(fromLevel + 1).
		SetEscalatedAt(at)
	if priority != nil {
		update = update.SetPriority(ticket.Priority(*priority))
	}
	if teamID != nil {
		update = update.SetAssignedTeamID(*teamID).ClearAssignedAgentID()
	}
	n, err := update.Save(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
