package sla

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuildReportQuery(t *testing.T) {
	tenant := uuid.New()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	q, args := buildReportQuery(ReportFilter{TenantID: tenant, From: from, To: to})
	if len(args) != 3 || args[0] != tenant {
		t.Fatalf("args = %v", args)
	}
	for _, want := range []string{
		"t.tenant_id = $1 AND t.created_at >= $2 AND t.created_at < $3",
		"GROUP BY GROUPING SETS ((t.priority), (t.assigned_team_id), ())",
		"percentile_cont(0.9) WITHIN GROUP (ORDER BY t.sla_first_response_minutes)",
		"percentile_cont(0.9) WITHIN GROUP (ORDER BY t.sla_resolution_minutes)",
		"t.deleted_at IS NULL",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query lacks %q", want)
		}
	}
	if strings.Contains(q, "$4") {
		t.Error("an unfiltered report has three arguments")
	}

	team, agent, prio := uuid.New(), uuid.New(), PriorityHigh
	q, args = buildReportQuery(ReportFilter{TenantID: tenant, From: from, To: to, TeamID: &team, AgentID: &agent, Priority: &prio})
	if len(args) != 6 || args[3] != team || args[4] != agent || args[5] != "high" {
		t.Fatalf("filtered args = %v", args)
	}
	for _, want := range []string{"t.assigned_team_id = $4", "t.assigned_agent_id = $5", "t.priority = $6"} {
		if !strings.Contains(q, want) {
			t.Errorf("filtered query lacks %q", want)
		}
	}
	if strings.Index(q, "t.priority = $6") > strings.Index(q, "GROUP BY") {
		t.Error("filters belong in WHERE, before GROUP BY")
	}
}

func TestShapeReport(t *testing.T) {
	team := uuid.New()
	f := ReportFilter{From: time.Unix(0, 0), To: time.Unix(3600, 0)}
	rows := []reportRow{
		{gPriority: 1, gTeam: 1, tickets: 10, frMet: 9, frBreached: 1, frAvg: sql.NullFloat64{Float64: 42.04, Valid: true}, frP90: sql.NullFloat64{Float64: 80, Valid: true}},
		{gPriority: 0, gTeam: 1, priority: sql.NullString{String: "high", Valid: true}, tickets: 4, resMet: 1, resBreached: 3},
		{gPriority: 1, gTeam: 0, team: uuid.NullUUID{UUID: team, Valid: true}, tickets: 6},
		{gPriority: 1, gTeam: 0, tickets: 4},
	}
	rep := shapeReport(f, rows)
	if rep.Overall.Tickets != 10 || *rep.Overall.FirstResponse.CompliancePct != 90 || *rep.Overall.FirstResponse.AvgMinutes != 42 {
		t.Errorf("overall = %+v", rep.Overall.FirstResponse)
	}
	if rep.Overall.Resolution.CompliancePct != nil || rep.Overall.Resolution.AvgMinutes != nil {
		t.Error("a target with nothing measured has null compliance and times")
	}
	if len(rep.ByPriority) != 1 || rep.ByPriority[0].Priority != PriorityHigh || *rep.ByPriority[0].Resolution.CompliancePct != 25 {
		t.Errorf("by priority = %+v", rep.ByPriority)
	}
	if len(rep.ByTeam) != 2 || rep.ByTeam[0].TeamID == nil || *rep.ByTeam[0].TeamID != team || rep.ByTeam[1].TeamID != nil {
		t.Errorf("by team = %+v", rep.ByTeam)
	}
}

func TestParseReportFilter(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	tenant := uuid.New()

	f, err := parseReportFilter(httptest.NewRequest("GET", "/sla/reports", nil), tenant, now)
	if err != nil || !f.To.Equal(now) || !f.From.Equal(now.AddDate(0, 0, -30)) {
		t.Fatalf("default window = %s to %s, %v", f.From, f.To, err)
	}

	team := uuid.New()
	f, err = parseReportFilter(httptest.NewRequest("GET", "/sla/reports?from=2026-10-01&to=2026-11-01T00:00:00Z&team_id="+team.String()+"&priority=urgent", nil), tenant, now)
	if err != nil || f.TeamID == nil || *f.TeamID != team || f.Priority == nil || *f.Priority != PriorityUrgent || f.From.Month() != time.October {
		t.Fatalf("filter = %+v, %v", f, err)
	}

	for _, q := range []string{
		"from=yesterday",
		"team_id=abc",
		"agent_id=abc",
		"priority=meh",
		"from=2026-11-10&to=2026-11-01",
		"from=2024-01-01&to=2026-01-01",
	} {
		if _, err := parseReportFilter(httptest.NewRequest("GET", "/sla/reports?"+q, nil), tenant, now); !errors.Is(err, ErrInvalidReport) {
			t.Errorf("%s: err = %v, want ErrInvalidReport", q, err)
		}
	}
}
