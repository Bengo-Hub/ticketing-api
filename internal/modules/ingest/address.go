package ingest

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Bengo-Hub/httpware/contact"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/platform/tenantcache"
)

// Addresses maps support mail recipients to tenants: the platform's own support address belongs
// to the platform tenant, and <slug>@<SupportDomain> to the tenant with that slug.
type Addresses struct {
	// SupportDomain is the catch-all forwarding domain (support.codevertexafrica.com).
	SupportDomain string
	// PlatformAddress is the platform tenant's own support mailbox (support@codevertexafrica.com).
	PlatformAddress string
	// PlatformSlug is the platform tenant's slug (codevertex).
	PlatformSlug string
}

// ErrUnknownRecipient is mail to an address that is neither the platform address nor on the
// support domain.
var ErrUnknownRecipient = errors.New("ingest: recipient is not a support address")

// ErrUnknownTenant is mail to <slug>@<support domain> for a slug auth-api does not know.
var ErrUnknownTenant = errors.New("ingest: no tenant for this support address")

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// SlugFor returns the tenant slug a recipient address names, or false when it is not a support
// address. A plus tag (hadia+billing@...) is ignored.
func (a Addresses) SlugFor(recipient string) (string, bool) {
	addr, err := contact.NormalizeEmail(recipient)
	if err != nil {
		return "", false
	}
	if a.PlatformAddress != "" && addr == strings.ToLower(strings.TrimSpace(a.PlatformAddress)) {
		return a.PlatformSlug, a.PlatformSlug != ""
	}
	local, domain, _ := strings.Cut(addr, "@")
	if a.SupportDomain == "" || domain != strings.ToLower(a.SupportDomain) {
		return "", false
	}
	local, _, _ = strings.Cut(local, "+")
	if !slugPattern.MatchString(local) {
		return "", false
	}
	return local, true
}

// ForwardingAddress is the address a tenant forwards its support mailbox to.
func (a Addresses) ForwardingAddress(slug string) string {
	if slug == "" {
		return ""
	}
	if slug == a.PlatformSlug && a.PlatformAddress != "" {
		return a.PlatformAddress
	}
	if a.SupportDomain == "" {
		return ""
	}
	return slug + "@" + a.SupportDomain
}

// IsOwn reports whether an address is one of ours (the platform address or anything on the
// support domain), so our own mail is never ingested in a loop.
func (a Addresses) IsOwn(address string) bool {
	addr, err := contact.NormalizeEmail(address)
	if err != nil {
		return false
	}
	if a.PlatformAddress != "" && addr == strings.ToLower(strings.TrimSpace(a.PlatformAddress)) {
		return true
	}
	_, domain, _ := strings.Cut(addr, "@")
	return a.SupportDomain != "" && domain == strings.ToLower(a.SupportDomain)
}

// TenantResolver turns support addresses into tenant ids through the tenant cache (Redis, then
// auth-api).
type TenantResolver struct {
	addrs  Addresses
	lookup tenantcache.Lookup
}

// NewTenantResolver creates a resolver.
func NewTenantResolver(addrs Addresses, lookup tenantcache.Lookup) *TenantResolver {
	return &TenantResolver{addrs: addrs, lookup: lookup}
}

// Resolve returns the tenant of the first support address among the candidates (the envelope
// recipient first, then To and Cc). An address that is not ours and a slug auth-api does not
// know are permanent errors; an auth-api outage is not.
func (r *TenantResolver) Resolve(ctx context.Context, candidates ...string) (uuid.UUID, string, error) {
	slug := ""
	for _, c := range candidates {
		if s, ok := r.addrs.SlugFor(c); ok {
			slug = s
			break
		}
	}
	if slug == "" {
		return uuid.Nil, "", Permanent(ErrUnknownRecipient)
	}
	if r.lookup == nil {
		return uuid.Nil, slug, errors.New("ingest: tenant lookup not configured")
	}
	d, err := r.lookup.GetTenantDetails(ctx, slug)
	if err != nil {
		// The shared cache reports an unknown slug as auth-api's 404; anything else is transient.
		if strings.Contains(err.Error(), "not found") {
			return uuid.Nil, slug, Permanent(fmt.Errorf("%w: %s", ErrUnknownTenant, slug))
		}
		return uuid.Nil, slug, fmt.Errorf("resolve tenant %s: %w", slug, err)
	}
	id, err := uuid.Parse(d.ID)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, slug, Permanent(fmt.Errorf("%w: %s", ErrUnknownTenant, slug))
	}
	return id, slug, nil
}
