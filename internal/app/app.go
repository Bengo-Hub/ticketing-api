package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	sharedcache "github.com/Bengo-Hub/cache"
	"net/http"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/Bengo-Hub/httpware/objectstore"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	eventslib "github.com/Bengo-Hub/shared-events"
	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"github.com/Bengo-Hub/shared-service-client/crm"
	"github.com/bengobox/ticketing-service/internal/clients"
	"github.com/bengobox/ticketing-service/internal/config"
	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/http/handlers"
	"github.com/bengobox/ticketing-service/internal/http/metrics"
	httprouter "github.com/bengobox/ticketing-service/internal/http/router"
	"github.com/bengobox/ticketing-service/internal/modules/identity"
	"github.com/bengobox/ticketing-service/internal/modules/ingest"
	"github.com/bengobox/ticketing-service/internal/modules/kb"
	"github.com/bengobox/ticketing-service/internal/modules/rbac"
	"github.com/bengobox/ticketing-service/internal/modules/routing"
	"github.com/bengobox/ticketing-service/internal/modules/settings"
	"github.com/bengobox/ticketing-service/internal/modules/sla"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
	"github.com/bengobox/ticketing-service/internal/platform/database"
	"github.com/bengobox/ticketing-service/internal/platform/events"
	"github.com/bengobox/ticketing-service/internal/platform/sse"
	"github.com/bengobox/ticketing-service/internal/platform/tenantcache"
	"github.com/bengobox/ticketing-service/internal/shared/logger"
	"github.com/google/uuid"
)

// App holds the application dependencies.
type App struct {
	cfg             *config.Config
	log             *zap.Logger
	httpServer      *http.Server
	metricsServer   *http.Server
	db              dbCloser
	cache           cacheCloser
	events          eventCloser
	orm             *ent.Client
	outboxPublisher *eventslib.Publisher
}

// slaAdapter adapts sla.Service to the tickets.SLAComputer interface.
type slaAdapter struct{ svc *sla.Service }

// routingAdapter adapts routing.Service to the tickets.RulesEvaluator interface.
type routingAdapter struct{ svc *routing.Service }

func (a *routingAdapter) EvaluateRules(ctx context.Context, tenantID uuid.UUID, priority, channel, subject, description string, categoryID *uuid.UUID) *tickets.RoutingAction {
	applied := a.svc.EvaluateRules(ctx, routing.TicketContext{
		TenantID:    tenantID,
		Priority:    priority,
		CategoryID:  categoryID,
		Channel:     channel,
		Subject:     subject,
		Description: description,
	})
	if applied == nil {
		return nil
	}
	valStr, _ := applied.Action.Value.(string)
	return &tickets.RoutingAction{
		Type:  string(applied.Action.Type),
		Value: valStr,
	}
}

func (a *slaAdapter) ComputeDeadlines(ctx context.Context, tenantID uuid.UUID, priority string, categoryID *uuid.UUID, start time.Time) (*tickets.SLAPlan, error) {
	result, err := a.svc.ComputeDeadlines(ctx, tenantID, sla.Priority(priority), categoryID, start)
	if err != nil || result == nil {
		return nil, err
	}
	return &tickets.SLAPlan{PolicyID: result.PolicyID, ResponseDueAt: result.ResponseDueAt, ResolutionDueAt: result.ResolutionDueAt}, nil
}

func (a *slaAdapter) PolicyClock(ctx context.Context, tenantID uuid.UUID, policyID *uuid.UUID) (tickets.SLAClock, error) {
	c, err := a.svc.PolicyClock(ctx, tenantID, policyID)
	if err != nil {
		return tickets.SLAClock{}, err
	}
	return tickets.SLAClock{Calendar: c.Calendar, PauseOnPendingInternal: c.PauseOnPendingInternal}, nil
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

	redisClient, redisErr := sharedcache.NewRedis(ctx, sharedcache.RedisConfig{
		URL: cfg.Redis.URL, Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
	})
	if redisErr != nil {
		log.Warn("app: redis not reachable at startup", zap.Error(redisErr))
	}
	sharedcache.SetLeaseClient(redisClient)
	tenantResolver := tenantcache.New(redisClient, cfg.Auth.ServiceURL, cfg.Tenant.CacheTTL, log)

	natsConn, err := events.Connect(cfg.Events)
	if err != nil {
		log.Warn("app: nats connect failed", zap.Error(err))
	}
	var relay *eventslib.Broadcaster
	if natsConn != nil {
		relay = eventslib.NewBroadcaster(log, natsConn, "ticketing")
		// Drop revoked/rotated API keys from every validator on this pod at once.
		_ = eventslib.NewBroadcaster(log, natsConn, "auth").Subscribe("apikey.changed", func(m eventslib.BroadcastMessage) {
			authclient.InvalidateAPIKeyHash(string(m.Data))
		})
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
	// Schema changes are applied once per rollout by ticketing-migrate (entrypoint,
	// advisory-locked, versioned migrations). The ent auto-diff that used to run here on every
	// pod start was unlocked, ran through PgBouncer, and bypassed the versioned migrations.

	// Layer 1: auth-api JWT validator, with API keys accepted unless turned off in config.
	if cfg.Auth.ServiceURL == "" {
		return nil, errors.New("app: AUTH_SERVICE_URL is required")
	}
	authConfig := authclient.DefaultConfig(
		fmt.Sprintf("%s/api/v1/.well-known/jwks.json", cfg.Auth.ServiceURL),
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
	authMiddleware := authclient.NewAuthMiddleware(validator)
	if cfg.Auth.EnableAPIKeyAuth {
		authMiddleware = authclient.NewAuthMiddlewareWithAPIKey(validator, authclient.NewAPIKeyValidator(cfg.Auth.ServiceURL, nil))
	}

	// Layer 3: the ticketing permission catalogue is seeded on every start (idempotent); each
	// tenant's system roles are seeded the first time one of its users signs in.
	rbacService := rbac.NewService(rbac.NewEntRepository(ormClient), log)
	seedCtx, seedCancel := context.WithTimeout(ctx, 30*time.Second)
	seedErr := rbacService.Seed(seedCtx)
	seedCancel()
	if seedErr != nil {
		return nil, fmt.Errorf("app: seed ticketing permissions: %w", seedErr)
	}
	rbacHandler := rbac.NewHandler(rbacService, log)

	// Notifications go out only through ticket events (ticketing.>, notifications-api's ticketing
	// consumer); there is no direct notifications client.

	// Initialize OTP validator (calls auth-api to validate public OTP tokens)
	otpClient := clients.NewOTPClient(cfg.Auth.ServiceURL, cfg.Auth.InternalServiceKey, log)

	// Initialize rate limiter for public endpoints
	rl := ratelimit.NewLimiter(redisClient, log, "ticketing")

	// Initialize identity module (teams, agents)
	identityRepo := identity.NewEntRepository(ormClient)
	identityService := identity.NewService(identityRepo, log)
	identityHandler := identity.NewHandler(identityService, log)

	// auth.user.created and auth.user.updated provision staff ahead of sign-in and refresh agents.
	authEventHandler := identity.NewAuthEventHandler(rbacService, log)
	if natsConn != nil {
		if err := authEventHandler.SubscribeToAuthEvents(natsConn); err != nil {
			log.Warn("failed to subscribe to auth events", zap.Error(err))
		}
	}

	// SSE broker: per-tenant ticket events, relayed across replicas.
	sseBroker, sseErr := sse.New(relay)
	if sseErr != nil {
		log.Warn("app: sse relay subscribe failed, single-replica delivery only", zap.Error(sseErr))
	}

	// Initialize KB module
	kbRepo := kb.NewEntRepository(ormClient)
	kbService := kb.NewService(kbRepo, log)
	kbHandler := kb.NewHandler(kbService, log)
	kbHandler.SetTenantLookup(tenantResolver)

	// Initialize SLA module
	slaRepo := sla.NewEntRepository(ormClient)
	slaService := sla.NewService(slaRepo, log)
	slaHandler := sla.NewHandler(slaService, sla.NewReportRepository(sqlDB), log)

	// Initialize routing module (auto-routing rules)
	routingRepo := routing.NewEntRepository(ormClient)
	routingService := routing.NewService(routingRepo, log)
	routingHandler := routing.NewHandler(routingService, log)

	// Initialize tickets module (tickets, comments, categories)
	// Ticket changes and their events commit together: the repository writes outbox rows in the
	// change's own transaction, whether or not NATS is up. The relay below drains them.
	outbox := events.NewOutbox(sqlDB)
	ticketsRepo := tickets.NewEntRepository(ormClient, sqlDB, outbox)
	ticketsService := tickets.NewService(ticketsRepo, log)
	ticketsService.SetSLAService(&slaAdapter{svc: slaService})
	ticketsService.SetRoutingService(&routingAdapter{svc: routingService})
	ticketsService.SetSSEBroker(sseBroker)
	// Attachments live in the private R2 bucket (OBJECT_STORAGE_*), or under MEDIA_ROOT locally.
	if fileStore, storeErr := objectstore.New("ticketing", objectstore.FromEnv()); storeErr != nil {
		log.Warn("app: object storage not configured, attachment uploads are disabled", zap.Error(storeErr))
	} else {
		ticketsService.SetFileStore(fileStore)
	}
	// Collision presence keeps short-lived Redis keys and broadcasts over the SSE relay.
	var presenceStore tickets.PresenceStore
	if redisClient != nil {
		presenceStore = tickets.NewRedisPresenceStore(redisClient)
	}
	ticketsHandler := tickets.NewHandler(ticketsService, log,
		tickets.WithServiceKey(cfg.Auth.InternalServiceKey),
		tickets.WithPresence(tickets.NewPresence(presenceStore, sseBroker)),
	)
	publicTicketsHandler := tickets.NewPublicHandler(ticketsService, otpClient, rl, log)
	publicTicketsHandler.SetTenantLookup(tenantResolver)
	// Ticket access tokens for the public read and reply routes are signed with a key derived
	// from the service key; without it the public read refuses every request.
	publicTicketsHandler.SetAccessKey(cfg.Auth.InternalServiceKey)
	if cfg.Auth.InternalServiceKey == "" {
		log.Warn("INTERNAL_SERVICE_KEY is empty: public ticket reads are disabled")
	}

	// Tenant settings (channels.whatsapp.auto_ticket) and the channel ingest: email from
	// email-provisioner and WhatsApp from notifications-api become tickets through the tickets
	// service, so numbering, routing, SLA and events apply as for any other ticket.
	settingsService := settings.NewService(settings.NewStore(sqlDB))
	supportAddresses := ingest.Addresses{
		SupportDomain:   cfg.Channels.SupportDomain,
		PlatformAddress: cfg.Channels.PlatformAddress,
		PlatformSlug:    cfg.Channels.PlatformSlug,
	}
	settingsHandler := settings.NewHandler(settingsService, supportAddresses.ForwardingAddress, log)
	if natsConn != nil && cfg.Channels.Enabled {
		if js, jsErr := natsConn.JetStream(); jsErr != nil {
			log.Warn("channel ingest not started", zap.Error(jsErr))
		} else {
			ingestService := ingest.NewService(ticketsService, log)
			ingestService.SetThreadWindow(cfg.Channels.WhatsAppWindow)
			// The requester of a new ticket is linked to their marketflow contact (best effort; an
			// empty URL or service key turns the lookup off).
			ingestService.SetContacts(crm.New(cfg.Marketflow.URL, cfg.Auth.InternalServiceKey, log.Named("crm")))
			ingest.NewConsumers(ingestService, ingest.NewTenantResolver(supportAddresses, tenantResolver), settingsService, supportAddresses,
				ingest.ConsumerConfig{
					EmailStream: cfg.Channels.EmailStream, EmailSubject: cfg.Channels.EmailSubject,
					WhatsAppStream: cfg.Channels.WhatsAppStream, WhatsAppSubject: cfg.Channels.WhatsAppSubject,
				}, log).Start(js)
		}
	}

	// The shared-events background publisher relays outbox rows to JetStream.
	var outboxPub *eventslib.Publisher
	if natsConn != nil {
		js, jsErr := natsConn.JetStream()
		if jsErr != nil {
			log.Warn("jetstream init for outbox publisher", zap.Error(jsErr))
		} else {
			pubCfg := eventslib.DefaultPublisherConfig(js, outbox.OutboxRepo(), log)
			outboxPub = eventslib.NewPublisher(pubCfg)
			// tenant.purge (dormancy, a deleted tenant, a declined registration): delete every row
			// this service holds for the tenant, by the shared schema-driven purger.
			purger := &eventslib.TenantPurger{DB: sqlDB, Log: log.Named("tenant-purge"), Service: "ticketing"}
			if err := purger.Subscribe(js); err != nil {
				log.Warn("tenant purge consumer not started", zap.Error(err))
			}
		}
	}

	router := httprouter.New(httprouter.Config{
		Log:                  log,
		HealthHandler:        healthHandler,
		AuthMiddleware:       authMiddleware,
		Limiter:              rl,
		RBACHandler:          rbacHandler,
		AccessResolver:       rbacService,
		AllowedOrigins:       cfg.HTTP.AllowedOrigins,
		IdentityHandler:      identityHandler,
		TicketsHandler:       ticketsHandler,
		PublicTicketsHandler: publicTicketsHandler,
		SLAHandler:           slaHandler,
		KBHandler:            kbHandler,
		RoutingHandler:       routingHandler,
		SettingsHandler:      settingsHandler,
		SSEBroker:            sseBroker,
		TenantResolver:       tenantResolver,
		CRMLinks:             tickets.NewCRMLinksHandler(ormClient, tenantResolver, cfg.Auth.InternalServiceKey, log),
	})

	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:           router,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.MetricsPort),
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{
		cfg:             cfg,
		log:             log,
		httpServer:      httpServer,
		metricsServer:   metricsServer,
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

	go func() {
		a.log.Info("starting metrics server", zap.String("addr", a.metricsServer.Addr))
		if err := a.metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Error("metrics server stopped", zap.Error(err))
		}
	}()

	a.log.Info("starting http server", zap.String("addr", a.httpServer.Addr))

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = a.metricsServer.Shutdown(shutdownCtx)

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
