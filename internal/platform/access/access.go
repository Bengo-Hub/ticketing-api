// Package access carries the resolved caller of a protected request (tenant, user, ticketing
// roles and permissions) and the route gates that check it. The rbac module fills it in; every
// other module only reads it.
package access

import (
	"context"
	"strings"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"
)

// Access is everything a handler needs to authorise and scope a request.
type Access struct {
	// TenantID is the tenant the request acts in: the token's tenant, or for a platform owner the
	// tenant named by ?tenantId= or the URL.
	TenantID   uuid.UUID
	TenantSlug string
	// AuthUserID is the auth-api user id (uuid.Nil for a service key).
	AuthUserID uuid.UUID
	// LocalUserID is the TicketingUser row id (uuid.Nil when none was provisioned).
	LocalUserID uuid.UUID
	Email       string
	Name        string
	GlobalRoles []string
	// Roles are the caller's ticketing role codes in the tenant.
	Roles []string
	// Perms are the caller's ticketing permission codes in the tenant.
	Perms []string
	// AgentID is the caller's agent profile in the tenant, when they have one.
	AgentID         *uuid.UUID
	IsPlatformOwner bool
	IsService       bool
	// Bypass skips Layer 3 permission checks: platform owners, superusers and service keys.
	Bypass bool
	Claims *authclient.Claims
}

type accessKey struct{}

// From returns the request's Access, or nil when the access middleware did not run.
func From(ctx context.Context) *Access {
	a, _ := ctx.Value(accessKey{}).(*Access)
	return a
}

// With returns ctx carrying a.
func With(ctx context.Context, a *Access) context.Context {
	return context.WithValue(ctx, accessKey{}, a)
}

// Has reports whether the caller holds any of the permission codes, directly or through an
// implying code (see Satisfies).
func (a *Access) Has(codes ...string) bool {
	if a == nil {
		return false
	}
	if a.Bypass {
		return true
	}
	for _, need := range codes {
		for _, held := range a.Perms {
			if Satisfies(held, need) {
				return true
			}
		}
	}
	return false
}

// HasRole reports whether the caller holds a ticketing role.
func (a *Access) HasRole(code string) bool {
	if a == nil {
		return false
	}
	for _, r := range a.Roles {
		if r == code {
			return true
		}
	}
	return false
}

// Satisfies reports whether holding permission `held` grants `need`. Codes are
// ticketing.{module}.{action}. Within one module, manage grants every action, manage_own grants
// every *_own action, and an action grants its own variant (view grants view_own).
func Satisfies(held, need string) bool {
	if held == need {
		return true
	}
	hm, ha, ok1 := split(held)
	nm, na, ok2 := split(need)
	if !ok1 || !ok2 || hm != nm {
		return false
	}
	switch {
	case ha == "manage":
		return true
	case strings.HasSuffix(na, "_own") && (ha == "manage_own" || ha == strings.TrimSuffix(na, "_own")):
		return true
	}
	return false
}

// split returns the module and action of a permission code.
func split(code string) (module, action string, ok bool) {
	i := strings.LastIndexByte(code, '.')
	if i <= 0 || i == len(code)-1 {
		return "", "", false
	}
	return code[:i], code[i+1:], true
}
