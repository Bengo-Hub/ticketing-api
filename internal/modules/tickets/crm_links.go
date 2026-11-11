package tickets

import (
	"context"
	"net/http"

	"github.com/Bengo-Hub/httpware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/ent"
	entticket "github.com/bengobox/ticketing-service/internal/ent/ticket"
	"github.com/bengobox/ticketing-service/internal/platform/tenantcache"
)

// CRMLinksHandler tells marketflow whether ticketing still links a CRM contact, so marketflow can
// decide between deleting an erased person's contact and stripping it to a minimal record.
//
// Ticketing keeps no tenant table, so it cannot check a tenant UUID: a UUID is accepted as given,
// and a slug is resolved through auth-api (an unknown one answers 404). The contact id is matched
// in every tenant: CRM contact ids are random UUIDs minted by marketflow, so a match anywhere is
// this person, and tickets held under a mismatched tenant id can only answer "referenced", which
// keeps the contact.
type CRMLinksHandler struct {
	referenced func(ctx context.Context, contactID uuid.UUID) (bool, error)
	tenants    tenantcache.Lookup
	key        string
	log        *zap.Logger
}

// NewCRMLinksHandler builds the handler over the tickets table.
func NewCRMLinksHandler(db *ent.Client, tenants tenantcache.Lookup, key string, log *zap.Logger) *CRMLinksHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &CRMLinksHandler{
		referenced: func(ctx context.Context, contactID uuid.UUID) (bool, error) {
			return db.Ticket.Query().Where(entticket.CrmContactID(contactID)).Exist(ctx)
		},
		tenants: tenants,
		key:     key,
		log:     log,
	}
}

// Referenced answers whether any ticket, in any tenant, carries the contact id. The answer is the
// flag only: no ticket numbers, names or ids.
// GET /api/v1/s2s/{tenant}/crm-links/{contactID} (X-API-Key) -> {"referenced": bool}
func (h *CRMLinksHandler) Referenced(w http.ResponseWriter, r *http.Request) {
	if !httpware.ValidServiceKey(r, h.key) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	contactID, err := uuid.Parse(chi.URLParam(r, "contactID"))
	if err != nil || contactID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "contact id must be a UUID")
		return
	}
	tenantID, ok := tenantcache.PublicTenantID(r.Context(), h.tenants, chi.URLParam(r, "tenant"))
	if !ok {
		respondError(w, http.StatusNotFound, "tenant not found")
		return
	}
	found, err := h.referenced(r.Context(), contactID)
	if err != nil {
		// Ids only in the log, never the person's details.
		h.log.Error("crm links check failed", zap.String("tenant_id", tenantID.String()),
			zap.String("contact_id", contactID.String()), zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to check links")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"referenced": found})
}
