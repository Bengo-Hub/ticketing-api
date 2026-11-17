# Sprint 14: User management and role administration

**Status: Built, not deployed.** The code and unit tests are written. Ent generation, the migration for `audit_logs`, the build and the test run are still to do. ticketing-ui codes its team and roles pages against these routes.

I ported treasury-api's user management and role administration into ticketing, with treasury's paths, JSON field names and rules. The route list and the rules are in [architecture.md](../architecture.md) under Role and user administration.

## Tasks

- [x] Role routes: list with member and permission counts, create, customize, clone, rename, delete, read and replace permissions
- [x] Permission catalogue route with a readable name for each code
- [x] User routes: add a user through auth-api's S2S member add, list with held roles (paged), read one, read their current roles
- [x] Grant with an optional end date, and revoke that keeps the ended row so sign-in never gives the role back
- [x] Admin tier on top of `ticketing.rbac.manage` for role edits, revokes and adding users, as in treasury
- [x] No escalation: a non-admin may grant only a role whose permissions they hold; only a platform owner may give an admin-equivalent role
- [x] Self-lockout guard on revokes and permission edits
- [x] System roles cannot be deleted; editing one customizes it, and the seeder leaves a customized role alone
- [x] `audit_logs` table and `GET /rbac/audit-logs`
- [x] Removed `PUT /users/{userId}/roles` and its handler, service and repository code
- [ ] Generate Ent and the versioned migration for `audit_logs` with atlas
- [ ] Run `go build`, `go vet` and `go test`
- [ ] ticketing-ui team and roles pages on these routes

## Acceptance criteria

- An organisation admin creates a role, edits a system role's permissions and sees it marked customized, and the edit survives a pod restart.
- A manager holding `ticketing.rbac.manage` cannot create or edit roles and cannot grant a role wider than their own access.
- Deleting a system role answers 409.
- Adding a user by email calls auth-api once and the person shows in the team list before their first sign-in.
- A revoked role stays revoked after the person signs in again.
- Every change shows in the role change log.

## Tests

`internal/modules/rbac/roles_test.go` and `handler_admin_test.go` cover the rules above over an in-memory repository. `internal/clients/members_test.go` covers the auth-api member call.
