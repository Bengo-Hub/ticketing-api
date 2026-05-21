# Sprint 4 — Public API & OTP Verification

**Duration**: Week 5  
**Status**: 🔲 Pending

---

## Overview

Adds an unauthenticated public API for end-customers to submit tickets, track status, and verify their identity via OTP — enabling Vera AI widget and customer-facing portals to create tickets without requiring full SSO login.

---

## Deliverables

### 1. Public Ticket Submission
- `POST /api/v1/{tenant}/public/tickets` — create ticket (no JWT required)
  - Requires OTP verification header: `X-OTP-Token: <verified-token>`
  - Fields: `name`, `email`, `subject`, `description`, `category_id`, `channel`, `order_id` (optional)
  - Returns `{ ticket_number, ticket_id, status }`
- `GET /api/v1/{tenant}/public/tickets/{ticketNumber}` — check status by ticket number (no auth)
- `POST /api/v1/{tenant}/public/tickets/{ticketNumber}/comments` — customer reply (OTP verified)

### 2. OTP Verification Flow
Delegates to auth-api OTP endpoints. Ticketing validates the OTP token returned by auth-api before accepting a public ticket submission.

OTP flow:
1. Customer calls `POST /api/v1/auth/otp/send` on auth-api → receives SMS/email code
2. Customer calls `POST /api/v1/auth/otp/verify` → receives short-lived `otp_token`
3. Customer includes `X-OTP-Token` header on public ticket endpoint
4. Ticketing validates token with auth-api: `GET /api/v1/auth/otp/validate?token=<token>`

### 3. Public Rate Limiting
- Rate limit by IP: 5 ticket creations per hour per IP
- Rate limit by email: 10 tickets per 24h per email
- Use Redis INCR + expiry for counters

### 4. Public Router Group
New unauthenticated route group in `router.go`:
```go
v1.Route("/{tenant}/public", func(pub chi.Router) {
    pub.Use(middleware.ThrottleBacklog(100, 50, 2*time.Second))
    pub.Post("/tickets", cfg.TicketsHandler.CreatePublicTicket)
    pub.Get("/tickets/{ticketNumber}", cfg.TicketsHandler.GetPublicTicket)
    pub.Post("/tickets/{ticketNumber}/comments", cfg.TicketsHandler.AddPublicComment)
})
```

### 5. Branding Endpoint (public, no auth)
- `GET /api/v1/{tenant}/branding` — tenant branding from cache/auth-api
  - ✅ Already implemented in Sprint 2 as part of shared-cache integration
  - Used by widget and customer portal for theming

---

## Definition of Done

- [ ] Public ticket create endpoint with OTP validation
- [ ] Public ticket status endpoint (no auth)
- [ ] Rate limiting by IP and email
- [ ] OTP token validation via auth-api
- [ ] Go build clean, no JWT middleware on public routes

---

## Next Sprint

**Sprint 5**: Knowledge Base — articles, tagging, search, AI-suggested articles on ticket create.
