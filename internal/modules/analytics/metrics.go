// Package analytics serves the /analytics routes and fills the rollups they read. Every period
// figure comes from ticket_rollups (hour and UTC-day cells the worker computes); only the live
// figures (the open backlog, overdue, today's created and resolved) are counted on tickets, each
// with one indexed aggregate. No route loads ticket rows into Go.
package analytics

import "math"

// Totals are the summed rollup counters of a set of cells.
type Totals struct {
	Created              int64
	Resolved             int64
	FirstResponses       int64
	FirstResponseMinutes int64
	ResolutionMinutes    int64
	FRMet                int64
	FRBreached           int64
	ResMet               int64
	ResBreached          int64
	CSAT                 [5]int64 // responses scoring 1 to 5
}

// Add sums o into t.
func (t *Totals) Add(o Totals) {
	t.Created += o.Created
	t.Resolved += o.Resolved
	t.FirstResponses += o.FirstResponses
	t.FirstResponseMinutes += o.FirstResponseMinutes
	t.ResolutionMinutes += o.ResolutionMinutes
	t.FRMet += o.FRMet
	t.FRBreached += o.FRBreached
	t.ResMet += o.ResMet
	t.ResBreached += o.ResBreached
	for i := range t.CSAT {
		t.CSAT[i] += o.CSAT[i]
	}
}

// SLATarget is one SLA target's outcome in the period.
type SLATarget struct {
	Met           int64    `json:"met"`
	Breached      int64    `json:"breached"`
	CompliancePct *float64 `json:"compliance_pct"`
}

// CSAT is the satisfaction figures of the period. A satisfied response scores 4 or 5.
type CSAT struct {
	Responses    int64            `json:"responses"`
	Average      *float64         `json:"average"`
	SatisfiedPct *float64         `json:"satisfied_pct"`
	Scores       map[string]int64 `json:"scores"`
}

// Metrics are the figures every analytics view is made of. Averages and percentages are null
// when there is nothing to average.
type Metrics struct {
	Created                 int64     `json:"created"`
	Resolved                int64     `json:"resolved"`
	FirstResponses          int64     `json:"first_responses"`
	AvgFirstResponseMinutes *float64  `json:"avg_first_response_minutes"`
	AvgResolutionMinutes    *float64  `json:"avg_resolution_minutes"`
	FirstResponseSLA        SLATarget `json:"first_response_sla"`
	ResolutionSLA           SLATarget `json:"resolution_sla"`
	CSAT                    CSAT      `json:"csat"`
}

// Metrics derives the figures from the counters.
func (t Totals) Metrics() Metrics {
	m := Metrics{
		Created:                 t.Created,
		Resolved:                t.Resolved,
		FirstResponses:          t.FirstResponses,
		AvgFirstResponseMinutes: ratio(t.FirstResponseMinutes, t.FirstResponses, 1),
		AvgResolutionMinutes:    ratio(t.ResolutionMinutes, t.Resolved, 1),
		FirstResponseSLA:        target(t.FRMet, t.FRBreached),
		ResolutionSLA:           target(t.ResMet, t.ResBreached),
		CSAT:                    csatOf(t.CSAT),
	}
	return m
}

func target(met, breached int64) SLATarget {
	return SLATarget{Met: met, Breached: breached, CompliancePct: ratio(met*100, met+breached, 1)}
}

func csatOf(scores [5]int64) CSAT {
	c := CSAT{Scores: map[string]int64{}}
	var sum, satisfied int64
	for i, n := range scores {
		c.Scores[string(rune('1'+i))] = n
		c.Responses += n
		sum += n * int64(i+1)
		if i >= 3 {
			satisfied += n
		}
	}
	c.Average = ratio(sum, c.Responses, 2)
	c.SatisfiedPct = ratio(satisfied*100, c.Responses, 1)
	return c
}

// ratio is num/den rounded to places decimals, or nil when den is zero.
func ratio(num, den int64, places int) *float64 {
	if den == 0 {
		return nil
	}
	return round(float64(num)/float64(den), places)
}

func round(v float64, places int) *float64 {
	p := math.Pow(10, float64(places))
	r := math.Round(v*p) / p
	return &r
}

// Change compares a period with the one before it. *Pct fields are the relative change in
// percent ((current - previous) / previous * 100), null when the previous figure is zero or
// missing. *Delta fields are the plain difference: percentage points for compliance and satisfied
// share, points on the 1 to 5 scale for the CSAT average.
type Change struct {
	CreatedPct                   *float64 `json:"created_pct"`
	ResolvedPct                  *float64 `json:"resolved_pct"`
	AvgFirstResponsePct          *float64 `json:"avg_first_response_pct"`
	AvgResolutionPct             *float64 `json:"avg_resolution_pct"`
	FirstResponseComplianceDelta *float64 `json:"first_response_compliance_delta"`
	ResolutionComplianceDelta    *float64 `json:"resolution_compliance_delta"`
	CSATResponsesPct             *float64 `json:"csat_responses_pct"`
	CSATAverageDelta             *float64 `json:"csat_average_delta"`
	CSATSatisfiedDelta           *float64 `json:"csat_satisfied_delta"`
}

// Compare returns the change from prev to cur.
func Compare(cur, prev Metrics) Change {
	return Change{
		CreatedPct:                   pctChange(float64(cur.Created), float64(prev.Created)),
		ResolvedPct:                  pctChange(float64(cur.Resolved), float64(prev.Resolved)),
		AvgFirstResponsePct:          pctPtr(cur.AvgFirstResponseMinutes, prev.AvgFirstResponseMinutes),
		AvgResolutionPct:             pctPtr(cur.AvgResolutionMinutes, prev.AvgResolutionMinutes),
		FirstResponseComplianceDelta: delta(cur.FirstResponseSLA.CompliancePct, prev.FirstResponseSLA.CompliancePct, 1),
		ResolutionComplianceDelta:    delta(cur.ResolutionSLA.CompliancePct, prev.ResolutionSLA.CompliancePct, 1),
		CSATResponsesPct:             pctChange(float64(cur.CSAT.Responses), float64(prev.CSAT.Responses)),
		CSATAverageDelta:             delta(cur.CSAT.Average, prev.CSAT.Average, 2),
		CSATSatisfiedDelta:           delta(cur.CSAT.SatisfiedPct, prev.CSAT.SatisfiedPct, 1),
	}
}

func pctChange(cur, prev float64) *float64 {
	if prev == 0 {
		return nil
	}
	return round((cur-prev)/prev*100, 1)
}

func pctPtr(cur, prev *float64) *float64 {
	if cur == nil || prev == nil {
		return nil
	}
	return pctChange(*cur, *prev)
}

func delta(cur, prev *float64, places int) *float64 {
	if cur == nil || prev == nil {
		return nil
	}
	return round(*cur-*prev, places)
}
