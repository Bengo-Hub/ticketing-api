package routing

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// requestTenant returns the tenant the access middleware resolved for the request.
func requestTenant(w http.ResponseWriter, r *http.Request) (*uuid.UUID, bool) {
	a := access.From(r.Context())
	if a == nil || a.TenantID == uuid.Nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	id := a.TenantID
	return &id, true
}
