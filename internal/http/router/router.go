package router

import (
	"context"
	"encoding/json"
	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	_ "github.com/bengobox/ticketing-service/docs"
	"github.com/bengobox/ticketing-service/internal/http/handlers"
	"github.com/bengobox/ticketing-service/internal/modules/identity"
	"github.com/bengobox/ticketing-service/internal/modules/kb"
	"github.com/bengobox/ticketing-service/internal/modules/routing"
	"github.com/bengobox/ticketing-service/internal/modules/sla"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
	"github.com/bengobox/ticketing-service/internal/platform/sse"
	httpware "github.com/Bengo-Hub/httpware"
)

// tenantResolver resolves tenant details from cache/auth-api.
type tenantResolver interface {
	GetBranding(ctx context.Context, slug string) (sharedcache.TenantBranding, error)
	GetTenantDetails(ctx context.Context, slug string) (sharedcache.TenantDetails, error)
}

// Config holds all module handlers for the router.
type Config struct {
	Log                  *zap.Logger
	HealthHandler        *handlers.HealthHandler
	AuthMiddleware       *authclient.AuthMiddleware
	AllowedOrigins       []string
	IdentityHandler      *identity.Handler
	TicketsHandler       *tickets.Handler
	PublicTicketsHandler *tickets.PublicHandler
	SLAHandler           *sla.Handler
	KBHandler            *kb.Handler
	RoutingHandler       *routing.Handler
	SSEBroker            *sse.Broker
	TenantResolver       tenantResolver
}

// New constructs the chi router with global middleware and base routes.
func New(cfg Config) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	// Never chi RealIP: it trusts client-sent True-Client-IP/X-Forwarded-For.
	r.Use(ratelimit.TrustedRealIP)
	r.Use(middleware.Heartbeat("/readyz"))
	r.Use(httpware.RequestID)
	r.Use(httpware.Logging(cfg.Log))
	r.Use(httpware.Recover(cfg.Log))
	r.Use(httpware.CORS(httpware.StandardCORS(cfg.AllowedOrigins)))

	// Health endpoints (no auth required)
	r.Get("/healthz", cfg.HealthHandler.Liveness)
	r.Get("/metrics", cfg.HealthHandler.Metrics)

	// Swagger UI
	r.Get("/v1/docs/*", httpSwagger.WrapHandler)

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
				v1.Get("/internal/tickets/{ticketNumber}", cfg.TicketsHandler.InternalGetTicket)
			}

			// Health status (auth required) lives in its own Group so Use()
			// precedes it — v1 already has the S2S route above, so auth
			// can't be added via a bare v1.Use here.
			v1.Group(func(protected chi.Router) {
				if cfg.AuthMiddleware != nil {
					protected.Use(cfg.AuthMiddleware.RequireAuth)
				}
				protected.Get("/status", cfg.HealthHandler.Status)
			})

			// Tenant-scoped routes: ONE /{tenant} sub-router hosting both the
			// public (no-auth) routes and, nested inside it, the auth-protected
			// ones. chi panics if the same mux mounts two Route()/Mount() calls
			// at the same pattern, so these must not be two separate
			// v1.Route("/{tenant}", ...) calls — the protected subset instead
			// gets its own inner Group() so Use() still precedes its routes.
			v1.Route("/{tenant}", func(tenant chi.Router) {
				// Public (no-auth) routes
				if cfg.PublicTicketsHandler != nil {
					cfg.PublicTicketsHandler.RegisterPublicRoutes(tenant)
				}
				if cfg.KBHandler != nil {
					cfg.KBHandler.RegisterPublicRoutes(tenant)
				}

				// Auth-protected routes
				tenant.Group(func(protected chi.Router) {
					if cfg.AuthMiddleware != nil {
						protected.Use(cfg.AuthMiddleware.RequireAuth)
					}

					// Branding endpoint — returns tenant details from cache/auth-api (no DB query)
					if cfg.TenantResolver != nil {
						protected.Get("/branding", func(w http.ResponseWriter, r *http.Request) {
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
						cfg.IdentityHandler.RegisterRoutes(protected)
					}

					// Tickets module (tickets, comments, categories)
					if cfg.TicketsHandler != nil {
						cfg.TicketsHandler.RegisterRoutes(protected)
					}

					// SLA module (policies)
					if cfg.SLAHandler != nil {
						cfg.SLAHandler.RegisterRoutes(protected)
					}

					// KB module (articles)
					if cfg.KBHandler != nil {
						cfg.KBHandler.RegisterRoutes(protected)
					}

					// Routing rules module
					if cfg.RoutingHandler != nil {
						cfg.RoutingHandler.RegisterRoutes(protected)
					}

					// SSE streaming endpoints
					if cfg.TicketsHandler != nil && cfg.SSEBroker != nil {
						cfg.TicketsHandler.RegisterSSERoutes(protected, cfg.SSEBroker)
					}
				})
			})
		})
	})

	return r
}
