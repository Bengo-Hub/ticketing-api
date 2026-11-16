package clock

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseSettings(t *testing.T) {
	s, err := ParseSettings(decode(t, `{
		"notify_before_breach_min": 30,
		"pause_on_pending_internal": true,
		"escalation_rules": [
			{"target": "first_response", "when": "after_breach", "minutes": 0, "raise_priority": true},
			{"target": "resolution", "when": "before_breach", "minutes": 60, "team_id": "7d3c1b0e-1f2a-4c55-9a8b-2f1e3d4c5b6a"}
		],
		"other": "kept elsewhere"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.NotifyBeforeBreachMin != 30 || !s.PauseOnPendingInternal || len(s.EscalationRules) != 2 {
		t.Fatalf("settings = %+v", s)
	}
	if s.EscalationRules[1].Team() == nil || s.EscalationRules[0].Team() != nil {
		t.Errorf("teams = %v, %v", s.EscalationRules[0].Team(), s.EscalationRules[1].Team())
	}
	if empty, err := ParseSettings(nil); err != nil || empty.NotifyBeforeBreachMin != 0 || len(empty.EscalationRules) != 0 {
		t.Errorf("nil metadata = %+v, %v", empty, err)
	}
}

func TestParseSettingsRejects(t *testing.T) {
	bad := []string{
		`{"notify_before_breach_min": -5}`,
		`{"notify_before_breach_min": 2.5}`,
		`{"notify_before_breach_min": "30"}`,
		`{"notify_before_breach_min": 99999}`,
		`{"pause_on_pending_internal": "yes"}`,
		`{"escalation_rules": {"target": "resolution"}}`,
		`{"escalation_rules": [{"target": "csat", "when": "after_breach", "raise_priority": true}]}`,
		`{"escalation_rules": [{"target": "resolution", "when": "soon", "raise_priority": true}]}`,
		`{"escalation_rules": [{"target": "resolution", "when": "after_breach"}]}`,
		`{"escalation_rules": [{"target": "resolution", "when": "after_breach", "team_id": "not-a-uuid"}]}`,
		`{"escalation_rules": [{"target": "resolution", "when": "after_breach", "minutes": -1, "raise_priority": true}]}`,
		`{"escalation_rules": [1, 2, 3, 4, 5, 6]}`,
	}
	for _, b := range bad {
		if _, err := ParseSettings(decode(t, b)); err == nil {
			t.Errorf("%s: accepted", b)
		}
	}
}
