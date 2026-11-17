# AI with Vera

Last updated 2026-11-17 (sprint 13).

ticketing-api offers five AI features to agents: classify a draft, summarize a thread, suggest a reply, suggest knowledge base articles, and draft an article from a resolved ticket. A tenant can also turn on auto-classify for new tickets. This page is the contract for ticketing-ui.

## How it works

- **Models run in marketflow-ai (Vera).** ticketing-api builds the prompts from the tenant's own ticket and knowledge base data and never names a model; marketflow-ai's router picks the model from its provider settings.
- **marketflow-ai's guardrails apply to every call.** It fences ticket text as data, masks emails and phone numbers before a model sees them, flags a ticket that carries an injection phrase (`injected`, confidence capped at 0.8, review always required), and checks every generated answer for figures and commitments that are not in the instructions or the ticket data (`review_notes`, advice only). ticketing-api adds no screening of its own; it sends the ticket as data and passes these outcomes through.
- **Every call is paid from the tenant's prepaid AI credit wallet** in subscriptions-api. One call costs one credit. The wallet is checked before the model runs and spent once after a usable answer. An answer that failed costs nothing. There is no per-resolution metering.
- **Only the platform owner (codevertex) is exempt.** subscriptions-api decides it on every check and spend; ticketing-api has no exemption list of its own. The public demo tenant pays like any other tenant, so its public password cannot run AI for free.
- **AI never blocks a ticket action.** These routes are separate calls the UI makes beside the ticket form or view. Ticket create and update never call AI. Auto-classify runs after the create has committed, on its own goroutine.

## Common rules

All routes are under the signed-in tenant, `POST /api/v1/{tenant}/ai/...`, with the usual bearer token. Customers never reach them: every route needs `ticketing.tickets.view`, which customers do not hold.

**Idempotency-Key (optional request header).** 8 to 100 characters from `A-Z a-z 0-9 . _ : -`. Send a new key for each click of an AI button and the same key when retrying that click. A repeat of a key returns the same answer with no second model call and no second charge (`credits.replayed: true`). Without the header every request is new.

**`credits` object** in every answer:

| Field | Meaning |
|---|---|
| `charged` | Credits spent by this request: 1, or 0 for the platform owner and for a repeated key |
| `exempt` | true for the platform owner |
| `replayed` | true when the request key was already answered or already paid |

**`run` object** in the summary, reply, KB suggestion and KB draft answers:

| Field | Meaning |
|---|---|
| `stopped_early` | true when marketflow-ai stopped the run before it finished, so the text may be partial |
| `stop_reason` | Why it stopped, for example `token_cap_reached`; empty otherwise |

**Review fields**, top level in the summary, reply, KB suggestion and KB draft answers, passed through from marketflow-ai as they came and kept with the answer for a replayed key:

| Field | Meaning |
|---|---|
| `review_notes` | List of strings, left out when empty. marketflow-ai's figure and commitment checks of the text against ticketing's instructions and the ticket data, for example an amount or a refund promise the ticket never mentioned. Advice only: nothing is blocked. The UI shows them beside the text so the agent checks those points before using it |
| `tainted` | true when the run read text carrying an injection phrase, so the text deserves extra care |

**Errors.** Every error body is `{"error": "<message>", "code": "<code>"}`.

| Status | `code` | When | What the UI does |
|---|---|---|---|
| 402 | `ai_credits_exhausted` | The wallet refused: no credit left, no AI subscription, or AI not activated. The body also has `reason`: `insufficient_credits`, `no_subscription` or `service_not_activated` | Hide the suggestion, show a short "Top up AI credits" hint, keep the ticket form working |
| 503 | `ai_credits_unknown` | subscriptions-api could not be read, so the AI did not run | Hide the suggestion; the agent may try again |
| 503 | `ai_unavailable` | marketflow-ai gave no usable answer (down, timed out, or an answer it could not complete). Nothing was charged | Hide the suggestion; the agent may try again |
| 400 | `invalid_request` | Bad body, bad ticket id, or a classify draft with neither subject nor description | |
| 404 | `ticket_not_found` | The ticket does not exist in this tenant | |
| 409 | `ticket_not_resolved` | A KB draft from a ticket that is not resolved or closed | |
| 401, 403 | `unauthorized`, `forbidden` | Not signed in, or missing the permission | |

## Classify a draft

`POST /api/v1/{tenant}/ai/classify`, permissions `ticketing.tickets.view` and `ticketing.tickets.add`.

Request:

```json
{ "subject": "Charged twice for my order", "description": "<p>My card was charged twice</p>", "channel": "web" }
```

`subject` or `description` is required; `channel` is optional and only feeds the routing rules. HTML is reduced to plain text, the subject to 300 characters and the description to 4000.

Response 200:

```json
{
  "label": "billing",
  "category": { "id": "5b1f...", "name": "Billing and payments" },
  "priority": "high",
  "team": { "id": "9c2e..." },
  "confidence": 0.8,
  "reasoning": "The customer reports a duplicate card charge.",
  "requires_human_review": true,
  "injected": false,
  "credits": { "charged": 1, "exempt": false, "replayed": false }
}
```

| Field | Meaning |
|---|---|
| `label` | marketflow-ai's label: `billing`, `technical`, `product` or `general` |
| `category` | The tenant's active category matching the label (same name, a name containing the label, or a name word that starts it, such as Tech for technical), or null |
| `priority` | `low`, `normal`, `high` or `urgent` |
| `team` | The team the tenant's routing rules would assign for this category and priority, or null when no rule assigns a team |
| `confidence` | 0 to 1. At most 0.8 for a draft carrying an injection phrase |
| `reasoning` | One sentence from the model |
| `requires_human_review` | true below 0.85 and always when `injected`; the UI should show the suggestion as a suggestion only |
| `injected` | true when marketflow-ai found an injection phrase in the draft (someone wrote instructions to the AI). Confidence is then at most 0.8. The UI should mark the suggestion as untrusted |

The UI fills the form fields only when the agent accepts the suggestion. Nothing is saved by this route.

## Summarize a ticket

`POST /api/v1/{tenant}/ai/tickets/{ticketId}/summary`, permission `ticketing.tickets.view`. No body.

The summary reads the ticket and its newest 40 messages, internal notes included, up to 12,000 characters of thread. System entries are left out.

Response 200:

```json
{
  "ticket_id": "1d3a...",
  "summary": "Customer was charged twice for order 1043...",
  "run": { "stopped_early": false },
  "review_notes": ["The draft states KES 500, which is not in the ticket"],
  "tainted": false,
  "credits": { "charged": 1, "exempt": false, "replayed": false }
}
```

## Suggest a reply

`POST /api/v1/{tenant}/ai/tickets/{ticketId}/reply`, permissions `ticketing.tickets.view` and `ticketing.comments.add`. No body.

The draft answers the customer's latest public message and reads only the public thread: internal notes never reach a customer-facing draft. It is never sent. The UI puts it in the reply editor; the agent edits it and posts it as a normal comment. Where the draft needs a figure or a promise that is not in the thread, it leaves a placeholder in square brackets for the agent.

Response 200:

```json
{
  "ticket_id": "1d3a...",
  "reply": "Hello Jane, thank you for letting us know...",
  "run": { "stopped_early": false },
  "review_notes": ["The draft states KES 500, which is not in the ticket"],
  "tainted": false,
  "credits": { "charged": 1, "exempt": false, "replayed": false }
}
```

## Knowledge base suggestions

`POST /api/v1/{tenant}/ai/tickets/{ticketId}/kb-suggestions`, permissions `ticketing.tickets.view` and `ticketing.kb.view`. No body.

Candidates are up to 20 of the tenant's own published articles: matches for the subject's longer words first, then the most recent. The model ranks them and only ids from that list are kept, at most 5. When the tenant has no published article the list is empty, no model runs and nothing is spent.

Response 200:

```json
{
  "ticket_id": "1d3a...",
  "suggestions": [
    {
      "article_id": "77aa...",
      "title": "Clearing a printer jam",
      "slug": "clearing-a-printer-jam",
      "excerpt": "Open the tray and...",
      "is_public": true,
      "reason": "Same jam on the invoice printer.",
      "rank": 1
    }
  ],
  "run": { "stopped_early": false },
  "review_notes": ["The draft states KES 500, which is not in the ticket"],
  "tainted": false,
  "credits": { "charged": 1, "exempt": false, "replayed": false }
}
```

`is_public` tells the agent whether the article can be linked for the customer; a private article is for the agent only.

## Article draft from a resolved ticket

`POST /api/v1/{tenant}/ai/tickets/{ticketId}/kb-draft`, permissions `ticketing.tickets.view` and `ticketing.kb.add`. No body. The ticket must be `resolved` or `closed` (409 `ticket_not_resolved` otherwise, before any credit check).

The model writes a general how-to from the public thread, without names, contact details or order numbers. It is saved as a knowledge base article with `status: draft` and `is_public: false`, and metadata `source: ai_draft`, `ticket_id` and `ticket_number`. It is never published by this route; a person reviews it and publishes it in the KB editor. A repeat of the request key returns the same draft instead of saving a second one.

Response 201:

```json
{
  "ticket_id": "1d3a...",
  "article": { "id": "a1b2...", "title": "Clearing a printer jam", "slug": "clearing-a-printer-jam", "status": "draft", "is_public": false, "...": "..." },
  "run": { "stopped_early": false },
  "review_notes": ["The draft states KES 500, which is not in the ticket"],
  "tainted": false,
  "credits": { "charged": 1, "exempt": false, "replayed": false }
}
```

`article` has the same shape as the KB article routes return (snake_case JSON tags on kb.Article since 2026-11-17).

## Auto-classify new tickets

Off by default. A tenant turns it on in its settings:

- `GET /api/v1/{tenant}/settings/ai` returns `{"auto_classify": false}`.
- `PUT /api/v1/{tenant}/settings/ai` with `{"auto_classify": true}` saves it and returns the settings. Both need `ticketing.settings.manage`.

It is stored in the tenant settings document under `ai.auto_classify`, so no migration was needed.

When it is on, every new ticket, from any create path (signed in, public form, internal S2S, email and WhatsApp ingest), is classified after the create has committed:

- It runs on its own goroutine with a 45 second limit; at most 4 run at once on a pod, and a create beyond that is not classified.
- It spends one credit per ticket under a key made from the ticket id, so a ticket is never charged twice. A tenant at zero balance gets nothing and spends nothing; the ticket is already saved.
- It fills the ticket's category only when the ticket still has none, the ticket is not `injected`, and the classification needs no review (confidence 0.85 or more).
- It publishes the SSE event `ticket.ai_classified` with `{"ticket_id": "...", "classification": <the classify response>}`, so an open desk can show the suggestion. Priority and team are only suggested there, never changed.

## What was removed

The keyword classifier (`POST /tickets/classify` with `suggested_priority` and `suggested_category_hint`) is gone; `POST /ai/classify` replaces it. The routing rules engine never used it and is unchanged; its own `subject_contains` and `description_contains` conditions still work.
