# Sprint 8: Routing and Classification

**Status: Partial.** Routing rules exist and the first matching rule applies on create. Load-based assignment and ticket counts are not wired. AI classification came in sprint 13.

## Tasks

- [x] `POST/GET /api/v1/{tenant}/routing/rules`, `GET/PUT/DELETE /routing/rules/{ruleId}` (`ticketing.routing.*`)
- [x] Rules evaluated in `sort_order` on create; the first match applies `assign_team`, `assign_agent` or `set_priority`
- [x] `POST /tickets/classify` returned a priority and category hint from keyword rules (removed in sprint 13)
- [ ] A rule test endpoint
- [ ] Least-loaded agent fallback when no rule matches. `GetLeastLoadedAgent` exists in the identity module and the ticket create does not call it.
- [ ] `current_ticket_count` kept on assign and resolve. `IncrementAgentTicketCount` and `DecrementAgentTicketCount` exist and nothing calls them.
- [ ] Escalation (sprint 10)
- [x] Classification through marketflow-ai in place of the keyword rules: `POST /ai/classify` (sprint 13, [ai.md](../ai.md))

## Acceptance criteria

- A rule matching `priority=urgent` assigns its team on create. Met.
- An unmatched ticket goes to the least-loaded available agent. Not met.
