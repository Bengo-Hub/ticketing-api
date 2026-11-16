# Sprint 4: Public API and Email Codes

**Status: Done.** Customers without an account open, read and reply to tickets from the public support pages.

## Tasks

- [x] `POST /api/v1/{tenant}/public/tickets` with `X-OTP-Token`, the one-use token from auth-api's public email code
- [x] Token check against auth-api through `internal/clients/otp.go` with the internal service key
- [x] `GET /public/tickets/{ticketNumber}` proves the requester: an OTP token for the requester's email, or a ticket access token
- [x] `POST /public/tickets/{ticketNumber}/access` issues a ticket access token; tokens are signed with a key derived from `INTERNAL_SERVICE_KEY`
- [x] `POST /public/tickets/{ticketNumber}/comments` for customer replies
- [x] Rate limits through shared-ratelimit: 5 creates an hour per IP and 10 a day per verified email
- [x] CORS allows `X-OTP-Token` and the ticket access header
- [x] Public tickets resolve the tenant from the URL, covered by `handler_public_tenant_test.go`

`GET /api/v1/{tenant}/branding` sits inside the protected group and needs a signed-in caller. The public pages read tenant branding from auth-api.

## Acceptance criteria

- A create without a valid email code is refused. Met.
- A reader without proof of the requester's email cannot read the ticket. Met (c0d863b).
