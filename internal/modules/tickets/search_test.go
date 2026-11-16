package tickets

import "testing"

func TestPlanSearch(t *testing.T) {
	cases := []struct {
		term    string
		ok      bool
		number  string
		pattern string
		prefix  bool
	}{
		{term: "", ok: false},
		{term: "   ", ok: false},
		{term: "printer jam", ok: true, number: "PRINTER JAM", pattern: "%printer jam%"},
		{term: "  printer   jam ", ok: true, number: "PRINTER JAM", pattern: "%printer jam%"},
		{term: "TKT-000042", ok: true, number: "TKT-000042", pattern: "%TKT-000042%"},
		{term: "tkt-42", ok: true, number: "TKT-000042", pattern: "%tkt-42%"},
		{term: "42", ok: true, number: "TKT-000042", pattern: `42%`, prefix: true},
		{term: "50%", ok: true, number: "50%", pattern: `%50\%%`},
		{term: "a_", ok: true, number: "A_", pattern: `a\_%`, prefix: true},
		{term: "jo", ok: true, number: "JO", pattern: "jo%", prefix: true},
	}
	for _, c := range cases {
		plan, ok := PlanSearch(c.term)
		if ok != c.ok {
			t.Fatalf("%q: ok %v, want %v", c.term, ok, c.ok)
		}
		if !ok {
			continue
		}
		if plan.Number != c.number || plan.Pattern != c.pattern || plan.Prefix != c.prefix {
			t.Errorf("%q: got %+v, want number %q pattern %q prefix %v", c.term, plan, c.number, c.pattern, c.prefix)
		}
	}
}

func TestPlanSearchCapsLongTerms(t *testing.T) {
	long := ""
	for i := 0; i < 150; i++ {
		long += "x"
	}
	plan, ok := PlanSearch(long)
	if !ok || len(plan.Pattern) != 102 {
		t.Fatalf("long term not capped at 100 runes: %d", len(plan.Pattern))
	}
}

func TestTagNormalisation(t *testing.T) {
	got := MergeTags([]string{"Billing", "vip"}, []string{" billing ", "", "Refund  Request", "VIP"})
	want := []string{"billing", "vip", "refund request"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if NormalizeTags(nil) != nil {
		t.Error("nil tags must stay nil so an update leaves them alone")
	}
	if f := ParseTagFilter("a, B ,,a"); len(f) != 2 || f[0] != "a" || f[1] != "b" {
		t.Errorf("tag filter: %v", f)
	}
}
