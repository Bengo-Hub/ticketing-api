package access

import (
	"context"
	"net/http"
	"strings"

	"github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/tenantcache"
)

// TenantGuard resolves the tenant a protected request acts in and refuses a request whose
// {tenant} URL segment names a different tenant than the caller's token.
//
// The segment may be a slug or a UUID; a slug resolves through tenantcache (Redis, then auth-api).
// A platform owner is not bound to a token tenant: ?tenantId=<uuid> selects the tenant, otherwise
// the URL segment does. Everyone else acts only in their token's tenant. The resolved tenant is
// stored with httpware.WithTenantID and WithTenantSlug for the rest of the chain.
func TenantGuard(lookup tenantcache.Lookup, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := authclient.ClaimsFromContext(r.Context())
			if !ok {
				WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized", "code": "unauthorized"})
				return
			}
			segment := chi.URLParam(r, "tenant")
			override := r.URL.Query().Get("tenantId")
			tenantID, status, msg := ResolveTenant(r.Context(), lookup, claims, segment, override)
			if status != 0 {
				if status == http.StatusForbidden && log != nil {
					log.Warn("tenant mismatch refused",
						zap.String("segment", segment), zap.String("claimed", claims.TenantID),
						zap.String("subject", claims.Subject), zap.String("path", r.URL.Path))
				}
				WriteJSON(w, status, map[string]string{"error": msg, "code": "tenant_mismatch"})
				return
			}
			ctx := httpware.WithTenantID(r.Context(), tenantID.String())
			// The URL slug names the tenant unless a platform owner chose another by ?tenantId=.
			if segment != "" && (override == "" || !claims.IsPlatformOwner) {
				if _, err := uuid.Parse(segment); err != nil {
					ctx = httpware.WithTenantSlug(ctx, segment)
				}
			}
			if claims.IsPlatformOwner {
				ctx = context.WithValue(ctx, httpware.IsPlatformOwnerKey, true)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ResolveTenant returns the tenant a request acts in, or a non-zero HTTP status and message when
// it must be refused. override is the ?tenantId= value, honoured only for platform owners.
func ResolveTenant(ctx context.Context, lookup tenantcache.Lookup, claims *authclient.Claims, segment, override string) (uuid.UUID, int, string) {
	if claims.IsPlatformOwner {
		if override != "" {
			id, err := uuid.Parse(override)
			if err != nil || id == uuid.Nil {
				return uuid.Nil, http.StatusBadRequest, "tenantId must be a tenant UUID"
			}
			return id, 0, ""
		}
		if id, ok := tenantcache.PublicTenantID(ctx, lookup, segment); ok {
			return id, 0, ""
		}
		if claimed, err := uuid.Parse(claims.TenantID); err == nil && segment == "" {
			return claimed, 0, ""
		}
		return uuid.Nil, http.StatusNotFound, "tenant not found"
	}

	claimed, err := uuid.Parse(claims.TenantID)
	if err != nil || claimed == uuid.Nil {
		return uuid.Nil, http.StatusForbidden, "the signed-in session has no organization"
	}
	if segment == "" {
		return claimed, 0, ""
	}
	if id, ok := tenantcache.PublicTenantID(ctx, lookup, segment); ok {
		if id != claimed {
			return uuid.Nil, http.StatusForbidden, "tenant does not match the signed-in organization"
		}
		return claimed, 0, ""
	}
	// The slug could not be resolved (unknown, or the tenant cache and auth-api are unreachable).
	// The token's own slug still identifies the caller's tenant.
	if slug := claims.GetTenantSlug(); slug != "" && strings.EqualFold(slug, segment) {
		return claimed, 0, ""
	}
	return uuid.Nil, http.StatusForbidden, "tenant does not match the signed-in organization"
}
