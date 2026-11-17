# Sprint 13: AI with Vera

**Status: Built, not deployed.** It replaces the keyword classifier from [sprint 8](sprint-8-routing.md). ticketing-ui sprint 12 consumes it; the contract is [ai.md](../ai.md).

## Tasks

- [x] marketflow-ai calls through shared-service-client (`internal/clients/vera.go`): classify through `/api/v1/internal/triage`, and summarize a thread, suggested reply, KB suggestions for a ticket and KB article draft from a resolved ticket through the generic task route `/api/v1/agent/run`, with ticketing's own prompts and no tools. AI calls are never retried and time out after `AI_TIMEOUT` (30s).
- [x] Prompts built from the tenant's own data only: HTML reduced to plain text, subject 300 characters, body 4000, each message 1500, thread 12,000 and the newest 40 messages, no requester contact. marketflow-ai fences and masks it as data.
- [x] Routes under `/{tenant}/ai`, gated by the existing ticket, comment and KB permissions ([ai.md](../ai.md))
- [x] Remove the keyword `classify` rules and their code (`POST /tickets/classify`, `classifyTicketText`). Routing rules never used them and keep working.
- [x] Every AI call checks the prepaid AI credit wallet in subscriptions-api first and spends one credit once after a usable answer, under a key per request (`Idempotency-Key`), and refuses cleanly at zero balance (402 `ai_credits_exhausted`)
- [x] codevertex is the only exempt tenant, decided by subscriptions-api's credit check (its `exempt` answer). See the note below on `IsGatingExempt`.
- [x] AI failure or a zero balance never blocks the ticket action it decorates: AI runs only from the explicit routes, and auto-classify runs after the create commits, opt-in through `ai.auto_classify` (off by default)
- [x] Tests: the client against a fake server, credit refusal, exemption, idempotency, failed AI not charged, the never-blocking create, tenant isolation of KB candidates

## Note on the exemption check

The sprint first named the shared `IsGatingExempt` check. I did not use it for AI credits: auth-client's `Claims.IsGatingExempt()` also exempts the public demo tenant, `service_charge` and `sub_exempt` tenants, and my rule of 2026-11-12 is that only the platform owner's own tenant runs AI for free. subscriptions-api's credit check (`credits.Coverage` with the AI service and `IsPlatformTenant`) is the shared check for AI, and every spend goes through it, so there is no local exemption list.

## Acceptance criteria

- A suggested reply appears for a tenant with credit and debits the wallet once. Met (`TestSuggestedReplyDebitsOnce`, including a retry under the same key).
- A tenant at zero balance gets no suggestion and the ticket still saves. Met (`TestZeroBalanceHidesSuggestionAndTicketStillSaves`).
- The codevertex tenant is never debited. Met (`TestExemptTenantIsNeverDebited`).

## Fix requirements proved

- **Fix 4, predictable cost.** `TestZeroBalanceHidesSuggestionAndTicketStillSaves`: a zero-balance tenant gets a 402 with no suggestion and no model call, and a ticket created with auto-classify on still saves, with no charge.
- **Flip, AI from the existing wallet.** One credit per AI call from the prepaid AI wallet; no per-resolution metering.

## Left for deployment

- Build, vet and tests; regenerate swagger (`/tickets/classify` removed, `/ai/*` and `/settings/ai` added).
- No schema change and no migration.
- Env: `MARKETFLOW_AI_URL`, `AI_TIMEOUT` and `SUBSCRIPTION_BASE_URL` in the devops values if the cluster defaults differ. The network policy must let ticketing-api reach marketflow-ai (port 4001) and subscription-api (port 4000).
