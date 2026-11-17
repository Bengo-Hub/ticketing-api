// Package ai holds ticketing's AI features (sprint 13): classify a draft, summarize a thread,
// suggest a reply, suggest knowledge base articles, and draft an article from a resolved ticket.
//
// Every model call runs in marketflow-ai (Vera); ticketing builds the prompts from the tenant's
// own data only. Every call is paid from the tenant's prepaid AI credit wallet in subscriptions-api:
// checked before the model runs, spent once after a usable answer, under a key that makes a retry
// free. AI is reached only from these explicit routes, or after a ticket create has committed, so
// no ticket action ever waits on it or fails because of it.
package ai

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/clients"
	"github.com/bengobox/ticketing-service/internal/modules/kb"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// Feature names one AI task. It is part of the credit key and the credit history line.
type Feature string

const (
	FeatureClassify      Feature = "classify"
	FeatureAutoClassify  Feature = "auto_classify"
	FeatureSummary       Feature = "summary"
	FeatureReply         Feature = "reply"
	FeatureKBSuggestions Feature = "kb_suggestions"
	FeatureKBDraft       Feature = "kb_draft"
)

// creditsPerCall is the price of one AI call: one credit, whatever the feature.
const creditsPerCall = 1

var (
	// ErrCreditsExhausted is a refusal by the wallet: no credit left, no AI subscription or the
	// service not activated. The CreditError carries the wallet's reason.
	ErrCreditsExhausted = errors.New("ai: not enough AI credits")
	// ErrCreditsUnknown means the wallet could not be read, so the AI did not run (fail closed).
	ErrCreditsUnknown = errors.New("ai: AI credits could not be checked")
	// ErrUnavailable means marketflow-ai produced nothing usable; nothing was charged.
	ErrUnavailable = errors.New("ai: AI is unavailable")
	// ErrNotResolved refuses an article draft from a ticket that is not resolved or closed.
	ErrNotResolved = errors.New("ai: the ticket is not resolved")
	// ErrNoArticles means the tenant has no published article to suggest.
	ErrNoArticles = errors.New("ai: the knowledge base has no published article")
)

// CreditError is a wallet refusal with subscriptions-api's reason (insufficient_credits,
// no_subscription, service_not_activated).
type CreditError struct{ Reason string }

func (e *CreditError) Error() string { return "ai: AI credits refused: " + e.Reason }

// Unwrap lets errors.Is match ErrCreditsExhausted.
func (e *CreditError) Unwrap() error { return ErrCreditsExhausted }

// Model is marketflow-ai as ticketing uses it (clients.VeraClient).
type Model interface {
	Triage(ctx context.Context, in clients.TriageInput) (clients.TriageResult, error)
	RunTask(ctx context.Context, in clients.TaskInput) (clients.TaskResult, error)
}

// Wallet is the prepaid AI credit wallet (clients.CreditsClient).
type Wallet interface {
	Check(ctx context.Context, tenantID uuid.UUID, units int64) (clients.CreditDecision, error)
	Consume(ctx context.Context, tenantID uuid.UUID, units int64, key, description string) (clients.CreditDecision, error)
}

// TicketStore is the part of the tickets service the AI features read and write.
type TicketStore interface {
	GetTicket(ctx context.Context, tenantID, ticketID uuid.UUID) (*tickets.Ticket, error)
	PageComments(ctx context.Context, tenantID, ticketID uuid.UUID, q tickets.CommentQuery) (tickets.Page[*tickets.TicketComment], error)
	ListCategories(ctx context.Context, tenantID uuid.UUID, filter tickets.CategoryFilter) ([]*tickets.TicketCategory, int, error)
	UpdateTicket(ctx context.Context, tenantID, ticketID uuid.UUID, req *tickets.UpdateTicketRequest) error
}

// KBStore is the part of the knowledge base service the AI features use.
type KBStore interface {
	ListArticles(ctx context.Context, filter kb.ArticleFilter) (*kb.ArticleList, error)
	CreateArticle(ctx context.Context, req kb.CreateArticleRequest) (*kb.Article, error)
	DeleteArticle(ctx context.Context, tenantID, id uuid.UUID) error
}

// Settings reads the tenant's ai.auto_classify switch.
type Settings interface {
	AutoClassify(ctx context.Context, tenantID uuid.UUID) (bool, error)
}

// Credits is what one AI answer cost. Charged is 0 for an exempt tenant (the platform owner) and
// for a replayed request key.
type Credits struct {
	Charged  int64 `json:"charged"`
	Exempt   bool  `json:"exempt"`
	Replayed bool  `json:"replayed"`
}

// RunInfo is how marketflow-ai's task run ended. StoppedEarly means the answer may be partial.
type RunInfo struct {
	StoppedEarly bool   `json:"stopped_early"`
	StopReason   string `json:"stop_reason,omitempty"`
}

// Review is marketflow-ai's own check of a generated answer, passed through as it came. ReviewNotes
// are its figure and commitment findings against the instructions and the ticket data: advice the
// agent reads before using the text, never a block. Tainted is true when the run read text that
// carries an injection phrase. Both are flattened into the answer as review_notes and tainted.
type Review struct {
	ReviewNotes []string `json:"review_notes,omitempty"`
	Tainted     bool     `json:"tainted"`
}

func reviewOf(res clients.TaskResult) Review {
	return Review{ReviewNotes: res.ReviewNotes, Tainted: res.Tainted}
}

// ClassifyRequest is a draft ticket to classify.
type ClassifyRequest struct {
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Channel     string `json:"channel,omitempty"`
}

// CategoryRef names one of the tenant's categories.
type CategoryRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// TeamRef is the team the tenant's routing rules would assign.
type TeamRef struct {
	ID uuid.UUID `json:"id"`
}

// Classification is the suggestion for a draft. Category is the tenant category matching
// marketflow-ai's label, or null when none matches. Team comes from the tenant's routing rules
// for the suggested category and priority, or null when no rule assigns a team.
type Classification struct {
	Label               string       `json:"label"`
	Category            *CategoryRef `json:"category"`
	Priority            string       `json:"priority"`
	Team                *TeamRef     `json:"team"`
	Confidence          float64      `json:"confidence"`
	Reasoning           string       `json:"reasoning"`
	RequiresHumanReview bool         `json:"requires_human_review"`
	// Injected is marketflow-ai's flag for a draft carrying an injection phrase: confidence is then
	// at most 0.8, review is always required, and auto-classify never applies it.
	Injected bool    `json:"injected"`
	Credits  Credits `json:"credits"`
}

// Summary is a thread summary for the agent.
type Summary struct {
	TicketID uuid.UUID `json:"ticket_id"`
	Summary  string    `json:"summary"`
	Run      RunInfo   `json:"run"`
	Review
	Credits Credits `json:"credits"`
}

// ReplySuggestion is a draft reply. It is never sent: the agent edits it and posts it as a comment.
type ReplySuggestion struct {
	TicketID uuid.UUID `json:"ticket_id"`
	Reply    string    `json:"reply"`
	Run      RunInfo   `json:"run"`
	Review
	Credits Credits `json:"credits"`
}

// KBSuggestion is one ranked article of the tenant's own knowledge base.
type KBSuggestion struct {
	ArticleID uuid.UUID `json:"article_id"`
	Title     string    `json:"title"`
	Slug      string    `json:"slug"`
	Excerpt   string    `json:"excerpt,omitempty"`
	IsPublic  bool      `json:"is_public"`
	Reason    string    `json:"reason,omitempty"`
	Rank      int       `json:"rank"`
}

// KBSuggestions are the ranked articles for a ticket, best first.
type KBSuggestions struct {
	TicketID    uuid.UUID      `json:"ticket_id"`
	Suggestions []KBSuggestion `json:"suggestions"`
	Run         RunInfo        `json:"run"`
	Review
	Credits Credits `json:"credits"`
}

// KBDraft is the draft article saved from a resolved ticket.
type KBDraft struct {
	TicketID uuid.UUID   `json:"ticket_id"`
	Article  *kb.Article `json:"article"`
	Run      RunInfo     `json:"run"`
	Review
	Credits Credits `json:"credits"`
}
