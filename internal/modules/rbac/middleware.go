package rbac

import (
	"context"
	"net/http"

	"github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// Resolver is the part of Service the access middleware needs.
type Resolver interface {
	EnsureUser(ctx context.Context, id Identity) (uuid.UUID, error)
	Grants(ctx context.Context, tenantID, localUserID, authUserID uuid.UUID) (Grants, error)
}

// ResolveAccess builds the request's access.Access after authentication and the tenant guard.
// A user acting in their own tenant is provisioned just in time and gets their ticketing roles
// and permissions. A platform owner acting in another tenant gets no local user there. Platform
// owners, superusers and service keys bypass the permission checks. An API key without a user
// (an app credential) gets the permissions of the role its global roles map to.
func ResolveAccess(res Resolver, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			claims, ok := authclient.ClaimsFromContext(ctx)
			if !ok {
				access.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized", "code": "unauthorized"})
				return
			}
			tenantID, err := uuid.Parse(httpware.GetTenantID(ctx))
			if err != nil || tenantID == uuid.Nil {
				access.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant context required", "code": "tenant_required"})
				return
			}
			a, status := buildAccess(ctx, res, log, claims, tenantID, httpware.GetTenantSlug(ctx))
			if status != 0 {
				access.WriteJSON(w, status, map[string]string{"error": "could not resolve your ticketing access", "code": "access_unavailable"})
				return
			}
			next.ServeHTTP(w, r.WithContext(access.With(ctx, a)))
		})
	}
}

// buildAccess resolves the caller; a non-zero status means the request must be refused.
func buildAccess(ctx context.Context, res Resolver, log *zap.Logger, claims *authclient.Claims, tenantID uuid.UUID, slug string) (*access.Access, int) {
	a := &access.Access{
		TenantID:        tenantID,
		TenantSlug:      slug,
		Email:           claims.Email,
		GlobalRoles:     claims.Roles,
		IsPlatformOwner: claims.IsPlatformOwner,
		IsService:       claims.IsService,
		Bypass:          claims.IsPlatformOwner || claims.IsSuperuser() || claims.IsService,
		Claims:          claims,
	}
	if a.TenantSlug == "" {
		a.TenantSlug = claims.GetTenantSlug()
	}
	authUserID, err := claims.UserID()
	switch {
	case err != nil && claims.IsService:
		// A service key acts for its service, with no user of its own.
	case err != nil:
		role := MapGlobalRoles(claims.Roles)
		a.Roles = []string{role}
		a.Perms = PermissionsForRoles(a.Roles)
	default:
		a.AuthUserID = authUserID
		if claims.TenantID == tenantID.String() {
			local, err := res.EnsureUser(ctx, Identity{
				TenantID: tenantID, AuthUserID: authUserID, Email: claims.Email, GlobalRoles: claims.Roles,
			})
			if err != nil {
				log.Error("ticketing user provisioning failed", zap.String("auth_user_id", authUserID.String()), zap.Error(err))
				if !a.Bypass {
					return nil, http.StatusServiceUnavailable
				}
			} else {
				a.LocalUserID = local
				g, err := res.Grants(ctx, tenantID, local, authUserID)
				if err != nil {
					log.Error("ticketing grants failed", zap.String("auth_user_id", authUserID.String()), zap.Error(err))
					if !a.Bypass {
						return nil, http.StatusServiceUnavailable
					}
				}
				a.Roles, a.Perms, a.AgentID = g.Roles, g.Perms, g.AgentID
			}
		}
	}
	if a.Bypass {
		a.Perms = AllCodes()
	}
	return a, 0
}
