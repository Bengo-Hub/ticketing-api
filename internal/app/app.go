package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/bengobox/ticketing-service/internal/config"
	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/http/handlers"
	httprouter "github.com/bengobox/ticketing-service/internal/http/router"
	"github.com/bengobox/ticketing-service/internal/modules/identity"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
	"github.com/bengobox/ticketing-service/internal/platform/cache"
	"github.com/bengobox/ticketing-service/internal/platform/database"
	"github.com/bengobox/ticketing-service/internal/platform/events"
	"github.com/bengobox/ticketing-service/internal/shared/logger"
)

// App holds the application dependencies.
type App struct {
	cfg             *config.Config
	log             *zap.Logger
	httpServer      *http.Server
	db              dbCloser
	cache           cacheCloser
	events          eventCloser
	orm             *ent.Client
	outboxPublisher *eventslib.Publisher
}

type dbCloser interface {
	Ping(context.Context) error
	Close()
}

type cacheCloser interface {
	Close() error
}

type eventCloser interface {
	Drain() error
	Close()
}

// New initialises the application with all infrastructure dependencies wired.
func New(ctx context.Context) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	log, err := logger.New(cfg.App.Env)
	if err != nil {
		return nil, fmt.Errorf("app: logger init: %w", err)
	}

	dbPool, err := database.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return nil, fmt.Errorf("app: postgres init: %w", err)
	}

	redisClient := cache.NewClient(cfg.Redis)

	natsConn, err := events.Connect(cfg.Events)
	if err != nil {
		log.Warn("app: nats connect failed", zap.Error(err))
	}

	// Ensure ticketing JetStream stream exists
	if natsConn != nil {
		if streamErr := events.EnsureStream(ctx, natsConn, cfg.Events); streamErr != nil {
			log.Warn("failed to ensure ticketing stream", zap.Error(streamErr))
		}
	}

	healthHandler := handlers.NewHealthHandler(log, dbPool, redisClient, natsConn)

	// Initialize Ent ORM
	sqlDB, err := sql.Open("pgx", cfg.Postgres.URL)
	if err != nil {
		return nil, fmt.Errorf("app: ent driver init: %w", err)
	}
	sqlDB.SetMaxIdleConns(cfg.Postgres.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.Postgres.MaxOpenConns)
	sqlDB.SetConnMaxIdleTime(cfg.Postgres.ConnMaxIdleTime)

	drv := entsql.OpenDB(dialect.Postgres, sqlDB)
	ormClient := ent.NewClient(ent.Driver(drv))
	if err := ormClient.Schema.Create(ctx); err != nil {
		return nil, fmt.Errorf("app: ent schema create: %w", err)
	}

	log.Info("app: migrations completed - run 'go run cmd/seed/main.go' to seed initial data (idempotent)")

	// Initialize auth-service JWT validator
	var authMiddleware *authclient.AuthMiddleware
	if cfg.Auth.ServiceURL != "" {
		jwksURL := fmt.Sprintf("%s/api/v1/.well-known/jwks.json", cfg.Auth.ServiceURL)
		authConfig := authclient.DefaultConfig(
			jwksURL,
			cfg.Auth.Issuer,
			cfg.Auth.Audience,
		)
		authConfig.CacheTTL = cfg.Auth.JWKSCacheTTL
		authConfig.RefreshInterval = cfg.Auth.JWKSRefreshInterval
		authConfig.RedisClient = redisClient

		validator, err := authclient.NewValidator(authConfig)
		if err != nil {
			return nil, fmt.Errorf("auth validator init: %w", err)
		}

		if cfg.Auth.EnableAPIKeyAuth {
			apiKeyValidator := authclient.NewAPIKeyValidator(cfg.Auth.ServiceURL, nil)
			authMiddleware = authclient.NewAuthMiddlewareWithAPIKey(validator, apiKeyValidator)
		} else {
			authMiddleware = authclient.NewAuthMiddleware(validator)
		}
	}

	// Initialize identity module (teams, agents)
	identityRepo := identity.NewEntRepository(ormClient)
	identityService := identity.NewService(identityRepo, log)
	identityHandler := identity.NewHandler(identityService, log)

	// Initialize tickets module (tickets, comments, categories)
	ticketsRepo := tickets.NewEntRepository(ormClient)
	ticketsService := tickets.NewService(ticketsRepo, log)
	ticketsHandler := tickets.NewHandler(ticketsService, log, tickets.WithServiceKey(cfg.Auth.InternalServiceKey))

	// Wire event publisher for ticket lifecycle events (shared-events outbox pattern)
	var outboxPub *eventslib.Publisher
	if natsConn != nil {
		eventPub := events.NewPublisher(sqlDB, log)
		ticketsService.SetPublisher(eventPub)

		js, jsErr := natsConn.JetStream()
		if jsErr != nil {
			log.Warn("jetstream init for outbox publisher", zap.Error(jsErr))
		} else {
			pubCfg := eventslib.DefaultPublisherConfig(js, eventPub.OutboxRepo(), log)
			outboxPub = eventslib.NewPublisher(pubCfg)
		}
	}

	router := httprouter.New(httprouter.Config{
		Log:             log,
		HealthHandler:   healthHandler,
		AuthMiddleware:  authMiddleware,
		AllowedOrigins:  cfg.HTTP.AllowedOrigins,
		IdentityHandler: identityHandler,
		TicketsHandler:  ticketsHandler,
	})

	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:           router,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	return &App{
		cfg:             cfg,
		log:             log,
		httpServer:      httpServer,
		db:              dbPool,
		cache:           redisClient,
		events:          natsConn,
		orm:             ormClient,
		outboxPublisher: outboxPub,
	}, nil
}

// Run starts the HTTP server and blocks until context cancellation.
func (a *App) Run(ctx context.Context) error {
	// Start outbox background publisher for ticketing events
	if a.outboxPublisher != nil {
		go func() {
			if err := a.outboxPublisher.Start(ctx); err != nil {
				a.log.Error("outbox publisher stopped", zap.Error(err))
			}
		}()
		a.log.Info("outbox background publisher started")
	}

	a.log.Info("starting http server", zap.String("addr", a.httpServer.Addr))

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("app: http shutdown: %w", err)
		}

		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("app: http server error: %w", err)
	}
}

// Close releases infrastructure resources.
func (a *App) Close() {
	if a.events != nil {
		if err := a.events.Drain(); err != nil {
			a.log.Warn("failed to drain nats connection", zap.Error(err))
		}
		a.events.Close()
	}

	if a.cache != nil {
		if err := a.cache.Close(); err != nil {
			a.log.Warn("failed to close redis client", zap.Error(err))
		}
	}

	if a.db != nil {
		a.db.Close()
	}

	if a.orm != nil {
		if err := a.orm.Close(); err != nil {
			a.log.Warn("failed to close ent client", zap.Error(err))
		}
	}

	a.log.Sync() //nolint:errcheck // ignore sync error
}
