# Sprint 1: Identity and Access

**Status: Done.** Teams, agents and just-in-time provisioning were built first. Local Trinity RBAC, the tenant guard and `auth/me` landed in 4f03d8e and are live in production, with the 108 permission codes seeded.

## Tasks

### Teams and agents

- [x] Teams: `POST/GET /api/v1/{tenant}/teams`, `GET/PUT/DELETE /teams/{teamId}` (`ticketing.teams.*`)
- [x] Agents: create, list (filters `teamId`, `status`, `role`, `active`), `GET /agents/available`, `GET /agents/{agentId}`, `GET /agents/user/{userId}`, update, `PUT /agents/{agentId}/status` (`agents.change` or `agents.change_own`), delete
- [x] Agents unique per `(tenant_id, user_id)`, so one person can be an agent in several tenants (4f03d8e)

### Authorization (4f03d8e)

- [x] Chain on every protected route: `RequireAuth` with API keys on by default, `RequireActiveSubscriptionForMutations`, `httpware.TenantV2`, the tenant guard (platform owners may use `?tenantId=`), `rbac.ResolveAccess`, `OutletContext`, `RequirePermission`
- [x] Permission catalogue `ticketing.{module}.{action}`: 12 modules by 9 actions, upserted on start
- [x] Implication rules: `manage` grants the module, `manage_own` grants every `_own` action, an action grants its `_own` variant
- [x] System roles `ticketing_admin`, `supervisor`, `agent`, `customer`, seeded per tenant on first sign-in
- [x] Just-in-time `TicketingUser` with the role mapped from global roles, and an agent profile for staff roles
- [x] `auth.user.created` and `auth.user.updated` on a queue subscription, with the same rules as sign-in
- [x] `GET /api/v1/{tenant}/ticketing/auth/me`
- [x] Role administration: `GET /ticketing/roles`, `GET /ticketing/users`, `PUT /ticketing/users/{userId}/roles`
- [x] Customers see and change only their own tickets and never see internal notes

### Not in this sprint

- Keyset pagination moved to sprint 9. The agent and team lists page by keyset since 2026-11-17.
- `auth.user.deleted` handling is part of the integration review.

## Acceptance criteria

- A token for tenant A calling `/api/v1/{tenant-B-slug}/tickets` gets 403 `tenant_mismatch`. Met, covered by `access_test.go`.
- A first sign-in by a `manager` creates a `supervisor` ticketing user with an agent profile. Met, covered by `rbac_test.go`.
- A customer cannot read another customer's ticket or any internal note. Met in the handlers.
