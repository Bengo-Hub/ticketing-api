package clock

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Policy metadata keys read by the SLA clock and the worker. They live in sla_policies.metadata
// because only the worker's join reads them and nothing indexes them.
const (
	MetaNotifyBeforeBreachMin  = "notify_before_breach_min"
	MetaPauseOnPendingInternal = "pause_on_pending_internal"
	MetaEscalationRules        = "escalation_rules"
)

// Targets an SLA warning or escalation rule watches.
const (
	TargetFirstResponse = "first_response"
	TargetResolution    = "resolution"
)

// When an escalation rule fires relative to its target's deadline.
const (
	WhenBeforeBreach = "before_breach"
	WhenAfterBreach  = "after_breach"
)

// Limits on policy settings. MaxLeadMinutes also bounds the worker's index range scans.
const (
	MaxLeadMinutes     = 7 * 24 * 60
	MaxEscalationRules = 5
)

// EscalationRule is one step of a policy's escalation ladder. Rule n (from 0) fires for a ticket
// at escalation level n once its target's deadline, moved by Minutes before or after, has passed.
// It raises the priority one level, assigns TeamID, or both.
type EscalationRule struct {
	Target        string `json:"target"`
	When          string `json:"when"`
	Minutes       int    `json:"minutes"`
	RaisePriority bool   `json:"raise_priority,omitempty"`
	TeamID        string `json:"team_id,omitempty"`
}

// Team returns the rule's team, or nil when the rule reassigns nobody.
func (r EscalationRule) Team() *uuid.UUID {
	if r.TeamID == "" {
		return nil
	}
	id, err := uuid.Parse(r.TeamID)
	if err != nil {
		return nil
	}
	return &id
}

// Settings are the typed SLA settings kept in a policy's metadata.
type Settings struct {
	NotifyBeforeBreachMin  int
	PauseOnPendingInternal bool
	EscalationRules        []EscalationRule
}

// ParseSettings reads and validates the SLA settings in policy metadata. Other keys are ignored.
func ParseSettings(meta map[string]any) (Settings, error) {
	var s Settings
	if v, ok := meta[MetaNotifyBeforeBreachMin]; ok && v != nil {
		n, ok := wholeNumber(v)
		if !ok || n < 0 || n > MaxLeadMinutes {
			return s, fmt.Errorf("%s must be a whole number from 0 to %d", MetaNotifyBeforeBreachMin, MaxLeadMinutes)
		}
		s.NotifyBeforeBreachMin = n
	}
	if v, ok := meta[MetaPauseOnPendingInternal]; ok && v != nil {
		b, ok := v.(bool)
		if !ok {
			return s, fmt.Errorf("%s must be true or false", MetaPauseOnPendingInternal)
		}
		s.PauseOnPendingInternal = b
	}
	if v, ok := meta[MetaEscalationRules]; ok && v != nil {
		raw, err := json.Marshal(v)
		if err != nil {
			return s, fmt.Errorf("%s: %w", MetaEscalationRules, err)
		}
		if err := json.Unmarshal(raw, &s.EscalationRules); err != nil {
			return s, fmt.Errorf("%s must be a list of rules", MetaEscalationRules)
		}
		if len(s.EscalationRules) > MaxEscalationRules {
			return s, fmt.Errorf("%s allows at most %d rules", MetaEscalationRules, MaxEscalationRules)
		}
		for i, r := range s.EscalationRules {
			if err := r.validate(); err != nil {
				return s, fmt.Errorf("%s[%d]: %w", MetaEscalationRules, i, err)
			}
		}
	}
	return s, nil
}

func (r EscalationRule) validate() error {
	if r.Target != TargetFirstResponse && r.Target != TargetResolution {
		return fmt.Errorf("target must be %s or %s", TargetFirstResponse, TargetResolution)
	}
	if r.When != WhenBeforeBreach && r.When != WhenAfterBreach {
		return fmt.Errorf("when must be %s or %s", WhenBeforeBreach, WhenAfterBreach)
	}
	if r.Minutes < 0 || r.Minutes > MaxLeadMinutes {
		return fmt.Errorf("minutes must be from 0 to %d", MaxLeadMinutes)
	}
	if r.TeamID != "" && r.Team() == nil {
		return fmt.Errorf("team_id is not a UUID")
	}
	if !r.RaisePriority && r.TeamID == "" {
		return fmt.Errorf("a rule must raise the priority, assign a team, or both")
	}
	return nil
}

// wholeNumber accepts a JSON number (float64 after decoding) or an int that has no fraction.
func wholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}
