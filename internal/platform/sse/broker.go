package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Bengo-Hub/httpware"
	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
)

// Event is a typed SSE message sent to subscribers.
type Event struct {
	Type    string `json:"type"`    // e.g. "ticket.updated", "comment.created"
	Payload any    `json:"payload"` // serialisable payload
}

// heartbeatInterval keeps an idle stream open through ingress and proxy idle timeouts.
const heartbeatInterval = 15 * time.Second

// Broker streams per-tenant ticket events to SSE clients on every replica.
//
// A client's stream lives on whichever replica the load balancer picked while the ticket
// change happens on whichever replica handled the write, so events go through the shared
// events.FanoutHub, which relays them to all replicas (each delivers to its own streams).
// The previous broker was in-memory only, so clients on other pods never saw the change.
type Broker struct {
	fan *eventslib.FanoutHub
}

// New creates a Broker. relay may be nil (single replica, tests): delivery is then local only.
func New(relay *eventslib.Broadcaster) (*Broker, error) {
	fan, err := eventslib.NewFanoutHub(relay, "ticket-sse", 16)
	return &Broker{fan: fan}, err
}

// Publish sends an event to all subscribers of tenantID on every replica.
// Implements tickets.SSEPublisher so it can be wired directly into the service.
func (b *Broker) Publish(tenantID uuid.UUID, eventType string, payload any) {
	data, err := json.Marshal(Event{Type: eventType, Payload: payload})
	if err != nil {
		return
	}
	b.fan.Publish(tenantID.String(), "", data)
}

// ServeStream streams SSE events for a tenant until the client disconnects.
func (b *Broker) ServeStream(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	httpware.StreamHeaders(w)
	// The server WriteTimeout (30s) would cut the stream; the heartbeat keeps it alive instead.
	httpware.ExtendWriteDeadline(w)
	flusher := http.NewResponseController(w)

	sub := b.fan.Subscribe(tenantID.String())
	defer b.fan.Unsubscribe(sub)

	fmt.Fprint(w, "event: connected\ndata: {}\n\n")
	if err := flusher.Flush(); err != nil {
		return
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			if flusher.Flush() != nil {
				return
			}
		case raw, ok := <-sub.C:
			if !ok {
				return
			}
			var evt Event
			if json.Unmarshal(raw, &evt) != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, raw)
			if flusher.Flush() != nil {
				return
			}
		}
	}
}
