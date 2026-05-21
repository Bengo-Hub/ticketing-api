package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

// Event is a typed SSE message sent to subscribers.
type Event struct {
	Type    string `json:"type"`    // e.g. "ticket.updated", "comment.created"
	Payload any    `json:"payload"` // serialisable payload
}

// Broker manages per-tenant SSE subscriptions.
type Broker struct {
	mu   sync.RWMutex
	subs map[string]map[uint64]chan Event // tenantID → {subID → ch}
	next uint64
}

// New creates a new Broker.
func New() *Broker {
	return &Broker{subs: make(map[string]map[uint64]chan Event)}
}

// Subscribe returns a channel that will receive events for tenantID.
// Call the returned unsub function to clean up.
func (b *Broker) Subscribe(tenantID uuid.UUID) (<-chan Event, func()) {
	key := tenantID.String()

	b.mu.Lock()
	b.next++
	id := b.next
	ch := make(chan Event, 16)
	if b.subs[key] == nil {
		b.subs[key] = make(map[uint64]chan Event)
	}
	b.subs[key][id] = ch
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		delete(b.subs[key], id)
		close(ch)
		b.mu.Unlock()
	}
	return ch, unsub
}

// Publish sends an event to all subscribers of tenantID.
// Implements tickets.SSEPublisher so it can be wired directly into the service.
func (b *Broker) Publish(tenantID uuid.UUID, eventType string, payload any) {
	b.publish(tenantID, Event{Type: eventType, Payload: payload})
}

func (b *Broker) publish(tenantID uuid.UUID, evt Event) {
	key := tenantID.String()
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[key] {
		select {
		case ch <- evt:
		default: // drop if subscriber is slow
		}
	}
}

// ServeHTTP streams SSE events for a tenant until the client disconnects.
// Expects tenantID to be set by the handler.
func (b *Broker) ServeStream(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := b.Subscribe(tenantID)
	defer unsub()

	// Send an initial "connected" event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		}
	}
}
