package ai

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/clients"
	"github.com/bengobox/ticketing-service/internal/modules/kb"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

type fakeModel struct {
	mu        sync.Mutex
	triage    clients.TriageResult
	answer    string
	notes     []string
	tainted   bool
	err       error
	triages   int
	tasks     []clients.TaskInput
	lastInput clients.TriageInput
}

func (m *fakeModel) Triage(_ context.Context, in clients.TriageInput) (clients.TriageResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triages++
	m.lastInput = in
	return m.triage, m.err
}

func (m *fakeModel) RunTask(_ context.Context, in clients.TaskInput) (clients.TaskResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = append(m.tasks, in)
	if m.err != nil {
		return clients.TaskResult{}, m.err
	}
	return clients.TaskResult{Answer: m.answer, ReviewNotes: m.notes, Tainted: m.tainted}, nil
}

func (m *fakeModel) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.triages + len(m.tasks)
}

// fakeWallet answers checks from balance (-1 is exempt) and keeps every spend key, charging a
// key once.
type fakeWallet struct {
	mu       sync.Mutex
	balance  int64
	checkErr error
	checks   int
	spent    map[string]bool
	consumes []string
}

func newWallet(balance int64) *fakeWallet {
	return &fakeWallet{balance: balance, spent: map[string]bool{}}
}

func (w *fakeWallet) Check(_ context.Context, _ uuid.UUID, units int64) (clients.CreditDecision, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.checks++
	if w.checkErr != nil {
		return clients.CreditDecision{}, w.checkErr
	}
	if w.balance < 0 {
		return clients.CreditDecision{Allowed: true, Exempt: true}, nil
	}
	if w.balance < units {
		return clients.CreditDecision{Reason: "insufficient_credits"}, nil
	}
	return clients.CreditDecision{Allowed: true, BundleBalance: w.balance}, nil
}

func (w *fakeWallet) Consume(_ context.Context, _ uuid.UUID, units int64, key, _ string) (clients.CreditDecision, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.consumes = append(w.consumes, key)
	if w.balance < 0 {
		return clients.CreditDecision{Allowed: true, Exempt: true}, nil
	}
	if w.spent[key] {
		return clients.CreditDecision{Allowed: true, Replayed: true}, nil
	}
	if w.balance < units {
		return clients.CreditDecision{Reason: "insufficient_credits"}, nil
	}
	w.balance -= units
	w.spent[key] = true
	return clients.CreditDecision{Allowed: true, BundleBalance: w.balance}, nil
}

func (w *fakeWallet) state() (balance int64, checks, consumes int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.balance, w.checks, len(w.consumes)
}

type fakeTickets struct {
	mu       sync.Mutex
	tickets  map[uuid.UUID]*tickets.Ticket
	comments []*tickets.TicketComment
	cats     []*tickets.TicketCategory
	updates  []tickets.UpdateTicketRequest
	lastQ    tickets.CommentQuery
}

func newTickets() *fakeTickets { return &fakeTickets{tickets: map[uuid.UUID]*tickets.Ticket{}} }

func (f *fakeTickets) add(tenant uuid.UUID, status tickets.TicketStatus) *tickets.Ticket {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &tickets.Ticket{ID: uuid.New(), TenantID: tenant, TicketNumber: "TKT-000042", Subject: "Printer jam on invoices",
		Description: "<p>The printer <b>jams</b> &amp; stops.</p>", Status: status, Priority: tickets.PriorityNormal, Channel: tickets.ChannelEmail}
	f.tickets[t.ID] = t
	return t
}

func (f *fakeTickets) GetTicket(_ context.Context, tenantID, id uuid.UUID) (*tickets.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tickets[id]
	if !ok || t.TenantID != tenantID {
		return nil, tickets.ErrTicketNotFound
	}
	cp := *t
	return &cp, nil
}

func (f *fakeTickets) PageComments(_ context.Context, _, _ uuid.UUID, q tickets.CommentQuery) (tickets.Page[*tickets.TicketComment], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQ = q
	var rows []*tickets.TicketComment
	for i := len(f.comments) - 1; i >= 0; i-- { // newest first
		c := f.comments[i]
		if q.PublicOnly && c.CommentType == tickets.CommentInternal {
			continue
		}
		rows = append(rows, c)
	}
	return tickets.Page[*tickets.TicketComment]{Data: rows}, nil
}

func (f *fakeTickets) ListCategories(_ context.Context, _ uuid.UUID, _ tickets.CategoryFilter) ([]*tickets.TicketCategory, int, error) {
	return f.cats, len(f.cats), nil
}

func (f *fakeTickets) UpdateTicket(_ context.Context, _, id uuid.UUID, req *tickets.UpdateTicketRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, *req)
	if t, ok := f.tickets[id]; ok && req.CategoryID != nil {
		t.CategoryID = req.CategoryID
	}
	return nil
}

type fakeKB struct {
	mu       sync.Mutex
	articles []*kb.Article
	created  []kb.CreateArticleRequest
	deleted  []uuid.UUID
}

func (f *fakeKB) ListArticles(_ context.Context, filter kb.ArticleFilter) (*kb.ArticleList, error) {
	var out []*kb.Article
	for _, a := range f.articles {
		if a.TenantID == filter.TenantID && (filter.Status == "" || a.Status == filter.Status) {
			out = append(out, a)
		}
	}
	return &kb.ArticleList{Articles: out, Total: len(out)}, nil
}

func (f *fakeKB) CreateArticle(_ context.Context, req kb.CreateArticleRequest) (*kb.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, req)
	return &kb.Article{ID: uuid.New(), TenantID: req.TenantID, Title: req.Title, Content: req.Content,
		Status: req.Status, IsPublic: req.IsPublic, Metadata: req.Metadata}, nil
}

func (f *fakeKB) DeleteArticle(_ context.Context, _, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeSettings struct{ on bool }

func (f fakeSettings) AutoClassify(context.Context, uuid.UUID) (bool, error) { return f.on, nil }

type memAnswers struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (c *memAnswers) Get(_ context.Context, key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.m[key]
	return b, ok
}

func (c *memAnswers) Set(_ context.Context, key string, val []byte, _ time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = val
}

type rig struct {
	svc     *Service
	model   *fakeModel
	wallet  *fakeWallet
	tickets *fakeTickets
	kb      *fakeKB
}

func newRig(balance int64, autoClassify bool) *rig {
	r := &rig{model: &fakeModel{}, wallet: newWallet(balance), tickets: newTickets(), kb: &fakeKB{}}
	r.svc = NewService(Deps{Model: r.model, Wallet: r.wallet, Tickets: r.tickets, KB: r.kb,
		Settings: fakeSettings{on: autoClassify}, Answers: &memAnswers{m: map[string][]byte{}}}, zap.NewNop())
	return r
}

// eventually polls cond for up to two seconds, for the auto-classify goroutine.
func eventually(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}
