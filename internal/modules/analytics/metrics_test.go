package analytics

import "testing"

func f64(v float64) *float64 { return &v }

func eq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func TestMetricsFromTotals(t *testing.T) {
	m := Totals{
		Created: 10, Resolved: 4, FirstResponses: 5, FirstResponseMinutes: 52, ResolutionMinutes: 1000,
		FRMet: 3, FRBreached: 1, ResMet: 2, ResBreached: 2, CSAT: [5]int64{1, 0, 1, 2, 4},
	}.Metrics()
	if !eq(m.AvgFirstResponseMinutes, f64(10.4)) || !eq(m.AvgResolutionMinutes, f64(250)) {
		t.Fatalf("averages %v %v", *m.AvgFirstResponseMinutes, *m.AvgResolutionMinutes)
	}
	if !eq(m.FirstResponseSLA.CompliancePct, f64(75)) || !eq(m.ResolutionSLA.CompliancePct, f64(50)) {
		t.Fatalf("compliance %+v %+v", m.FirstResponseSLA, m.ResolutionSLA)
	}
	// (1 + 3 + 8 + 20) / 8 = 4.0; satisfied (4 and 5) 6 of 8.
	if m.CSAT.Responses != 8 || !eq(m.CSAT.Average, f64(4)) || !eq(m.CSAT.SatisfiedPct, f64(75)) || m.CSAT.Scores["5"] != 4 {
		t.Fatalf("csat %+v", m.CSAT)
	}
}

func TestEmptyMetricsHaveNullAverages(t *testing.T) {
	m := Totals{}.Metrics()
	if m.AvgFirstResponseMinutes != nil || m.FirstResponseSLA.CompliancePct != nil || m.CSAT.Average != nil || len(m.CSAT.Scores) != 5 {
		t.Fatalf("empty metrics %+v", m)
	}
}

func TestCompare(t *testing.T) {
	cur := Totals{Created: 15, Resolved: 10, FRMet: 9, FRBreached: 1, CSAT: [5]int64{0, 0, 0, 1, 1}}.Metrics()
	prev := Totals{Created: 10, Resolved: 0, FRMet: 8, FRBreached: 2, CSAT: [5]int64{0, 0, 1, 1, 0}}.Metrics()
	c := Compare(cur, prev)
	if !eq(c.CreatedPct, f64(50)) || c.ResolvedPct != nil {
		t.Fatalf("counts %v %v", c.CreatedPct, c.ResolvedPct)
	}
	if !eq(c.FirstResponseComplianceDelta, f64(10)) || !eq(c.CSATAverageDelta, f64(1)) || !eq(c.CSATSatisfiedDelta, f64(50)) {
		t.Fatalf("deltas %+v", c)
	}
}

func TestTotalsAdd(t *testing.T) {
	a := Totals{Created: 1, CSAT: [5]int64{1, 0, 0, 0, 0}}
	a.Add(Totals{Created: 2, ResBreached: 1, CSAT: [5]int64{0, 0, 0, 0, 3}})
	if a.Created != 3 || a.ResBreached != 1 || a.CSAT != [5]int64{1, 0, 0, 0, 3} {
		t.Fatalf("sum %+v", a)
	}
}
