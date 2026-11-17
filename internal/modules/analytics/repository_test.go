package analytics

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRollupQueryNumbersFilters(t *testing.T) {
	team := uuid.New()
	f := Filter{TenantID: tenant, Source: SourceHour, TeamID: &team, Priority: "high"}
	from, to := time.Unix(0, 0), time.Unix(3600, 0)
	q := rollupQuery(f, from, to)
	want := "r.tenant_id = $1 AND r.granularity = $2 AND r.bucket_start >= $3 AND r.bucket_start < $4 AND r.team_id = $5 AND r.priority = $6"
	if q.sql() != want || len(q.args) != 6 || q.args[4] != team || q.args[5] != "high" {
		t.Fatalf("sql %q args %v", q.sql(), q.args)
	}
	if p := q.arg("day"); p != "$7" {
		t.Fatalf("next placeholder = %s", p)
	}
}

func TestTicketQueryUsesTicketColumns(t *testing.T) {
	agent := uuid.New()
	q := ticketQuery(Filter{TenantID: tenant, AgentID: &agent, Channel: "email"})
	s := q.sql()
	for _, part := range []string{"t.tenant_id = $1", "t.deleted_at IS NULL", "t.assigned_agent_id = $2", "t.channel = $3"} {
		if !strings.Contains(s, part) {
			t.Fatalf("%q lacks %q", s, part)
		}
	}
}

func TestWindowQueryPicksTheCheapestTable(t *testing.T) {
	team := uuid.New()
	from, to := time.Unix(0, 0), time.Unix(3600, 0)

	table, q := windowQuery(Filter{TenantID: tenant, Source: SourceDay}, from, to)
	if table != tableSeries || q.args[2] != kindAll || q.args[3] != zeroUUID || q.args[4] != "" {
		t.Fatalf("unfiltered: %s %v", table, q.args)
	}
	table, q = windowQuery(Filter{TenantID: tenant, Source: SourceHour, TeamID: &team}, from, to)
	if table != tableSeries || q.args[2] != "team" || q.args[3] != zeroUUID || q.args[4] != team.String() {
		t.Fatalf("one filter: %s %v", table, q.args)
	}
	// ticketrollupdim_key is (tenant_id, granularity, dim_kind, team_id, dim_value, bucket_start):
	// every equality comes before the bucket range.
	want := "r.tenant_id = $1 AND r.granularity = $2 AND r.dim_kind = $3 AND r.team_id = $4 AND r.dim_value = $5 AND r.bucket_start >= $6 AND r.bucket_start < $7"
	if q.sql() != want {
		t.Fatalf("series sql = %q", q.sql())
	}
	// A team and one other filter read the team pair on day rows, the fine cells on hour rows.
	table, q = windowQuery(Filter{TenantID: tenant, Source: SourceDay, TeamID: &team, Priority: "high"}, from, to)
	if table != tableSeries || q.args[2] != "team_priority" || q.args[3] != team.String() || q.args[4] != "high" {
		t.Fatalf("team pair: %s %v", table, q.args)
	}
	if table, _ = windowQuery(Filter{TenantID: tenant, Source: SourceHour, TeamID: &team, Priority: "high"}, from, to); table != tableCells {
		t.Fatalf("team pair on hours read %s", table)
	}
	agent := uuid.New()
	if table, _ = windowQuery(Filter{TenantID: tenant, Source: SourceDay, AgentID: &agent, Priority: "high"}, from, to); table != tableCells {
		t.Fatalf("two filters without a team read %s", table)
	}
	if table, _ = windowQuery(Filter{TenantID: tenant, Source: SourceDay, TeamID: &team, AgentID: &agent, Priority: "high"}, from, to); table != tableCells {
		t.Fatalf("three filters read %s", table)
	}
}

func TestBreakdownSeries(t *testing.T) {
	team := uuid.New()
	all := []Dim{DimPriority, DimChannel, DimCategory, DimTeam}

	if s, c := planBreakdown(Filter{Source: SourceHour}, all); len(s) != 4 || len(c) != 0 {
		t.Fatalf("unfiltered: series %v cells %v", s, c)
	}
	// One team filter on hours: only the team itself from the series.
	if s, c := planBreakdown(Filter{Source: SourceHour, TeamID: &team}, all); len(s) != 1 || s[0] != DimTeam || len(c) != 3 {
		t.Fatalf("team on hours: series %v cells %v", s, c)
	}
	// One team filter on days: every breakdown from the team pairs.
	if s, c := planBreakdown(Filter{Source: SourceDay, TeamID: &team}, all); len(s) != 4 || len(c) != 0 {
		t.Fatalf("team on days: series %v cells %v", s, c)
	}
	ser, byTeam, ok := breakdownSeries(Filter{Source: SourceDay, TeamID: &team}, DimChannel)
	if !ok || byTeam || ser.kind != "team_channel" || ser.team != team.String() || ser.value != nil {
		t.Fatalf("team channel breakdown = %+v %v %v", ser, byTeam, ok)
	}
	// Team and priority on days: team and priority from the pair row, the rest from fine cells.
	f := Filter{Source: SourceDay, TeamID: &team, Priority: "high"}
	if s, c := planBreakdown(f, all); len(s) != 2 || len(c) != 2 || c[0] != DimChannel || c[1] != DimCategory {
		t.Fatalf("team and priority: series %v cells %v", s, c)
	}
	ser, byTeam, _ = breakdownSeries(f, DimTeam)
	if !byTeam || ser.kind != "team_priority" || *ser.value != "high" {
		t.Fatalf("team key from pair = %+v %v", ser, byTeam)
	}
	agent := uuid.New()
	if s, c := planBreakdown(Filter{Source: SourceDay, AgentID: &agent, Channel: "email"}, all); len(s) != 0 || len(c) != 4 {
		t.Fatalf("two filters without a team: series %v cells %v", s, c)
	}
}
