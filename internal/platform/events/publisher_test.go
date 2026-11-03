package events

import "testing"

// SLA breaches go out as ticketing.sla.<kind> with event type "sla.<kind>", the key
// notifications-api's ticketing consumer maps (cmd/worker/ticketing_consumer.go).
func TestSLABreachEventType(t *testing.T) {
	cases := map[string]struct {
		in   map[string]any
		want string
	}{
		"prefixed resolution":  {map[string]any{"event_type": "sla.resolution_breached"}, "sla.resolution_breached"},
		"bare first response":  {map[string]any{"event_type": "first_response_breached"}, "sla.first_response_breached"},
		"missing defaults":     {map[string]any{}, "sla.first_response_breached"},
		"empty string default": {map[string]any{"event_type": ""}, "sla.first_response_breached"},
	}
	for name, c := range cases {
		if got := slaBreachEventType(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}
