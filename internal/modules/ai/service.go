package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/clients"
	"github.com/bengobox/ticketing-service/internal/modules/kb"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// autoClassifyTimeout bounds one background classification; autoClassifySlots bounds how many
// run at once on a pod. A create beyond that is simply not auto-classified.
const (
	autoClassifyTimeout = 45 * time.Second
	autoClassifySlots   = 4
)

// ErrEmptyDraft refuses a classify request with neither subject nor description.
var ErrEmptyDraft = errors.New("ai: subject or description required")

// Service runs the AI features.
type Service struct {
	model    Model
	wallet   Wallet
	tickets  TicketStore
	kb       KBStore
	settings Settings
	routing  tickets.RulesEvaluator
	sse      tickets.SSEPublisher
	answers  AnswerCache
	log      *zap.Logger
	slots    chan struct{}
}

// Deps are the Service's collaborators. Routing, SSE and Answers are optional.
type Deps struct {
	Model    Model
	Wallet   Wallet
	Tickets  TicketStore
	KB       KBStore
	Settings Settings
	Routing  tickets.RulesEvaluator
	SSE      tickets.SSEPublisher
	Answers  AnswerCache
}

// NewService creates the AI service.
func NewService(d Deps, log *zap.Logger) *Service {
	return &Service{
		model: d.Model, wallet: d.Wallet, tickets: d.Tickets, kb: d.KB, settings: d.Settings,
		routing: d.Routing, sse: d.SSE, answers: d.Answers,
		log: log.Named("ai"), slots: make(chan struct{}, autoClassifySlots),
	}
}

// draftRef identifies a draft by its text, so one request key on two different drafts is two
// requests.
func draftRef(subject, description string) string {
	sum := sha256.Sum256([]byte(subject + "\x00" + description))
	return "draft-" + hex.EncodeToString(sum[:8])
}

// Classify suggests a category, priority and team for a draft ticket.
func (s *Service) Classify(ctx context.Context, tenantID uuid.UUID, req ClassifyRequest, requestKey string) (*Classification, error) {
	subject := plainText(req.Subject, maxSubjectChars)
	description := plainText(req.Description, maxBodyChars)
	if subject == "" && description == "" {
		return nil, ErrEmptyDraft
	}
	out := &Classification{}
	credits, err := s.metered(ctx, paidCall{
		TenantID: tenantID, Feature: FeatureClassify,
		Key:         creditKey(FeatureClassify, draftRef(subject, description), requestKey),
		Description: "Ticketing AI: classify a draft ticket",
	}, out, func(ctx context.Context) error {
		c, err := s.classify(ctx, tenantID, subject, description, req.Channel)
		if err != nil {
			return err
		}
		*out = *c
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Credits = credits
	return out, nil
}

// classify asks marketflow-ai's triage and maps its answer onto the tenant's categories and
// routing rules. The subject is required by triage, so a draft with only a description sends its
// first line as the subject.
func (s *Service) classify(ctx context.Context, tenantID uuid.UUID, subject, description, channel string) (*Classification, error) {
	if subject == "" {
		subject = capChars(strings.SplitN(description, "\n", 2)[0], maxSubjectChars)
	}
	res, err := s.model.Triage(ctx, clients.TriageInput{TenantID: tenantID, Subject: subject, Description: description})
	if err != nil {
		s.log.Warn("ai classify failed", zap.Error(err))
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	out := &Classification{
		Label: res.Category, Priority: ticketPriority(res.Priority), Confidence: res.Confidence,
		Reasoning: res.Reasoning, RequiresHumanReview: res.RequiresHumanReview || res.Injected, Injected: res.Injected,
	}
	cats, _, err := s.tickets.ListCategories(ctx, tenantID, tickets.CategoryFilter{TenantID: tenantID, IsActive: ptr(true), Limit: 200})
	if err != nil {
		s.log.Warn("categories not read for ai classify", zap.Error(err))
	}
	out.Category = matchCategory(res.Category, cats)
	if s.routing != nil {
		var catID *uuid.UUID
		if out.Category != nil {
			catID = &out.Category.ID
		}
		if act := s.routing.EvaluateRules(ctx, tenantID, out.Priority, channel, subject, description, catID); act != nil && act.Type == "assign_team" {
			if id, err := uuid.Parse(act.Value); err == nil {
				out.Team = &TeamRef{ID: id}
			}
		}
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }

// ticketPriority maps triage's priority onto the ticket priorities; anything else is normal.
func ticketPriority(p string) string {
	switch tickets.TicketPriority(p) {
	case tickets.PriorityLow, tickets.PriorityNormal, tickets.PriorityHigh, tickets.PriorityUrgent:
		return p
	}
	return string(tickets.PriorityNormal)
}

// matchCategory finds the tenant category for triage's label: an equal name first, then a name
// that contains the label or a word of the name that starts the label (Tech for technical).
func matchCategory(label string, cats []*tickets.TicketCategory) *CategoryRef {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return nil
	}
	for _, c := range cats {
		if strings.ToLower(strings.TrimSpace(c.Name)) == label {
			return &CategoryRef{ID: c.ID, Name: c.Name}
		}
	}
	for _, c := range cats {
		name := strings.ToLower(c.Name)
		if strings.Contains(name, label) {
			return &CategoryRef{ID: c.ID, Name: c.Name}
		}
		for _, w := range strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) }) {
			if len(w) >= 4 && strings.HasPrefix(label, w) {
				return &CategoryRef{ID: c.ID, Name: c.Name}
			}
		}
	}
	return nil
}

// thread reads the ticket's newest messages, oldest first. publicOnly leaves out internal notes.
func (s *Service) thread(ctx context.Context, tenantID, ticketID uuid.UUID, publicOnly bool) ([]message, error) {
	page, err := s.tickets.PageComments(ctx, tenantID, ticketID, tickets.CommentQuery{
		Page: tickets.PageParams{Limit: maxThreadMessages}, PublicOnly: publicOnly, Newest: true,
	})
	if err != nil {
		return nil, err
	}
	rows := page.Data
	if len(rows) > maxThreadMessages {
		rows = rows[:maxThreadMessages]
	}
	chrono := make([]*tickets.TicketComment, len(rows))
	for i, c := range rows {
		chrono[len(rows)-1-i] = c
	}
	return threadData(chrono), nil
}

// runTask runs one task on marketflow-ai and maps a failure to ErrUnavailable.
func (s *Service) runTask(ctx context.Context, tenantID uuid.UUID, key, instructions string, data map[string]any) (clients.TaskResult, error) {
	res, err := s.model.RunTask(ctx, clients.TaskInput{RunID: key, TenantID: tenantID, Instructions: instructions, Data: data})
	if err != nil {
		s.log.Warn("ai task failed", zap.String("run_id", key), zap.Error(err))
		return clients.TaskResult{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return res, nil
}

// Summarize summarizes a ticket's thread, internal notes included (staff only).
func (s *Service) Summarize(ctx context.Context, t *tickets.Ticket, requestKey string) (*Summary, error) {
	out := &Summary{TicketID: t.ID}
	key := creditKey(FeatureSummary, t.ID.String(), requestKey)
	credits, err := s.metered(ctx, paidCall{TenantID: t.TenantID, Feature: FeatureSummary, Key: key,
		Description: "Ticketing AI: summary of " + t.TicketNumber,
	}, out, func(ctx context.Context) error {
		msgs, err := s.thread(ctx, t.TenantID, t.ID, false)
		if err != nil {
			return err
		}
		res, err := s.runTask(ctx, t.TenantID, key, summaryInstructions, map[string]any{"ticket": ticketData(t), "messages": msgs})
		if err != nil {
			return err
		}
		out.Summary = res.Answer
		out.Run = RunInfo{StoppedEarly: res.StoppedEarly, StopReason: res.StopReason}
		out.Review = reviewOf(res)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Credits = credits
	return out, nil
}

// SuggestReply drafts a reply to the customer's latest public message. It is only returned: the
// agent edits it and posts it as a comment, nothing here sends it.
func (s *Service) SuggestReply(ctx context.Context, t *tickets.Ticket, requestKey string) (*ReplySuggestion, error) {
	out := &ReplySuggestion{TicketID: t.ID}
	key := creditKey(FeatureReply, t.ID.String(), requestKey)
	credits, err := s.metered(ctx, paidCall{TenantID: t.TenantID, Feature: FeatureReply, Key: key,
		Description: "Ticketing AI: suggested reply on " + t.TicketNumber,
	}, out, func(ctx context.Context) error {
		msgs, err := s.thread(ctx, t.TenantID, t.ID, true)
		if err != nil {
			return err
		}
		res, err := s.runTask(ctx, t.TenantID, key, replyInstructions, map[string]any{"ticket": ticketData(t), "messages": msgs})
		if err != nil {
			return err
		}
		out.Reply = res.Answer
		out.Run = RunInfo{StoppedEarly: res.StoppedEarly, StopReason: res.StopReason}
		out.Review = reviewOf(res)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Credits = credits
	return out, nil
}

// candidates gathers up to maxCandidates published articles of the tenant: matches for the
// subject's longer words first, then the most recent articles.
func (s *Service) candidates(ctx context.Context, t *tickets.Ticket) ([]*kb.Article, error) {
	seen := map[uuid.UUID]bool{}
	var out []*kb.Article
	add := func(list *kb.ArticleList) {
		if list == nil {
			return
		}
		for _, a := range list.Articles {
			if len(out) < maxCandidates && a.TenantID == t.TenantID && !seen[a.ID] {
				seen[a.ID] = true
				out = append(out, a)
			}
		}
	}
	terms := 0
	for _, w := range strings.FieldsFunc(plainText(t.Subject, maxSubjectChars), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if terms == 4 || len(out) >= maxCandidates {
			break
		}
		if len([]rune(w)) < 4 {
			continue
		}
		terms++
		list, err := s.kb.ListArticles(ctx, kb.ArticleFilter{TenantID: t.TenantID, Status: kb.StatusPublished, Search: w, Limit: 10})
		if err != nil {
			return nil, err
		}
		add(list)
	}
	if len(out) < maxCandidates {
		list, err := s.kb.ListArticles(ctx, kb.ArticleFilter{TenantID: t.TenantID, Status: kb.StatusPublished, Limit: maxCandidates})
		if err != nil {
			return nil, err
		}
		add(list)
	}
	return out, nil
}

// SuggestArticles ranks the tenant's own published articles for a ticket. A tenant with no
// published article gets an empty list, with no model call and no charge.
func (s *Service) SuggestArticles(ctx context.Context, t *tickets.Ticket, requestKey string) (*KBSuggestions, error) {
	cands, err := s.candidates(ctx, t)
	if err != nil {
		return nil, err
	}
	out := &KBSuggestions{TicketID: t.ID, Suggestions: []KBSuggestion{}}
	if len(cands) == 0 {
		return out, nil
	}
	byID := make(map[string]*kb.Article, len(cands))
	list := make([]map[string]any, len(cands))
	for i, a := range cands {
		byID[a.ID.String()] = a
		list[i] = articleData(a)
	}
	key := creditKey(FeatureKBSuggestions, t.ID.String(), requestKey)
	credits, err := s.metered(ctx, paidCall{TenantID: t.TenantID, Feature: FeatureKBSuggestions, Key: key,
		Description: "Ticketing AI: article suggestions for " + t.TicketNumber,
	}, out, func(ctx context.Context) error {
		res, err := s.runTask(ctx, t.TenantID, key, kbRankInstructions, map[string]any{"ticket": ticketData(t), "articles": list})
		if err != nil {
			return err
		}
		var ranked struct {
			Articles []struct {
				ID     string `json:"id"`
				Reason string `json:"reason"`
			} `json:"articles"`
		}
		if err := decodeJSONAnswer(res.Answer, &ranked); err != nil {
			return fmt.Errorf("%w: unreadable ranking: %v", ErrUnavailable, err)
		}
		picked := map[string]bool{}
		for _, r := range ranked.Articles {
			a, ok := byID[strings.TrimSpace(r.ID)]
			if !ok || picked[a.ID.String()] || len(out.Suggestions) == maxSuggestions {
				continue // only the candidates sent, each once
			}
			picked[a.ID.String()] = true
			out.Suggestions = append(out.Suggestions, KBSuggestion{
				ArticleID: a.ID, Title: a.Title, Slug: a.Slug, Excerpt: plainText(a.Excerpt, maxArticleChars),
				IsPublic: a.IsPublic, Reason: capChars(strings.TrimSpace(r.Reason), 300), Rank: len(out.Suggestions) + 1,
			})
		}
		out.Run = RunInfo{StoppedEarly: res.StoppedEarly, StopReason: res.StopReason}
		out.Review = reviewOf(res)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Credits = credits
	return out, nil
}

// DraftArticle writes a knowledge base article from a resolved or closed ticket and saves it as a
// private draft. It is never published here; a person reviews and publishes it in the KB editor.
// The saved draft is part of the kept answer, so a retried request returns the same draft instead
// of saving a second one; a draft whose credit could not be spent is removed again.
func (s *Service) DraftArticle(ctx context.Context, t *tickets.Ticket, authorID uuid.UUID, requestKey string) (*KBDraft, error) {
	if t.Status != tickets.StatusResolved && t.Status != tickets.StatusClosed {
		return nil, ErrNotResolved
	}
	out := &KBDraft{TicketID: t.ID}
	key := creditKey(FeatureKBDraft, t.ID.String(), requestKey)
	credits, err := s.metered(ctx, paidCall{TenantID: t.TenantID, Feature: FeatureKBDraft, Key: key,
		Description: "Ticketing AI: article draft from " + t.TicketNumber,
	}, out, func(ctx context.Context) error {
		msgs, err := s.thread(ctx, t.TenantID, t.ID, true)
		if err != nil {
			return err
		}
		res, err := s.runTask(ctx, t.TenantID, key, kbDraftInstructions, map[string]any{"ticket": ticketData(t), "messages": msgs})
		if err != nil {
			return err
		}
		var draft struct {
			Title   string   `json:"title"`
			Excerpt string   `json:"excerpt"`
			Steps   []string `json:"steps"`
		}
		if err := decodeJSONAnswer(res.Answer, &draft); err != nil {
			return fmt.Errorf("%w: unreadable draft: %v", ErrUnavailable, err)
		}
		content := draftHTML(draft.Steps)
		if strings.TrimSpace(draft.Title) == "" || content == "" {
			return fmt.Errorf("%w: empty draft", ErrUnavailable)
		}
		article, err := s.kb.CreateArticle(ctx, kb.CreateArticleRequest{
			TenantID: t.TenantID, AuthorID: authorID,
			Title:    capChars(strings.TrimSpace(draft.Title), 200),
			Content:  content,
			Excerpt:  capChars(strings.TrimSpace(draft.Excerpt), 300),
			Status:   kb.StatusDraft,
			IsPublic: false,
			Metadata: map[string]any{"source": "ai_draft", "ticket_id": t.ID.String(), "ticket_number": t.TicketNumber},
		})
		if err != nil {
			return err
		}
		out.Article = article
		out.Run = RunInfo{StoppedEarly: res.StoppedEarly, StopReason: res.StopReason}
		out.Review = reviewOf(res)
		return nil
	})
	if err != nil {
		if out.Article != nil {
			// Saved, then the credit was refused or unknown: the draft is withdrawn with the answer.
			if derr := s.kb.DeleteArticle(context.WithoutCancel(ctx), t.TenantID, out.Article.ID); derr != nil {
				s.log.Warn("unpaid ai draft not removed", zap.String("article_id", out.Article.ID.String()), zap.Error(derr))
			}
		}
		return nil, err
	}
	out.Credits = credits
	return out, nil
}

// stillUncategorised re-reads the ticket, so a category a person set while the model ran is kept.
func (s *Service) stillUncategorised(ctx context.Context, t tickets.Ticket) bool {
	if t.CategoryID != nil {
		return false
	}
	cur, err := s.tickets.GetTicket(ctx, t.TenantID, t.ID)
	return err == nil && cur.CategoryID == nil
}

// TicketCreated is the after-commit auto-classify (tickets.CreatedObserver). It runs only when the
// tenant turned ai.auto_classify on, a slot is free and the wallet allows one credit. It fills the
// category only when the ticket has none and marketflow-ai is confident (no review needed), and
// tells the desk the suggestion over SSE. Every failure is logged and dropped: the ticket was
// already saved.
func (s *Service) TicketCreated(ctx context.Context, t tickets.Ticket) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		s.log.Info("auto-classify skipped, all slots busy", zap.String("ticket_id", t.ID.String()))
		return
	}
	ctx, cancel := context.WithTimeout(ctx, autoClassifyTimeout)
	defer cancel()
	if s.settings == nil {
		return
	}
	on, err := s.settings.AutoClassify(ctx, t.TenantID)
	if err != nil || !on {
		return
	}
	subject, description := plainText(t.Subject, maxSubjectChars), plainText(t.Description, maxBodyChars)
	var c Classification
	credits, err := s.metered(ctx, paidCall{
		TenantID: t.TenantID, Feature: FeatureAutoClassify,
		// One key per ticket, so the ticket is charged once whatever happens.
		Key:         creditKey(FeatureAutoClassify, t.ID.String(), "create"),
		Description: "Ticketing AI: auto-classify " + t.TicketNumber,
	}, &c, func(ctx context.Context) error {
		res, err := s.classify(ctx, t.TenantID, subject, description, string(t.Channel))
		if err != nil {
			return err
		}
		c = *res
		return nil
	})
	if err != nil {
		s.log.Info("auto-classify not applied", zap.String("ticket_id", t.ID.String()), zap.Error(err))
		return
	}
	c.Credits = credits
	if c.Category != nil && !c.Injected && !c.RequiresHumanReview && s.stillUncategorised(ctx, t) {
		if err := s.tickets.UpdateTicket(ctx, t.TenantID, t.ID, &tickets.UpdateTicketRequest{CategoryID: &c.Category.ID}); err != nil {
			s.log.Warn("auto-classify category not saved", zap.String("ticket_id", t.ID.String()), zap.Error(err))
		}
	}
	if s.sse != nil {
		s.sse.Publish(t.TenantID, "ticket.ai_classified", map[string]any{"ticket_id": t.ID, "classification": c})
	}
}
