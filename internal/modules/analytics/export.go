package analytics

import (
	"slices"
	"strconv"
	"time"

	"github.com/Bengo-Hub/reports"
	"github.com/google/uuid"
)

// Exports are built from the same view structs the JSON routes return, so a file always shows
// the figures the screen shows for the same filters.

// Branding is the tenant's name and colour for the document header.
type Branding struct {
	TenantName   string
	PrimaryColor string
}

func count(n int64) string { return strconv.FormatInt(n, 10) }

func num(v *float64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func pct(v *float64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64) + "%"
}

func signed(v *float64, unit string) string {
	if v == nil {
		return ""
	}
	s := strconv.FormatFloat(*v, 'f', -1, 64)
	if *v > 0 {
		s = "+" + s
	}
	return s + unit + " vs previous period"
}

// baseReport fills the header, period and filter rows.
func baseReport(title string, p Period, fl Filters, b Branding, now time.Time) *reports.Report {
	r := &reports.Report{
		Title:        title,
		TenantName:   b.TenantName,
		PrimaryColor: b.PrimaryColor,
		PeriodFrom:   p.From,
		PeriodTo:     p.To,
		GeneratedAt:  now,
		Landscape:    true,
		Meta: [][2]string{
			{"Time zone", p.TZ},
			{"Interval", p.Interval},
			{"Previous period", p.PreviousFrom.Format(time.RFC3339) + " to " + p.PreviousTo.Format(time.RFC3339)},
		},
	}
	if p.RollupsAsOf != nil {
		r.Meta = append(r.Meta, [2]string{"Figures include changes up to", p.RollupsAsOf.Format(time.RFC3339)})
	}
	for _, m := range []struct {
		label string
		id    *uuid.UUID
	}{{"Team", fl.TeamID}, {"Agent", fl.AgentID}, {"Category", fl.CategoryID}} {
		if m.id != nil {
			r.Meta = append(r.Meta, [2]string{m.label, m.id.String()})
		}
	}
	if fl.Channel != nil {
		r.Meta = append(r.Meta, [2]string{"Channel", *fl.Channel})
	}
	if fl.Priority != nil {
		r.Meta = append(r.Meta, [2]string{"Priority", *fl.Priority})
	}
	return r
}

// columns returns lead, the metric columns and tail as a new slice.
func columns(lead []reports.Column, tail ...reports.Column) []reports.Column {
	return slices.Concat(lead, metricColumns, tail)
}

// metricColumns are the columns of a metrics table, after its label column.
var metricColumns = []reports.Column{
	{Header: "Created", Align: "R"},
	{Header: "Resolved", Align: "R"},
	{Header: "Avg first response (min)", Align: "R"},
	{Header: "Avg resolution (min)", Align: "R"},
	{Header: "First response SLA", Align: "R"},
	{Header: "Resolution SLA", Align: "R"},
	{Header: "CSAT avg", Align: "R"},
	{Header: "CSAT responses", Align: "R"},
}

func metricCells(m Metrics) []reports.Cell {
	return []reports.Cell{
		reports.Text(count(m.Created)),
		reports.Text(count(m.Resolved)),
		reports.Text(num(m.AvgFirstResponseMinutes)),
		reports.Text(num(m.AvgResolutionMinutes)),
		reports.Text(pct(m.FirstResponseSLA.CompliancePct)),
		reports.Text(pct(m.ResolutionSLA.CompliancePct)),
		reports.Text(num(m.CSAT.Average)),
		reports.Text(count(m.CSAT.Responses)),
	}
}

func metricsTable(title, label string, rows []Group) reports.Section {
	s := reports.Section{
		Kind:    reports.SectionTable,
		Title:   title,
		Columns: columns([]reports.Column{{Header: label, Weight: 2}}),
	}
	for _, g := range rows {
		s.Rows = append(s.Rows, append([]reports.Cell{reports.Text(g.Name)}, metricCells(g.Metrics)...))
	}
	return s
}

func trendTable(trend []TrendPoint, loc string) reports.Section {
	s := reports.Section{
		Kind:    reports.SectionTable,
		Title:   "Trend",
		Note:    "Buckets start in " + loc + ". Resolved counts by resolution time.",
		Columns: columns([]reports.Column{{Header: "Bucket", Weight: 2}}),
	}
	for _, p := range trend {
		s.Rows = append(s.Rows, append([]reports.Cell{reports.Text(p.Bucket.Format("2006-01-02 15:04"))}, metricCells(p.Metrics)...))
	}
	return s
}

func comparison(cur, prev Metrics) reports.Section {
	s := reports.Section{
		Kind:    reports.SectionTable,
		Title:   "This period and the previous one",
		Columns: columns([]reports.Column{{Header: "Period", Weight: 2}}),
	}
	s.Rows = [][]reports.Cell{
		append([]reports.Cell{reports.Text("This period")}, metricCells(cur)...),
		append([]reports.Cell{reports.Text("Previous period")}, metricCells(prev)...),
	}
	return s
}

// TicketsReport is the export of the ticket view.
func TicketsReport(v *TicketsView, b Branding, now time.Time) *reports.Report {
	r := baseReport("Ticket analytics", v.Period, v.Filters, b, now)
	r.Cards = []reports.Card{
		{Label: "Created", Value: count(v.Current.Created), Sub: signed(v.Change.CreatedPct, "%")},
		{Label: "Resolved", Value: count(v.Current.Resolved), Sub: signed(v.Change.ResolvedPct, "%")},
		{Label: "Open now", Value: count(v.Stats.Open)},
		{Label: "Overdue now", Value: count(v.Stats.Overdue)},
	}
	r.Sections = []reports.Section{
		{Kind: reports.SectionKeyValue, Title: "Today", Pairs: []reports.KV{
			{Label: "Created today", Value: count(v.Stats.CreatedToday)},
			{Label: "Resolved today", Value: count(v.Stats.ResolvedToday)},
			{Label: "Open", Value: count(v.Stats.Open)},
			{Label: "Unassigned", Value: count(v.Stats.Unassigned)},
			{Label: "Overdue", Value: count(v.Stats.Overdue)},
		}},
		comparison(v.Current, v.Previous),
		trendTable(v.Trend, v.Period.TZ),
		metricsTable("By priority", "Priority", v.ByPriority),
		metricsTable("By channel", "Channel", v.ByChannel),
		metricsTable("By category", "Category", v.ByCategory),
		metricsTable("By team", "Team", v.ByTeam),
	}
	return r
}

// AgentsReport is the export of the agent leaderboard.
func AgentsReport(v *AgentsView, b Branding, now time.Time) *reports.Report {
	r := baseReport("Agent performance", v.Period, v.Filters, b, now)
	s := reports.Section{
		Kind:  reports.SectionTable,
		Title: "Agents",
		Columns: columns([]reports.Column{{Header: "Agent", Weight: 2}, {Header: "Open now", Align: "R"}},
			reports.Column{Header: "Previous resolved", Align: "R"}),
	}
	for _, a := range v.Agents {
		row := append([]reports.Cell{reports.Text(a.Name), reports.Text(count(a.Open))}, metricCells(a.Metrics)...)
		s.Rows = append(s.Rows, append(row, reports.Text(count(a.PreviousResolved))))
	}
	r.Sections = []reports.Section{s}
	return r
}

// SLAReport is the export of the SLA view.
func SLAReport(v *SLAView, b Branding, now time.Time) *reports.Report {
	r := baseReport("SLA compliance", v.Period, v.Filters, b, now)
	r.Cards = []reports.Card{
		{Label: "First response SLA", Value: pct(v.Current.FirstResponseSLA.CompliancePct), Sub: signed(v.Change.FirstResponseComplianceDelta, " pts")},
		{Label: "Resolution SLA", Value: pct(v.Current.ResolutionSLA.CompliancePct), Sub: signed(v.Change.ResolutionComplianceDelta, " pts")},
		{Label: "Overdue now", Value: count(v.Overdue)},
	}
	r.Sections = []reports.Section{
		comparison(v.Current, v.Previous),
		trendTable(v.Trend, v.Period.TZ),
		metricsTable("By priority", "Priority", v.ByPriority),
		metricsTable("By team", "Team", v.ByTeam),
		metricsTable("By agent", "Agent", v.ByAgent),
	}
	return r
}

// CSATReport is the export of the CSAT view.
func CSATReport(v *CSATView, b Branding, now time.Time) *reports.Report {
	r := baseReport("Customer satisfaction", v.Period, v.Filters, b, now)
	r.Cards = []reports.Card{
		{Label: "Average score", Value: num(v.Current.Average), Sub: signed(v.Change.CSATAverageDelta, "")},
		{Label: "Satisfied", Value: pct(v.Current.SatisfiedPct), Sub: signed(v.Change.CSATSatisfiedDelta, " pts")},
		{Label: "Responses", Value: count(v.Current.Responses), Sub: signed(v.Change.CSATResponsesPct, "%")},
	}
	dist := reports.Section{Kind: reports.SectionKeyValue, Title: "Scores"}
	for _, k := range []string{"5", "4", "3", "2", "1"} {
		dist.Pairs = append(dist.Pairs, reports.KV{Label: "Score " + k, Value: count(v.Current.Scores[k])})
	}
	comments := reports.Section{
		Kind:    reports.SectionTable,
		Title:   "Latest comments",
		Columns: []reports.Column{{Header: "Ticket"}, {Header: "Score", Align: "R"}, {Header: "Comment", Weight: 4}, {Header: "Submitted"}},
	}
	for _, c := range v.RecentComments {
		comments.Rows = append(comments.Rows, []reports.Cell{
			reports.Text(c.TicketNumber), reports.Text(strconv.Itoa(c.Score)), reports.Text(c.Comment),
			reports.Text(c.SubmittedAt.Format("2006-01-02 15:04")),
		})
	}
	r.Sections = []reports.Section{
		dist,
		trendTable(v.Trend, v.Period.TZ),
		metricsTable("By agent", "Agent", v.ByAgent),
		metricsTable("By team", "Team", v.ByTeam),
		metricsTable("By channel", "Channel", v.ByChannel),
		comments,
	}
	return r
}
