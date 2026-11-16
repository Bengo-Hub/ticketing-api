# Sprint 13: AI with Vera

**Status: Pending.** It replaces the keyword classifier from [sprint 8](sprint-8-routing.md). ticketing-ui sprint 12 consumes it.

## Tasks

- [ ] marketflow-ai calls through shared-service-client: classify, summarize a thread, suggested reply, KB suggestions for a ticket, KB article draft from a resolved ticket
- [ ] Remove the keyword `classify` rules once the marketflow-ai call is live
- [ ] Every AI call debits the prepaid AI credit wallet in subscriptions-api and refuses cleanly at zero balance
- [ ] codevertex is the only exempt tenant, through the shared `IsGatingExempt` check
- [ ] AI failure or a zero balance never blocks the ticket action it decorates

## Acceptance criteria

- A suggested reply appears for a tenant with credit and debits the wallet once.
- A tenant at zero balance gets no suggestion and the ticket still saves.
- The codevertex tenant is never debited.

## Fix requirements proved

- **Fix 4, predictable cost.** A unit test on a zero-balance tenant shows the AI suggestion hidden and the ticket saved.
- **Flip, AI from the existing wallet.** No per-resolution metering.
