package tickets

import "strings"

// Outbox aggregates. An event goes out on the subject {aggregate}.{event type}: ticket events as
// ticketing.ticket.*, SLA breaches as ticketing.sla.*, and CRM activity as
// crm.activity.log_requested, which the CRM service's own stream captures.
const (
	AggregateTicketing = "ticketing"
	AggregateCRM       = "crm"
)

// Ticket event types (subject ticketing.<type>). notifications-api's ticketing consumer turns them
// into email, WhatsApp and SMS for the requester or the agent.
const (
	EventTicketCreated       = "ticket.created"
	EventTicketAssigned      = "ticket.assigned"
	EventTicketCommentAdded  = "ticket.comment_added"
	EventTicketStatusChanged = "ticket.status_changed"
	EventTicketResolved      = "ticket.resolved"
	EventCRMActivity         = "activity.log_requested"
	// EventTicketMentioned goes to one agent a comment @mentions (one event per agent).
	EventTicketMentioned = "ticket.mentioned"
	// EventTicketMerged records a ticket merged into another; the source is closed.
	EventTicketMerged = "ticket.merged"
)

// OutboxEvent is one event recorded during a domain change.
type OutboxEvent struct {
	Aggregate string
	Type      string
	Data      map[string]any
}

// Outbox collects the events of one domain change. The repository writes them to outbox_events in
// the same transaction as the change, so an event exists exactly when its change committed.
type Outbox struct {
	Events []OutboxEvent
}

// Add records an event.
func (o *Outbox) Add(aggregate, eventType string, data map[string]any) {
	o.Events = append(o.Events, OutboxEvent{Aggregate: aggregate, Type: eventType, Data: data})
}

// Ticket records a ticketing.ticket.* event.
func (o *Outbox) Ticket(eventType string, data map[string]any) {
	o.Add(AggregateTicketing, eventType, data)
}

// CRMActivity records a crm.activity.log_requested event for the ticket's CRM contact.
func (o *Outbox) CRMActivity(data map[string]any) {
	o.Add(AggregateCRM, EventCRMActivity, data)
}

// SLABreach records ticketing.sla.first_response_breached or ticketing.sla.resolution_breached.
func (o *Outbox) SLABreach(data map[string]any) {
	o.Add(AggregateTicketing, SLABreachEventType(data), data)
}

// SLABreachEventType is "sla.first_response_breached" or "sla.resolution_breached" from the
// payload's event_type, with or without its "sla." prefix; first response is the default.
func SLABreachEventType(data map[string]any) string {
	kind := "first_response_breached"
	if t, ok := data["event_type"].(string); ok && t != "" {
		kind = strings.TrimPrefix(t, "sla.")
	}
	return "sla." + kind
}

// crmActivity is the crm.activity.log_requested payload for a ticket's CRM contact.
func crmActivity(t *Ticket, activity string) map[string]any {
	return map[string]any{
		"entity_type":   "contact",
		"entity_id":     t.CRMContactID.String(),
		"activity_type": activity,
		"external_ref":  t.ID.String(),
		"subject":       t.Subject,
		"source":        "ticketing-api",
	}
}
