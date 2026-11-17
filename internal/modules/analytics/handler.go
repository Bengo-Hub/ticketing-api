package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/Bengo-Hub/reports"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
	"github.com/bengobox/ticketing-service/internal/platform/tenantcache"
)

// permAnalyticsView gates every analytics route and export.
const permAnalyticsView = "ticketing.analytics.view"

// Handler serves /{tenant}/analytics.
type Handler struct {
	svc     *Service
	tenants tenantcache.Lookup
	logger  *zap.Logger
	now     func() time.Time
}

// NewHandler creates the analytics handler. tenants gives the tenant's time zone (the default
// for tz), name and brand colour; nil uses UTC and no branding.
func NewHandler(svc *Service, tenants tenantcache.Lookup, logger *zap.Logger) *Handler {
	return &Handler{svc: svc, tenants: tenants, logger: logger.Named("analytics-handler"), now: time.Now}
}

// RegisterRoutes mounts the analytics routes under the protected /{tenant} group.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/analytics", func(a chi.Router) {
		a.Use(access.RequirePermission(permAnalyticsView))
		a.Get("/tickets", h.Tickets)
		a.Get("/agents", h.Agents)
		a.Get("/sla", h.SLA)
		a.Get("/csat", h.CSAT)
		a.Get("/{view}/export", h.Export)
	})
}

// tenant reads the tenant details once per request; failures leave them empty.
func (h *Handler) tenant(ctx context.Context, slug string) sharedcache.TenantDetails {
	if h.tenants == nil || slug == "" {
		return sharedcache.TenantDetails{}
	}
	d, err := h.tenants.GetTenantDetails(ctx, slug)
	if err != nil {
		return sharedcache.TenantDetails{}
	}
	return d
}

// filter parses the request's filter, answering 400 when it is invalid.
func (h *Handler) filter(w http.ResponseWriter, r *http.Request) (Filter, sharedcache.TenantDetails, bool) {
	a := access.From(r.Context())
	if a == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return Filter{}, sharedcache.TenantDetails{}, false
	}
	slug := a.TenantSlug
	if slug == "" {
		slug = chi.URLParam(r, "tenant")
	}
	details := h.tenant(r.Context(), slug)
	f, err := ParseFilter(r.URL.Query(), a.TenantID, details.Timezone, h.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return Filter{}, details, false
	}
	return f, details, true
}

// build runs one view by name.
func (h *Handler) build(ctx context.Context, view string, f Filter) (any, error) {
	switch view {
	case "tickets":
		return h.svc.Tickets(ctx, f)
	case "agents":
		return h.svc.Agents(ctx, f)
	case "sla":
		return h.svc.SLA(ctx, f)
	case "csat":
		return h.svc.CSAT(ctx, f)
	}
	return nil, errUnknownView
}

var errUnknownView = errors.New("unknown analytics view: use tickets, agents, sla or csat")

func (h *Handler) serve(w http.ResponseWriter, r *http.Request, view string) {
	f, _, ok := h.filter(w, r)
	if !ok {
		return
	}
	v, err := h.build(r.Context(), view, f)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// Tickets is GET /analytics/tickets: live stats, period figures against the previous period,
// the trend and breakdowns.
func (h *Handler) Tickets(w http.ResponseWriter, r *http.Request) { h.serve(w, r, "tickets") }

// Agents is GET /analytics/agents: the agent leaderboard.
func (h *Handler) Agents(w http.ResponseWriter, r *http.Request) { h.serve(w, r, "agents") }

// SLA is GET /analytics/sla: compliance, trend and breakdowns.
func (h *Handler) SLA(w http.ResponseWriter, r *http.Request) { h.serve(w, r, "sla") }

// CSAT is GET /analytics/csat: satisfaction, trend, breakdowns and the latest comments.
func (h *Handler) CSAT(w http.ResponseWriter, r *http.Request) { h.serve(w, r, "csat") }

// Export is GET /analytics/{view}/export?format=pdf|csv|xlsx with the same filters as the view.
// The file is built from the same view the JSON route returns, through the shared reports module.
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	view := chi.URLParam(r, "view")
	format, err := reports.ParseFormat(r.URL.Query().Get("format"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, details, ok := h.filter(w, r)
	if !ok {
		return
	}
	v, err := h.build(r.Context(), view, f)
	if err != nil {
		h.fail(w, err)
		return
	}
	brand := Branding{TenantName: details.Name, PrimaryColor: sharedcache.GetTenantBranding(details).PrimaryColor}
	now := h.now()
	var rep *reports.Report
	switch x := v.(type) {
	case *TicketsView:
		rep = TicketsReport(x, brand, now)
	case *AgentsView:
		rep = AgentsReport(x, brand, now)
	case *SLAView:
		rep = SLAReport(x, brand, now)
	case *CSATView:
		rep = CSATReport(x, brand, now)
	}
	body, mime, err := reports.Generate(rep, format)
	if err != nil {
		h.fail(w, err)
		return
	}
	name := fmt.Sprintf("%s-analytics-%s-%s.%s", view, f.From.In(f.Loc).Format("20060102"),
		f.To.Add(-time.Nanosecond).In(f.Loc).Format("20060102"), format.Ext())
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnknownView) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h.logger.Error("analytics failed", zap.Error(err))
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
