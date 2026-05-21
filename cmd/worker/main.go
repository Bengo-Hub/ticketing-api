package main

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"syscall"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/nats-io/nats.go"
	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
	"github.com/bengobox/ticketing-service/internal/platform/events"
	"github.com/bengobox/ticketing-service/internal/shared/logger"
)

const (
	pollInterval   = 5 * time.Minute
	breachBatch    = 200
)

func main() {
	_ = godotenv.Load()

	log, err := logger.New(os.Getenv("APP_ENV"))
	if err != nil {
		log, _ = zap.NewProduction()
	}

	dbURL := os.Getenv("POSTGRES_URL")
	if dbURL == "" {
		log.Fatal("POSTGRES_URL is required")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatal("failed to open database", zap.Error(err))
	}
	defer db.Close()

	drv := entsql.OpenDB(dialect.Postgres, db)
	ormClient := ent.NewClient(ent.Driver(drv))
	defer ormClient.Close()

	natsURL := os.Getenv("EVENTS_NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	var publisher *events.Publisher
	nc, err := nats.Connect(natsURL,
		nats.Name("ticketing-sla-worker"),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		log.Warn("worker: nats connect failed — breach events will not be published", zap.Error(err))
	} else {
		publisher = events.NewPublisher(db, log)
		defer nc.Close()
	}

	ticketsRepo := tickets.NewEntRepository(ormClient)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("sla breach worker started", zap.Duration("poll_interval", pollInterval))

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Run once immediately at startup
	runBreachCheck(ctx, ticketsRepo, publisher, log)

	for {
		select {
		case <-ticker.C:
			runBreachCheck(ctx, ticketsRepo, publisher, log)
		case <-ctx.Done():
			log.Info("sla breach worker stopping")
			return
		}
	}
}

func runBreachCheck(ctx context.Context, repo tickets.Repository, publisher *events.Publisher, log *zap.Logger) {
	now := time.Now()
	breached, err := repo.ListTicketsPendingBreach(ctx, now, breachBatch)
	if err != nil {
		log.Error("breach check: failed to list pending breaches", zap.Error(err))
		return
	}

	if len(breached) == 0 {
		return
	}

	log.Info("breach check: processing breaches", zap.Int("count", len(breached)))

	for _, t := range breached {
		responseBreached := t.SLAResponseDueAt != nil && now.After(*t.SLAResponseDueAt) &&
			!t.SLAResponseBreached && t.FirstResponseAt == nil
		resolutionBreached := t.SLAResolutionDueAt != nil && now.After(*t.SLAResolutionDueAt) &&
			!t.SLAResolutionBreached

		if !responseBreached && !resolutionBreached {
			continue
		}

		if err := repo.MarkSLABreached(ctx, t.TenantID, t.ID, responseBreached, resolutionBreached); err != nil {
			log.Error("breach check: failed to mark breach",
				zap.String("ticket_id", t.ID.String()),
				zap.Error(err),
			)
			continue
		}

		// Publish breach event for notifications-api to consume
		if publisher != nil {
			eventType := "sla.first_response_breached"
			if resolutionBreached {
				eventType = "sla.resolution_breached"
			}
			_ = publisher.PublishSLABreach(ctx, t.TenantID, map[string]any{
				"ticket_id":           t.ID.String(),
				"ticket_number":       t.TicketNumber,
				"subject":             t.Subject,
				"priority":            string(t.Priority),
				"response_breached":   responseBreached,
				"resolution_breached": resolutionBreached,
				"event_type":          eventType,
			})
		}

		log.Info("breach marked",
			zap.String("ticket_id", t.ID.String()),
			zap.Bool("response_breached", responseBreached),
			zap.Bool("resolution_breached", resolutionBreached),
		)
	}
}
