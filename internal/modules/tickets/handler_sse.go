package tickets

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// SSEBroker is the subset of *sse.Broker used by the handler.
type SSEBroker interface {
	ServeStream(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID)
}

// RegisterSSERoutes mounts the Server-Sent Events streams. A stream carries every ticket event of
// the tenant, so it needs the tenant-wide ticket view.
func (h *Handler) RegisterSSERoutes(r chi.Router, broker SSEBroker) {
	serve := func(w http.ResponseWriter, req *http.Request) {
		if a, ok := caller(w, req); ok {
			broker.ServeStream(w, req, a.TenantID)
		}
	}
	view := access.RequirePermission(permTicketsView)
	r.With(view).Get("/tickets/stream", serve)
	r.With(view).Get("/tickets/{ticketId}/stream", serve)
}
