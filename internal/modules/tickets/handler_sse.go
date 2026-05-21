package tickets

import (
	"net/http"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// SSEBroker is the subset of *sse.Broker used by the handler.
type SSEBroker interface {
	ServeStream(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID)
}

// RegisterSSERoutes mounts Server-Sent Events streaming endpoints.
// Must be called after RegisterRoutes on the same router.
func (h *Handler) RegisterSSERoutes(r chi.Router, broker SSEBroker) {
	// Ticket list stream — fires on any ticket change for the tenant
	r.Get("/tickets/stream", func(w http.ResponseWriter, req *http.Request) {
		claims, ok := authclient.ClaimsFromContext(req.Context())
		if !ok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		tenantID, err := claims.TenantUUID()
		if err != nil || tenantID == nil {
			http.Error(w, `{"error":"tenant ID required"}`, http.StatusBadRequest)
			return
		}
		broker.ServeStream(w, req, *tenantID)
	})

	// Ticket-specific comment stream
	r.Get("/tickets/{ticketId}/stream", func(w http.ResponseWriter, req *http.Request) {
		claims, ok := authclient.ClaimsFromContext(req.Context())
		if !ok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		tenantID, err := claims.TenantUUID()
		if err != nil || tenantID == nil {
			http.Error(w, `{"error":"tenant ID required"}`, http.StatusBadRequest)
			return
		}
		broker.ServeStream(w, req, *tenantID)
	})
}
