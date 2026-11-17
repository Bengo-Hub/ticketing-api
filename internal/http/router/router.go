package router

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
	"go.uber.org/zap"

	_ "github.com/bengobox/ticketing-service/docs"
	"github.com/bengobox/ticketing-service/internal/http/handlers"
	"github.com/bengobox/ticketing-service/internal/http/metrics"
	"github.com/bengobox/ticketing-service/internal/modules/identity"
	"github.com/bengobox/ticketing-service/internal/modules/kb"
	"github.com/bengobox/ticketing-service/internal/modules/rbac"
	"github.com/bengobox/ticketing-service/internal/modules/routing"
	"github.com/bengobox/ticketing-service/internal/modules/settings"
	"github.com/bengobox/ticketing-service/internal/modules/sla"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
	"github.com/bengobox/ticketing-service/internal/platform/access"
	"github.com/bengobox/ticketing-service/internal/platform/sse"
	"github.com/bengobox/ticketing-service/internal/platform/tenantcache"
)

// Config holds all module handlers for the router.
type Config struct {
	Log                  *zap.Logger
	HealthHandler        *handlers.HealthHandler
	AuthMiddleware       *authclient.AuthMiddleware
	Limiter              *ratelimit.Limiter
	AllowedOrigins       []string
	IdentityHandler      *identity.Handler
	TicketsHandler       *tickets.Handler
	PublicTicketsHandler *tickets.PublicHandler
	SLAHandler           *sla.Handler
	KBHandler            *kb.Handler
	RoutingHandler       *routing.Handler
	SettingsHandler      *settings.Handler
	RBACHandler          *rbac.Handler
	AccessResolver       rbac.Resolver
	SSEBroker            *sse.Broker
	TenantResolver       tenantcache.Lookup
	// CRMLinks answers marketflow's "does ticketing still link this contact" check (X-API-Key).
	CRMLinks *tickets.CRMLinksHandler
}

// New constructs the chi router.
//
// The protected /{tenant} group runs the Trinity chain in order: global rate limit, RequireAuth
// (JWT or API key), RequireActiveSubscriptionForMutations, the tenant guard, just-in-time user
// provisioning with access resolution, OutletContext, then each route's RequirePermission.
func New(cfg Config) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// Never chi RealIP: it trusts client-sent True-Client-IP/X-Forwarded-For.
	r.Use(ratelimit.TrustedRealIP)
	r.Use(httpware.RequestID)
	r.Use(httpware.Logging(cfg.Log))
	r.Use(httpware.Recover(cfg.Log))
	r.Use(metrics.Middleware)
	// The public support pages call ticketing-api from the browser with an email code token or a
	// ticket access token, so both headers must pass the CORS preflight.
	r.Use(httpware.CORS(httpware.StandardCORS(cfg.AllowedOrigins, "X-OTP-Token", tickets.TicketAccessHeader)))
	if cfg.Limiter != nil {
		r.Use(cfg.Limiter.Middleware(ratelimit.IPKey, 300, time.Minute))
	}

	r.Get("/healthz", cfg.HealthHandler.Liveness)
	r.Get("/readyz", cfg.HealthHandler.Readiness)
	r.Get("/v1/docs/*", httpSwagger.WrapHandler)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v1/docs/", http.StatusMovedPermanently)
	})

	r.Route("/api/v1", func(v1 chi.Router) {
		// S2S routes: X-API-Key and X-Tenant-ID, no user token.
		if cfg.TicketsHandler != nil {
			cfg.TicketsHandler.RegisterInternalRoutes(v1)
		}
		if cfg.CRMLinks != nil {
			v1.Get("/s2s/{tenant}/crm-links/{contactID}", cfg.CRMLinks.Referenced)
		}

		v1.Group(func(protected chi.Router) {
			protected.Use(cfg.AuthMiddleware.RequireAuth)
			protected.Get("/status", cfg.HealthHandler.Status)
		})

		// One /{tenant} sub-router holds the public routes and, in its own group, the protected
		// ones: chi panics when the same pattern is mounted twice.
		v1.Route("/{tenant}", func(tenant chi.Router) {
			if cfg.PublicTicketsHandler != nil {
				cfg.PublicTicketsHandler.RegisterPublicRoutes(tenant)
			}
			if cfg.KBHandler != nil {
				cfg.KBHandler.RegisterPublicRoutes(tenant)
			}

			tenant.Group(func(p chi.Router) {
				p.Use(cfg.AuthMiddleware.RequireAuth)
				p.Use(authclient.RequireActiveSubscriptionForMutations())
				p.Use(httpware.TenantV2(httpware.TenantConfig{
					ClaimsExtractor: claimsTenant,
					URLParamFunc:    chi.URLParam,
					URLParamName:    "tenant",
				}))
				p.Use(access.TenantGuard(cfg.TenantResolver, cfg.Log))
				p.Use(rbac.ResolveAccess(cfg.AccessResolver, cfg.Log))
				p.Use(access.OutletContext)
				mountProtected(p, cfg)
			})
		})
	})

	return r
}

// claimsTenant feeds the token's tenant to httpware.TenantV2, which refuses an X-Tenant-ID or
// X-Tenant-Slug header naming another tenant.
func claimsTenant(ctx context.Context) (string, string, bool, bool) {
	c, ok := authclient.ClaimsFromContext(ctx)
	if !ok {
		return "", "", false, false
	}
	return c.TenantID, c.GetTenantSlug(), c.IsPlatformOwner, true
}

// mountProtected registers every module's protected routes. Each route carries its own
// permission; branding and auth/me need only a signed-in caller in the tenant.
func mountProtected(p chi.Router, cfg Config) {
	if cfg.TenantResolver != nil {
		p.Get("/branding", func(w http.ResponseWriter, r *http.Request) {
			details, err := cfg.TenantResolver.GetTenantDetails(r.Context(), chi.URLParam(r, "tenant"))
			if err != nil {
				http.Error(w, `{"error":"tenant not found"}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(details)
		})
	}
	if cfg.RBACHandler != nil {
		cfg.RBACHandler.RegisterRoutes(p)
	}
	if cfg.IdentityHandler != nil {
		cfg.IdentityHandler.RegisterRoutes(p)
	}
	if cfg.TicketsHandler != nil {
		cfg.TicketsHandler.RegisterRoutes(p)
		if cfg.SSEBroker != nil {
			cfg.TicketsHandler.RegisterSSERoutes(p, cfg.SSEBroker)
		}
	}
	if cfg.SLAHandler != nil {
		cfg.SLAHandler.RegisterRoutes(p)
	}
	if cfg.KBHandler != nil {
		cfg.KBHandler.RegisterRoutes(p)
	}
	if cfg.RoutingHandler != nil {
		cfg.RoutingHandler.RegisterRoutes(p)
	}
	if cfg.SettingsHandler != nil {
		cfg.SettingsHandler.RegisterRoutes(p)
	}
}
