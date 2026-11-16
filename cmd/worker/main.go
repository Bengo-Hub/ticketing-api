// Command worker runs the SLA breach scan. It is the only process that runs it (the API pods do
// not), and every tick is claimed fleet-wide through the shared cache, so a second worker replica
// or a restart inside a period never repeats a scan.
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
	sharedcache "github.com/Bengo-Hub/cache"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/config"
	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
	"github.com/bengobox/ticketing-service/internal/platform/events"
	"github.com/bengobox/ticketing-service/internal/shared/logger"
)

const (
	pollInterval = 5 * time.Minute
	breachBatch  = 200
	runOnceKey   = "ticketing:sla-breach-scan"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		zap.NewExample().Fatal("load config", zap.Error(err))
	}
	log, err := logger.New(cfg.App.Env)
	if err != nil {
		log, _ = zap.NewProduction()
	}

	db, err := sql.Open("pgx", cfg.Postgres.URL)
	if err != nil {
		log.Fatal("failed to open database", zap.Error(err))
	}
	defer db.Close()
	db.SetMaxOpenConns(cfg.Postgres.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Postgres.MaxIdleConns)

	ormClient := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer ormClient.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb, redisErr := sharedcache.NewRedis(ctx, sharedcache.RedisConfig{
		URL: cfg.Redis.URL, Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
	})
	if redisErr != nil {
		log.Warn("worker: redis not reachable at startup; scans wait until it is", zap.Error(redisErr))
	}

	// Breach events go to the outbox in the breach's own transaction; the API pods relay them.
	repo := tickets.NewEntRepository(ormClient, db, events.NewOutbox(db))
	svc := tickets.NewService(repo, log)

	log.Info("sla breach worker started", zap.Duration("poll_interval", pollInterval))
	tick := func() {
		if rdb == nil {
			// No Redis configured (local development): a single worker runs every tick.
			runBreachCheck(ctx, repo, svc, log)
			return
		}
		key := sharedcache.PeriodKey(runOnceKey, pollInterval)
		ran, err := sharedcache.RunOnce(ctx, rdb, log, key, pollInterval, func(jobCtx context.Context) error {
			runBreachCheck(jobCtx, repo, svc, log)
			return nil
		})
		if err != nil {
			log.Warn("sla breach scan skipped", zap.Bool("ran", ran), zap.Error(err))
		}
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	tick()
	for {
		select {
		case <-ticker.C:
			tick()
		case <-ctx.Done():
			log.Info("sla breach worker stopping")
			return
		}
	}
}

// runBreachCheck records each due breach once, with its event, in one transaction per ticket.
func runBreachCheck(ctx context.Context, repo tickets.Repository, svc *tickets.Service, log *zap.Logger) {
	now := time.Now()
	due, err := repo.ListTicketsPendingBreach(ctx, now, breachBatch)
	if err != nil {
		log.Error("breach check: failed to list pending breaches", zap.Error(err))
		return
	}
	for _, t := range due {
		changed, err := svc.RecordSLABreach(ctx, t, now)
		if err != nil {
			log.Error("breach check: failed to record breach", zap.String("ticket_id", t.ID.String()), zap.Error(err))
			continue
		}
		if changed {
			log.Info("breach recorded", zap.String("ticket_id", t.ID.String()))
		}
	}
}
