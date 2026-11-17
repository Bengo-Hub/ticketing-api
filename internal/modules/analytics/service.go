package analytics

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Store is what the service reads; *Repository implements it.
type Store interface {
	Totals(ctx context.Context, f Filter, from, to time.Time) (Totals, error)
	Trend(ctx context.Context, f Filter) (map[string]Totals, error)
	Breakdown(ctx context.Context, f Filter, from, to time.Time, dims ...Dim) (map[Dim]map[string]Totals, error)
	LiveStats(ctx context.Context, f Filter, now, todayStart time.Time) (Stats, error)
	OpenByAgent(ctx context.Context, f Filter) (map[string]int64, error)
	Labels(ctx context.Context, tenantID uuid.UUID, d Dim) (map[string]Label, error)
	RecentComments(ctx context.Context, f Filter, limit int) ([]Comment, error)
	Freshness(ctx context.Context) (Freshness, error)
}

// recentCommentsLimit is how many CSAT comments the CSAT view lists.
const recentCommentsLimit = 20

// Service builds the analytics views.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService creates the analytics service.
func NewService(store Store) *Service { return &Service{store: store, now: time.Now} }

// Period describes the window a view covers after alignment.
type Period struct {
	From             time.Time  `json:"from"`
	To               time.Time  `json:"to"`
	PreviousFrom     time.Time  `json:"previous_from"`
	PreviousTo       time.Time  `json:"previous_to"`
	TZ               string     `json:"tz"`
	Interval         string     `json:"interval"`
	Source           string     `json:"source"`
	RollupsAsOf      *time.Time `json:"rollups_as_of"`
	BackfillComplete bool       `json:"backfill_complete"`
}

// Filters echoes the dimension filters applied.
type Filters struct {
	TeamID     *uuid.UUID `json:"team_id"`
	AgentID    *uuid.UUID `json:"agent_id"`
	CategoryID *uuid.UUID `json:"category_id"`
	Channel    *string    `json:"channel"`
	Priority   *string    `json:"priority"`
}

// Group is one value of a dimension with its figures. Key is the id or value ("" for no team,
// agent or category) and Name its display name.
type Group struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Metrics
}

// TrendPoint is one interval bucket; Bucket is its start in the filter's time zone (RFC 3339).
type TrendPoint struct {
	Bucket time.Time `json:"bucket"`
	Metrics
}

// TicketsView is GET /analytics/tickets.
type TicketsView struct {
	Period     Period       `json:"period"`
	Filters    Filters      `json:"filters"`
	Stats      Stats        `json:"stats"`
	Current    Metrics      `json:"current"`
	Previous   Metrics      `json:"previous"`
	Change     Change       `json:"change"`
	Trend      []TrendPoint `json:"trend"`
	ByPriority []Group      `json:"by_priority"`
	ByChannel  []Group      `json:"by_channel"`
	ByCategory []Group      `json:"by_category"`
	ByTeam     []Group      `json:"by_team"`
}

// AgentRow is one agent's figures; AgentID is null for the unassigned row.
type AgentRow struct {
	AgentID *uuid.UUID `json:"agent_id"`
	Name    string     `json:"name"`
	Email   string     `json:"email"`
	Open    int64      `json:"open"`
	Metrics
	PreviousResolved  int64    `json:"previous_resolved"`
	ResolvedChangePct *float64 `json:"resolved_change_pct"`
}

// AgentsView is GET /analytics/agents.
type AgentsView struct {
	Period  Period     `json:"period"`
	Filters Filters    `json:"filters"`
	Agents  []AgentRow `json:"agents"`
}

// SLAView is GET /analytics/sla.
type SLAView struct {
	Period     Period       `json:"period"`
	Filters    Filters      `json:"filters"`
	Overdue    int64        `json:"overdue"`
	Current    Metrics      `json:"current"`
	Previous   Metrics      `json:"previous"`
	Change     Change       `json:"change"`
	Trend      []TrendPoint `json:"trend"`
	ByPriority []Group      `json:"by_priority"`
	ByTeam     []Group      `json:"by_team"`
	ByAgent    []Group      `json:"by_agent"`
}

// CSATView is GET /analytics/csat.
type CSATView struct {
	Period         Period       `json:"period"`
	Filters        Filters      `json:"filters"`
	Current        CSAT         `json:"current"`
	Previous       CSAT         `json:"previous"`
	Change         Change       `json:"change"`
	Trend          []TrendPoint `json:"trend"`
	ByAgent        []Group      `json:"by_agent"`
	ByTeam         []Group      `json:"by_team"`
	ByChannel      []Group      `json:"by_channel"`
	RecentComments []Comment    `json:"recent_comments"`
}

func (s *Service) period(ctx context.Context, f Filter) (Period, error) {
	prev := f.Previous()
	fr, err := s.store.Freshness(ctx)
	if err != nil {
		return Period{}, err
	}
	return Period{
		From: f.From, To: f.To, PreviousFrom: prev.From, PreviousTo: prev.To,
		TZ: f.Loc.String(), Interval: f.Interval, Source: f.Source,
		RollupsAsOf: fr.AsOf, BackfillComplete: fr.Complete,
	}, nil
}

func filtersOf(f Filter) Filters {
	out := Filters{TeamID: f.TeamID, AgentID: f.AgentID, CategoryID: f.CategoryID}
	if f.Channel != "" {
		c := f.Channel
		out.Channel = &c
	}
	if f.Priority != "" {
		p := f.Priority
		out.Priority = &p
	}
	return out
}

// periodTotals returns the window's and the previous window's totals.
func (s *Service) periodTotals(ctx context.Context, f Filter) (Metrics, Metrics, error) {
	cur, err := s.store.Totals(ctx, f, f.From, f.To)
	if err != nil {
		return Metrics{}, Metrics{}, err
	}
	p := f.Previous()
	prev, err := s.store.Totals(ctx, f, p.From, p.To)
	if err != nil {
		return Metrics{}, Metrics{}, err
	}
	return cur.Metrics(), prev.Metrics(), nil
}

// trend fills every bucket of the window, empty ones included.
func (s *Service) trend(ctx context.Context, f Filter) ([]TrendPoint, error) {
	byLabel, err := s.store.Trend(ctx, f)
	if err != nil {
		return nil, err
	}
	return fillTrend(f, byLabel), nil
}

func fillTrend(f Filter, byLabel map[string]Totals) []TrendPoint {
	buckets := f.Buckets()
	out := make([]TrendPoint, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, TrendPoint{Bucket: b, Metrics: byLabel[b.Format(bucketLabel)].Metrics()})
	}
	return out
}

// groups turns a breakdown into sorted, named groups.
func (s *Service) groups(ctx context.Context, f Filter, d Dim, byKey map[string]Totals, less func(a, b Group) bool) ([]Group, error) {
	labels, err := s.store.Labels(ctx, f.TenantID, d)
	if err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(byKey))
	for key, t := range byKey {
		g := Group{Key: key, Name: key, Metrics: t.Metrics()}
		if l, ok := labels[key]; ok {
			g.Name, g.Email = l.Name, l.Email
		} else if key == noneKey {
			g.Name = noneName(d)
		}
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if less(out[i], out[j]) {
			return true
		}
		if less(out[j], out[i]) {
			return false
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func noneName(d Dim) string {
	switch d {
	case DimCategory:
		return "No category"
	default:
		return "Unassigned"
	}
}

var priorityOrder = map[string]int{"critical": 0, "urgent": 1, "high": 2, "normal": 3, "low": 4}

func byPriority(a, b Group) bool { return priorityOrder[a.Key] < priorityOrder[b.Key] }
func byVolume(a, b Group) bool   { return a.Created+a.Resolved > b.Created+b.Resolved }
func byResolved(a, b Group) bool { return a.Resolved > b.Resolved }
func byResponses(a, b Group) bool {
	return a.CSAT.Responses > b.CSAT.Responses
}
func bySLA(a, b Group) bool {
	return a.FirstResponseSLA.Met+a.FirstResponseSLA.Breached+a.ResolutionSLA.Met+a.ResolutionSLA.Breached >
		b.FirstResponseSLA.Met+b.FirstResponseSLA.Breached+b.ResolutionSLA.Met+b.ResolutionSLA.Breached
}

// todayStart is midnight of now's date in the filter's time zone.
func todayStart(now time.Time, loc *time.Location) time.Time {
	l := now.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// Tickets builds the ticket view: live stats, period figures against the previous period, the
// trend and the breakdowns by priority, channel, category and team.
func (s *Service) Tickets(ctx context.Context, f Filter) (*TicketsView, error) {
	v := &TicketsView{Filters: filtersOf(f)}
	var err error
	if v.Period, err = s.period(ctx, f); err != nil {
		return nil, err
	}
	now := s.now()
	if v.Stats, err = s.store.LiveStats(ctx, f, now, todayStart(now, f.Loc)); err != nil {
		return nil, err
	}
	if v.Current, v.Previous, err = s.periodTotals(ctx, f); err != nil {
		return nil, err
	}
	v.Change = Compare(v.Current, v.Previous)
	if v.Trend, err = s.trend(ctx, f); err != nil {
		return nil, err
	}
	bd, err := s.store.Breakdown(ctx, f, f.From, f.To, DimPriority, DimChannel, DimCategory, DimTeam)
	if err != nil {
		return nil, err
	}
	if v.ByPriority, err = s.groups(ctx, f, DimPriority, bd[DimPriority], byPriority); err != nil {
		return nil, err
	}
	if v.ByChannel, err = s.groups(ctx, f, DimChannel, bd[DimChannel], byVolume); err != nil {
		return nil, err
	}
	if v.ByCategory, err = s.groups(ctx, f, DimCategory, bd[DimCategory], byVolume); err != nil {
		return nil, err
	}
	if v.ByTeam, err = s.groups(ctx, f, DimTeam, bd[DimTeam], byVolume); err != nil {
		return nil, err
	}
	return v, nil
}

// Agents builds the agent leaderboard: each agent's period figures, live open load and resolved
// count against the previous period, most resolved first. Agents with neither figures nor open
// tickets in the window are left out.
func (s *Service) Agents(ctx context.Context, f Filter) (*AgentsView, error) {
	v := &AgentsView{Filters: filtersOf(f), Agents: []AgentRow{}}
	var err error
	if v.Period, err = s.period(ctx, f); err != nil {
		return nil, err
	}
	cur, err := s.store.Breakdown(ctx, f, f.From, f.To, DimAgent)
	if err != nil {
		return nil, err
	}
	p := f.Previous()
	prev, err := s.store.Breakdown(ctx, f, p.From, p.To, DimAgent)
	if err != nil {
		return nil, err
	}
	open, err := s.store.OpenByAgent(ctx, f)
	if err != nil {
		return nil, err
	}
	labels, err := s.store.Labels(ctx, f.TenantID, DimAgent)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for k := range cur[DimAgent] {
		keys[k] = true
	}
	for k := range open {
		keys[k] = true
	}
	for key := range keys {
		row := AgentRow{Open: open[key], Metrics: cur[DimAgent][key].Metrics()}
		row.PreviousResolved = prev[DimAgent][key].Resolved
		row.ResolvedChangePct = pctChange(float64(row.Resolved), float64(row.PreviousResolved))
		if key == noneKey {
			row.Name = noneName(DimAgent)
		} else {
			if id, err := uuid.Parse(key); err == nil {
				row.AgentID = &id
			}
			l := labels[key]
			row.Name, row.Email = l.Name, l.Email
		}
		v.Agents = append(v.Agents, row)
	}
	sort.SliceStable(v.Agents, func(i, j int) bool {
		a, b := v.Agents[i], v.Agents[j]
		if a.Resolved != b.Resolved {
			return a.Resolved > b.Resolved
		}
		if a.Open != b.Open {
			return a.Open > b.Open
		}
		return a.Name < b.Name
	})
	return v, nil
}

// SLA builds the SLA view: compliance of both targets against the previous period, the trend,
// the breakdowns by priority, team and agent, and the live overdue count.
func (s *Service) SLA(ctx context.Context, f Filter) (*SLAView, error) {
	v := &SLAView{Filters: filtersOf(f)}
	var err error
	if v.Period, err = s.period(ctx, f); err != nil {
		return nil, err
	}
	now := s.now()
	stats, err := s.store.LiveStats(ctx, f, now, todayStart(now, f.Loc))
	if err != nil {
		return nil, err
	}
	v.Overdue = stats.Overdue
	if v.Current, v.Previous, err = s.periodTotals(ctx, f); err != nil {
		return nil, err
	}
	v.Change = Compare(v.Current, v.Previous)
	if v.Trend, err = s.trend(ctx, f); err != nil {
		return nil, err
	}
	bd, err := s.store.Breakdown(ctx, f, f.From, f.To, DimPriority, DimTeam, DimAgent)
	if err != nil {
		return nil, err
	}
	if v.ByPriority, err = s.groups(ctx, f, DimPriority, bd[DimPriority], byPriority); err != nil {
		return nil, err
	}
	if v.ByTeam, err = s.groups(ctx, f, DimTeam, bd[DimTeam], bySLA); err != nil {
		return nil, err
	}
	if v.ByAgent, err = s.groups(ctx, f, DimAgent, bd[DimAgent], bySLA); err != nil {
		return nil, err
	}
	return v, nil
}

// CSAT builds the satisfaction view: score, satisfied share and responses against the previous
// period, the trend, the breakdowns by agent, team and channel, and the latest comments.
func (s *Service) CSAT(ctx context.Context, f Filter) (*CSATView, error) {
	v := &CSATView{Filters: filtersOf(f)}
	var err error
	if v.Period, err = s.period(ctx, f); err != nil {
		return nil, err
	}
	cur, prev, err := s.periodTotals(ctx, f)
	if err != nil {
		return nil, err
	}
	v.Current, v.Previous, v.Change = cur.CSAT, prev.CSAT, Compare(cur, prev)
	if v.Trend, err = s.trend(ctx, f); err != nil {
		return nil, err
	}
	bd, err := s.store.Breakdown(ctx, f, f.From, f.To, DimAgent, DimTeam, DimChannel)
	if err != nil {
		return nil, err
	}
	for _, d := range []struct {
		dim Dim
		dst *[]Group
	}{{DimAgent, &v.ByAgent}, {DimTeam, &v.ByTeam}, {DimChannel, &v.ByChannel}} {
		g, err := s.groups(ctx, f, d.dim, withResponses(bd[d.dim]), byResponses)
		if err != nil {
			return nil, err
		}
		*d.dst = g
	}
	if v.RecentComments, err = s.store.RecentComments(ctx, f, recentCommentsLimit); err != nil {
		return nil, err
	}
	return v, nil
}

// withResponses keeps the groups that have CSAT responses.
func withResponses(in map[string]Totals) map[string]Totals {
	out := map[string]Totals{}
	for k, t := range in {
		var n int64
		for _, c := range t.CSAT {
			n += c
		}
		if n > 0 {
			out[k] = t
		}
	}
	return out
}
