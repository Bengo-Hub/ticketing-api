// Command worker runs the SLA scans (breaches, pre-breach warnings and escalations) and the
// analytics rollup. It is the only process that runs them (the API pods do not), and every job of
// every tick is claimed fleet-wide through the shared cache, so a second worker replica or a
// restart inside a period never repeats one.
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
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/config"
	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/modules/analytics"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
	"github.com/bengobox/ticketing-service/internal/platform/events"
	"github.com/bengobox/ticketing-service/internal/shared/logger"
)

const (
	// pollInterval is one minute so a warning lands close to its configured lead time.
	pollInterval    = time.Minute
	breachBatch     = 200
	warningBatch    = 200
	escalationBatch = 100
)

// scan is one SLA job, claimed per period under its own RunOnce key.
type scan struct {
	key string
	run func(ctx context.Context, now time.Time)
}

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

	// Every event goes to the outbox in its change's own transaction; the API pods relay them.
	repo := tickets.NewEntRepository(ormClient, db, events.NewOutbox(db))
	svc := tickets.NewService(repo, log)
	rollup := analytics.NewRollup(db, log)
	rollup.Overlap, rollup.OverlapLimit = cfg.Analytics.RollupOverlap, cfg.Analytics.RollupOverlapLimit

	scans := []scan{
		{key: "ticketing:sla-breach-scan", run: func(c context.Context, now time.Time) { runBreachCheck(c, repo, svc, log, now) }},
		{key: "ticketing:sla-warning-scan", run: func(c context.Context, now time.Time) { runWarnings(c, repo, svc, log, now) }},
		{key: "ticketing:sla-escalation-scan", run: func(c context.Context, now time.Time) { runEscalations(c, repo, svc, log, now) }},
		// Last, so the SLA scans never wait behind it; its own budget keeps it inside the tick.
		{key: "ticketing:analytics-rollup", run: func(c context.Context, _ time.Time) { runRollup(c, rollup, log) }},
	}

	log.Info("sla worker started", zap.Duration("poll_interval", pollInterval))
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	tick(ctx, rdb, log, scans)
	for {
		select {
		case <-ticker.C:
			tick(ctx, rdb, log, scans)
		case <-ctx.Done():
			log.Info("sla worker stopping")
			return
		}
	}
}

// tick runs each scan once per period across the fleet. Without Redis (local development) a
// single worker runs every tick.
func tick(ctx context.Context, rdb *redis.Client, log *zap.Logger, scans []scan) {
	for _, s := range scans {
		if rdb == nil {
			s.run(ctx, time.Now())
			continue
		}
		key := sharedcache.PeriodKey(s.key, pollInterval)
		ran, err := sharedcache.RunOnce(ctx, rdb, log, key, pollInterval, func(jobCtx context.Context) error {
			s.run(jobCtx, time.Now())
			return nil
		})
		if err != nil {
			log.Warn("sla scan skipped", zap.String("scan", s.key), zap.Bool("ran", ran), zap.Error(err))
		}
	}
}

// runBreachCheck records each due breach once, with its event, in one transaction per ticket.
func runBreachCheck(ctx context.Context, repo tickets.Repository, svc *tickets.Service, log *zap.Logger, now time.Time) {
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

// runWarnings records each due pre-breach warning once per ticket and target.
func runWarnings(ctx context.Context, repo tickets.Repository, svc *tickets.Service, log *zap.Logger, now time.Time) {
	due, err := repo.ListBreachWarnings(ctx, now, warningBatch)
	if err != nil {
		log.Error("warning scan: failed to list", zap.Error(err))
		return
	}
	for _, w := range due {
		changed, err := svc.RecordBreachWarning(ctx, w, now)
		if err != nil {
			log.Error("warning scan: failed to record", zap.String("ticket_id", w.TicketID.String()), zap.Error(err))
			continue
		}
		if changed {
			log.Info("breach warning recorded", zap.String("ticket_id", w.TicketID.String()), zap.String("target", w.Target))
		}
	}
}

// runEscalations applies each due escalation rule once, a bounded batch per tick.
func runEscalations(ctx context.Context, repo tickets.Repository, svc *tickets.Service, log *zap.Logger, now time.Time) {
	due, err := repo.ListEscalationsDue(ctx, now, escalationBatch)
	if err != nil {
		log.Error("escalation scan: failed to list", zap.Error(err))
		return
	}
	for _, e := range due {
		changed, err := svc.ApplyEscalation(ctx, e, now)
		if err != nil {
			log.Error("escalation scan: failed to apply", zap.String("ticket_id", e.TicketID.String()), zap.Error(err))
			continue
		}
		if changed {
			log.Info("ticket escalated", zap.String("ticket_id", e.TicketID.String()), zap.Int("level", e.Level+1))
		}
	}
}

// runRollup brings the analytics rollups up to date: the buckets touched since the watermark,
// or the next slice of the backfill on a first run, bounded by the rollup's budget.
func runRollup(ctx context.Context, rollup *analytics.Rollup, log *zap.Logger) {
	n, err := rollup.Run(ctx)
	if err != nil {
		log.Error("analytics rollup failed", zap.Int("tickets", n), zap.Error(err))
		return
	}
	if n > 0 {
		log.Info("analytics rollup", zap.Int("tickets", n))
	}
}
