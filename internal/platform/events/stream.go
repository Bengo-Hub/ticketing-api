package events

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/bengobox/ticketing-service/internal/config"
)

// EnsureStream creates or updates the ticketing JetStream stream.
func EnsureStream(ctx context.Context, nc *nats.Conn, cfg config.EventsConfig) error {
	if nc == nil {
		return fmt.Errorf("nats connection is nil")
	}

	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("jetstream init: %w", err)
	}

	_, err = js.StreamInfo(cfg.StreamName)
	if err == nil {
		return nil
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:     cfg.StreamName,
		Subjects: []string{"ticketing.>"},
		Replicas: 1,
	})
	return err
}
