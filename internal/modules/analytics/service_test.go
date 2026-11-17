package analytics

import (
	"bytes"
	"context"
	"encoding/csv"
	"testing"
	"time"

	"github.com/Bengo-Hub/reports"
	"github.com/google/uuid"
)

// fakeStore answers from fixed rollup totals so the view building is tested without a database.
type fakeStore struct {
	cur, prev Totals
	trend     map[string]Totals
	breakdown map[Dim]map[string]Totals
	prevByDim map[Dim]map[string]Totals
	stats     Stats
	open      map[string]int64
	labels    map[Dim]map[string]Label
	from      time.Time
}

func (s *fakeStore) Totals(_ context.Context, _ Filter, from, _ time.Time) (Totals, error) {
	if from.Equal(s.from) {
		return s.cur, nil
	}
	return s.prev, nil
}
func (s *fakeStore) Trend(context.Context, Filter) (map[string]Totals, error) { return s.trend, nil }
func (s *fakeStore) Breakdown(_ context.Context, _ Filter, from, _ time.Time, dims ...Dim) (map[Dim]map[string]Totals, error) {
	src := s.breakdown
	if !from.Equal(s.from) {
		src = s.prevByDim
	}
	out := map[Dim]map[string]Totals{}
	for _, d := range dims {
		out[d] = src[d]
	}
	return out, nil
}
func (s *fakeStore) LiveStats(_ context.Context, _ Filter, _, today time.Time) (Stats, error) {
	st := s.stats
	st.TodayStart = today
	return st, nil
}
func (s *fakeStore) OpenByAgent(context.Context, Filter) (map[string]int64, error) {
	return s.open, nil
}
func (s *fakeStore) Labels(_ context.Context, _ uuid.UUID, d Dim) (map[string]Label, error) {
	return s.labels[d], nil
}
func (s *fakeStore) RecentComments(context.Context, Filter, int) ([]Comment, error) {
	return []Comment{{TicketNumber: "TKT-000007", Score: 2, Comment: "slow"}}, nil
}
func (s *fakeStore) Freshness(context.Context) (Freshness, error) {
	return Freshness{Complete: true}, nil
}

func fixture() (*Service, Filter, *fakeStore) {
	f := Filter{
		TenantID: tenant, Loc: time.UTC, Interval: IntervalDay, Source: SourceHour,
		From: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC),
	}
	team := uuid.New().String()
	agent := uuid.New().String()
	st := &fakeStore{
		from: f.From,
		cur:  Totals{Created: 12, Resolved: 9, FRMet: 8, FRBreached: 2, CSAT: [5]int64{0, 1, 0, 1, 2}},
		prev: Totals{Created: 6, Resolved: 9, FRMet: 5, FRBreached: 5},
		// Two tickets resolved on the 17th count there, whatever day they were created.
		trend: map[string]Totals{"2026-11-17T00:00:00": {Created: 1, Resolved: 2}},
		breakdown: map[Dim]map[string]Totals{
			DimPriority: {"low": {Created: 2}, "critical": {Created: 1}},
			DimTeam:     {team: {Created: 5, Resolved: 5}, noneKey: {Created: 7, Resolved: 4}},
			DimAgent:    {agent: {Resolved: 6, CSAT: [5]int64{0, 0, 0, 0, 2}}, noneKey: {Resolved: 3}},
			DimChannel:  {"email": {Created: 12}},
			DimCategory: {},
		},
		prevByDim: map[Dim]map[string]Totals{DimAgent: {agent: {Resolved: 3}}},
		stats:     Stats{Open: 4, Overdue: 1, ResolvedToday: 2, ByStatus: map[string]int64{"open": 4}},
		open:      map[string]int64{agent: 3, noneKey: 1},
		labels: map[Dim]map[string]Label{
			DimTeam:  {team: {Name: "Billing"}},
			DimAgent: {agent: {Name: "Wanjiku", Email: "wanjiku@example.com"}},
		},
	}
	svc := NewService(st)
	svc.now = func() time.Time { return time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC) }
	return svc, f, st
}

func TestTicketsView(t *testing.T) {
	svc, f, _ := fixture()
	v, err := svc.Tickets(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if v.Current.Created != 12 || v.Previous.Created != 6 || v.Change.CreatedPct == nil || *v.Change.CreatedPct != 100 {
		t.Fatalf("period figures %+v %+v %+v", v.Current, v.Previous, v.Change)
	}
	if len(v.Trend) != 3 || v.Trend[2].Resolved != 2 || v.Trend[0].Resolved != 0 {
		t.Fatalf("trend = %+v", v.Trend)
	}
	if v.Stats.ResolvedToday != 2 || !v.Stats.TodayStart.Equal(time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("stats = %+v", v.Stats)
	}
	if v.ByPriority[0].Key != "critical" || v.ByPriority[1].Key != "low" {
		t.Fatalf("priority order = %+v", v.ByPriority)
	}
	if v.ByTeam[0].Name != "Unassigned" || v.ByTeam[1].Name != "Billing" {
		t.Fatalf("teams = %+v", v.ByTeam)
	}
	if v.ByCategory == nil {
		t.Fatal("empty breakdown must be an empty list, not null")
	}
}

func TestAgentsView(t *testing.T) {
	svc, f, _ := fixture()
	v, err := svc.Agents(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Agents) != 2 {
		t.Fatalf("agents = %+v", v.Agents)
	}
	top := v.Agents[0]
	if top.Name != "Wanjiku" || top.AgentID == nil || top.Open != 3 || top.Resolved != 6 || top.PreviousResolved != 3 ||
		top.ResolvedChangePct == nil || *top.ResolvedChangePct != 100 {
		t.Fatalf("top agent = %+v", top)
	}
	if v.Agents[1].AgentID != nil || v.Agents[1].Name != "Unassigned" {
		t.Fatalf("unassigned row = %+v", v.Agents[1])
	}
}

func TestCSATViewKeepsOnlyGroupsWithResponses(t *testing.T) {
	svc, f, _ := fixture()
	v, err := svc.CSAT(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if v.Current.Responses != 4 || len(v.ByAgent) != 1 || v.ByAgent[0].Name != "Wanjiku" || len(v.RecentComments) != 1 {
		t.Fatalf("csat view = %+v", v)
	}
}

// The export is built from the view, so its figures are the screen's.
func TestExportMatchesView(t *testing.T) {
	svc, f, _ := fixture()
	v, err := svc.SLA(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	rep := SLAReport(v, Branding{TenantName: "Acme"}, time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC))
	if rep.Cards[0].Value != pct(v.Current.FirstResponseSLA.CompliancePct) || rep.Cards[0].Value != "80%" {
		t.Fatalf("card = %+v", rep.Cards[0])
	}
	body, mime, err := reports.Generate(rep, reports.FormatCSV)
	if err != nil || mime != reports.MimeCSV {
		t.Fatalf("csv: %v %s", err, mime)
	}
	cr := csv.NewReader(bytes.NewReader(body))
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	found := false
	for _, row := range rows {
		if len(row) > 5 && row[0] == "This period" && row[1] == "12" && row[5] == "80%" {
			found = true
		}
	}
	if !found {
		t.Fatalf("this period row missing from csv:\n%s", body)
	}
	if _, _, err := reports.Generate(rep, reports.FormatPDF); err != nil {
		t.Fatalf("pdf: %v", err)
	}
}
