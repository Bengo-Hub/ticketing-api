package events

import (
	"fmt"

	"github.com/bengobox/ticketing-service/internal/config"
	"github.com/nats-io/nats.go"
)

// Connection wraps a NATS connection.
type Connection struct {
	*nats.Conn
}

// Connect creates a new NATS connection.
func Connect(cfg config.EventsConfig) (*Connection, error) {
	nc, err := nats.Connect(cfg.URL,
		nats.Name("ticketing-service"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(nats.DefaultReconnectWait),
	)
	if err != nil {
		return nil, fmt.Errorf("events: failed to connect to NATS: %w", err)
	}

	return &Connection{Conn: nc}, nil
}
