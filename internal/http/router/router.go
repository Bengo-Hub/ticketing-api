package router

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/ticketing-service/internal/http/handlers"
	"github.com/bengobox/ticketing-service/internal/modules/identity"
	"github.com/bengobox/ticketing-service/internal/modules/kb"
	"github.com/bengobox/ticketing-service/internal/modules/sla"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
	sharedmw "github.com/bengobox/ticketing-service/internal/shared/middleware"
)

// tenantResolver resolves tenant details from cache/auth-api.
type tenantResolver interface {
	GetBranding(ctx context.Context, slug string) (sharedcache.TenantBranding, error)
	GetTenantDetails(ctx context.Context, slug string) (sharedcache.TenantDetails, error)
}

// Config holds all module handlers for the router.
type Config struct {
	Log             *zap.Logger
	HealthHandler   *handlers.HealthHandler
	AuthMiddleware  *authclient.AuthMiddleware
	AllowedOrigins  []string
	IdentityHandler      *identity.Handler
	TicketsHandler       *tickets.Handler
	PublicTicketsHandler *tickets.PublicHandler
	SLAHandler           *sla.Handler
	KBHandler            *kb.Handler
	TenantResolver       tenantResolver
}

// New constructs the chi router with global middleware and base routes.
func New(cfg Config) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Heartbeat("/readyz"))
	r.Use(sharedmw.RequestID)
	r.Use(sharedmw.Logging(cfg.Log))
	r.Use(sharedmw.Recover(cfg.Log))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID", "X-Tenant-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health endpoints (no auth required)
	r.Get("/healthz", cfg.HealthHandler.Liveness)
	r.Get("/metrics", cfg.HealthHandler.Metrics)

	// Redirect root path to Swagger documentation
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v1/docs/", http.StatusMovedPermanently)
	})

	// API routes
	r.Route("/api", func(api chi.Router) {
		api.Route("/v1", func(v1 chi.Router) {
			// S2S internal endpoint — no JWT auth, validated by X-API-Key header
			if cfg.TicketsHandler != nil {
				v1.Post("/internal/tickets", cfg.TicketsHandler.InternalCreateTicket)
			}

			// Public (no-auth) tenant-scoped routes
			if cfg.PublicTicketsHandler != nil || cfg.KBHandler != nil {
				v1.Route("/{tenant}", func(pub chi.Router) {
					if cfg.PublicTicketsHandler != nil {
						cfg.PublicTicketsHandler.RegisterPublicRoutes(pub)
					}
					if cfg.KBHandler != nil {
						cfg.KBHandler.RegisterPublicRoutes(pub)
					}
				})
			}

			// Apply auth middleware to protected routes
			if cfg.AuthMiddleware != nil {
				v1.Use(cfg.AuthMiddleware.RequireAuth)
			}

			// Health status (auth required)
			v1.Get("/status", cfg.HealthHandler.Status)

			// Tenant-scoped routes
			v1.Route("/{tenant}", func(tenant chi.Router) {
				// Branding endpoint — returns tenant details from cache/auth-api (no DB query)
				if cfg.TenantResolver != nil {
					tenant.Get("/branding", func(w http.ResponseWriter, r *http.Request) {
						slug := chi.URLParam(r, "tenant")
						details, err := cfg.TenantResolver.GetTenantDetails(r.Context(), slug)
						if err != nil {
							http.Error(w, `{"error":"tenant not found"}`, http.StatusNotFound)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(details)
					})
				}

				// Identity module (teams, agents)
				if cfg.IdentityHandler != nil {
					cfg.IdentityHandler.RegisterRoutes(tenant)
				}

				// Tickets module (tickets, comments, categories)
				if cfg.TicketsHandler != nil {
					cfg.TicketsHandler.RegisterRoutes(tenant)
				}

				// SLA module (policies)
				if cfg.SLAHandler != nil {
					cfg.SLAHandler.RegisterRoutes(tenant)
				}

				// KB module (articles)
				if cfg.KBHandler != nil {
					cfg.KBHandler.RegisterRoutes(tenant)
				}
			})
		})
	})

	return r
}
