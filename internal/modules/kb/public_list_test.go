package kb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// memRepo is an in-memory Repository. With leaky set it ignores every filter and returns all
// rows of all tenants, so a test proves the service itself never lets a hidden article out.
type memRepo struct {
	rows     []*Article
	leaky    bool
	filters  []ArticleFilter
	words    [][]string
	votes    int
	views    atomic.Int32
	lastSlug string
}

func (m *memRepo) match(f ArticleFilter, a *Article) bool {
	if m.leaky {
		return true
	}
	if a.TenantID != f.TenantID {
		return false
	}
	if f.Status != "" && a.Status != f.Status {
		return false
	}
	if f.IsPublic != nil && a.IsPublic != *f.IsPublic {
		return false
	}
	if f.Category != "" && a.Category != f.Category {
		return false
	}
	if f.Search != "" && !strings.Contains(strings.ToLower(a.Title), strings.ToLower(f.Search)) {
		return false
	}
	return true
}

// before is the keyset order: newest first, then id descending.
func before(a, b *Article) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID.String() > b.ID.String()
}

func (m *memRepo) List(_ context.Context, f ArticleFilter) ([]*Article, error) {
	m.filters = append(m.filters, f)
	var out []*Article
	for _, a := range m.rows {
		if !m.match(f, a) {
			continue
		}
		if c := f.After; c != nil && !m.leaky && !before(&Article{CreatedAt: c.CreatedAt, ID: c.ID}, a) {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return before(out[i], out[j]) })
	if len(out) > f.Limit+1 {
		out = out[:f.Limit+1]
	}
	return out, nil
}

func (m *memRepo) Categories(_ context.Context, f ArticleFilter) ([]CategoryCount, error) {
	m.filters = append(m.filters, f)
	counts := map[string]int{}
	for _, a := range m.rows {
		if m.match(f, a) && a.Category != "" {
			counts[a.Category]++
		}
	}
	out := []CategoryCount{}
	for n, c := range counts {
		out = append(out, CategoryCount{Name: n, Count: c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *memRepo) Suggest(_ context.Context, f ArticleFilter, words []string, limit int) ([]*Article, error) {
	m.filters = append(m.filters, f)
	m.words = append(m.words, words)
	var out []*Article
	for _, a := range m.rows {
		if !m.match(f, a) {
			continue
		}
		for _, w := range words {
			if m.leaky || strings.Contains(strings.ToLower(a.Title), w) {
				out = append(out, a)
				break
			}
		}
	}
	if !m.leaky && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memRepo) find(tenantID uuid.UUID, pick func(*Article) bool) (*Article, error) {
	for _, a := range m.rows {
		if (m.leaky || a.TenantID == tenantID) && pick(a) {
			return a, nil
		}
	}
	return nil, ErrArticleNotFound
}

func (m *memRepo) GetByID(_ context.Context, tenantID, id uuid.UUID) (*Article, error) {
	return m.find(tenantID, func(a *Article) bool { return a.ID == id })
}

func (m *memRepo) GetBySlug(_ context.Context, tenantID uuid.UUID, slug string) (*Article, error) {
	m.lastSlug = slug
	return m.find(tenantID, func(a *Article) bool { return a.Slug == slug })
}

func (m *memRepo) Vote(context.Context, uuid.UUID, uuid.UUID, bool) error {
	m.votes++
	return nil
}

// IncrementViewCount counts atomically: the GET path counts on its own goroutine.
func (m *memRepo) IncrementViewCount(context.Context, uuid.UUID, uuid.UUID) error {
	m.views.Add(1)
	return nil
}
func (m *memRepo) Create(context.Context, CreateArticleRequest) (*Article, error) {
	return nil, errors.New("unused")
}
func (m *memRepo) Update(context.Context, uuid.UUID, uuid.UUID, UpdateArticleRequest) (*Article, error) {
	return nil, errors.New("unused")
}
func (m *memRepo) Delete(context.Context, uuid.UUID, uuid.UUID) error { return errors.New("unused") }

// fixture is one tenant's knowledge base: visible articles plus a draft, an archived article, a
// private one, and a published public article of another tenant, all titled "Reset password".
type fixture struct {
	tenant, other uuid.UUID
	visible       []*Article
	hidden        []*Article
}

func newFixture(visible int) (*fixture, *memRepo) {
	f := &fixture{tenant: uuid.New(), other: uuid.New()}
	base := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	mk := func(tenant uuid.UUID, slug string, status ArticleStatus, public bool, age int) *Article {
		return &Article{ID: uuid.New(), TenantID: tenant, Title: "Reset password " + slug, Slug: slug,
			Content: "<p>body</p>", Category: "Account", Status: status, IsPublic: public,
			Metadata:  map[string]any{"ticket_number": "TKT-1"},
			CreatedAt: base.Add(-time.Duration(age) * time.Minute)}
	}
	for i := 0; i < visible; i++ {
		f.visible = append(f.visible, mk(f.tenant, "visible-"+string(rune('a'+i)), StatusPublished, true, i))
	}
	f.hidden = []*Article{
		mk(f.tenant, "draft", StatusDraft, true, 100),
		mk(f.tenant, "archived", StatusArchived, true, 101),
		mk(f.tenant, "private", StatusPublished, false, 102),
		mk(f.other, "other-tenant", StatusPublished, true, 103),
	}
	f.hidden[3].Category = "Elsewhere"
	repo := &memRepo{rows: append(append([]*Article{}, f.visible...), f.hidden...)}
	return f, repo
}

func (f *fixture) isVisible(id uuid.UUID) bool {
	for _, a := range f.visible {
		if a.ID == id {
			return true
		}
	}
	return false
}

func assertPublicFilter(t *testing.T, tenant uuid.UUID, filters []ArticleFilter) {
	t.Helper()
	if len(filters) == 0 {
		t.Fatal("the repository was not called")
	}
	for _, f := range filters {
		if f.TenantID != tenant || f.Status != StatusPublished || f.IsPublic == nil || !*f.IsPublic {
			t.Fatalf("a public call must filter to the tenant's published public articles: %+v", f)
		}
	}
}

func TestPublicCallsNeverReturnHiddenArticles(t *testing.T) {
	for _, leaky := range []bool{false, true} {
		f, repo := newFixture(2)
		repo.leaky = leaky
		svc := NewService(repo, zap.NewNop())
		ctx := context.Background()

		list, err := svc.ListPublicArticles(ctx, f.tenant, "", "", nil, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(list.Articles) != 2 {
			t.Errorf("leaky=%v: list returned %d articles, want the 2 visible", leaky, len(list.Articles))
		}
		for _, a := range list.Articles {
			if !f.isVisible(a.ID) {
				t.Errorf("leaky=%v: list leaked %s", leaky, a.Slug)
			}
		}

		sugg, err := svc.SuggestPublicArticles(ctx, f.tenant, "How do I reset my password?", 5)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range sugg {
			if !f.isVisible(a.ID) {
				t.Errorf("leaky=%v: suggestions leaked %s", leaky, a.Slug)
			}
		}
		if len(sugg) != 2 {
			t.Errorf("leaky=%v: suggestions returned %d, want 2", leaky, len(sugg))
		}

		for _, h := range f.hidden {
			if _, err := svc.GetArticleBySlug(ctx, f.tenant, h.Slug); !errors.Is(err, ErrArticleNotFound) {
				t.Errorf("leaky=%v: get by slug %s = %v, want not found", leaky, h.Slug, err)
			}
			if err := svc.VoteArticle(ctx, f.tenant, h.ID, true); !errors.Is(err, ErrArticleNotFound) {
				t.Errorf("leaky=%v: vote on %s = %v, want not found", leaky, h.Slug, err)
			}
		}
		if repo.votes != 0 {
			t.Errorf("leaky=%v: a hidden article was voted on", leaky)
		}
		if _, err := svc.GetArticleBySlug(ctx, f.tenant, f.visible[0].Slug); err != nil {
			t.Errorf("leaky=%v: a visible article must be found: %v", leaky, err)
		}
		assertPublicFilter(t, f.tenant, repo.filters)
	}
}

func TestPublicCategoriesOnlyCountVisibleArticles(t *testing.T) {
	f, repo := newFixture(3)
	cats, err := NewService(repo, zap.NewNop()).PublicCategories(context.Background(), f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 1 || cats[0].Name != "Account" || cats[0].Count != 3 {
		t.Fatalf("categories = %+v, want Account: 3", cats)
	}
	assertPublicFilter(t, f.tenant, repo.filters)
}

func TestSuggestionsAreCappedAndSkipEmptySubjects(t *testing.T) {
	f, repo := newFixture(8)
	svc := NewService(repo, zap.NewNop())
	got, err := svc.SuggestPublicArticles(context.Background(), f.tenant, "reset password", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != MaxSuggestions {
		t.Errorf("got %d suggestions, want at most %d", len(got), MaxSuggestions)
	}
	repo.words = nil
	if got, _ := svc.SuggestPublicArticles(context.Background(), f.tenant, "it is a", 5); len(got) != 0 || len(repo.words) != 0 {
		t.Errorf("a subject with no useful word must not query: %d rows, %d queries", len(got), len(repo.words))
	}
}

func TestSuggestionWords(t *testing.T) {
	cases := map[string][]string{
		"How do I reset my PASSWORD? password!": {"reset", "password"},
		"   ":                                   nil,
		"Siwezi kuingia kwenye akaunti, tafadhali": {"siwezi", "kuingia", "akaunti"},
		"one two three four five six seven eight":  {"one", "two", "three", "four", "five", "six"},
		"M-Pesa payment failed 2x":                 {"pesa", "payment", "failed"},
	}
	for in, want := range cases {
		got := suggestionWords(in)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("suggestionWords(%q) = %v, want %v", in, got, want)
		}
	}
}

// publicRouter mounts the public routes as the app does, for a tenant slug "acme".
func publicRouter(f *fixture, repo *memRepo) http.Handler {
	h := NewHandler(NewService(repo, zap.NewNop()), zap.NewNop())
	h.SetTenantLookup(fakeTenants{"acme": f.tenant.String(), "other": f.other.String()})
	r := chi.NewRouter()
	r.Route("/{tenant}", h.RegisterPublicRoutes)
	return r
}

func getJSON(t *testing.T, h http.Handler, path string, v any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if v != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	return rec.Code
}

// Walking the public list page by page returns every visible article once, newest first, and
// nothing else.
func TestPublicListKeysetPaging(t *testing.T) {
	f, repo := newFixture(5)
	h := publicRouter(f, repo)
	var seen []string
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("paging did not end")
		}
		var page struct {
			Data       []map[string]any `json:"data"`
			NextCursor string           `json:"next_cursor"`
			HasMore    bool             `json:"has_more"`
		}
		if code := getJSON(t, h, "/acme/kb/articles?limit=2&cursor="+cursor, &page); code != http.StatusOK {
			t.Fatalf("page %d: status %d", pages, code)
		}
		for _, row := range page.Data {
			seen = append(seen, row["slug"].(string))
			for _, field := range []string{"tenant_id", "author_id", "status", "is_public", "metadata", "content"} {
				if _, ok := row[field]; ok {
					t.Errorf("a public list row must not carry %q", field)
				}
			}
		}
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Error("the last page has no next cursor")
			}
			break
		}
		if len(page.Data) != 2 {
			t.Fatalf("a page with more must be full: %d", len(page.Data))
		}
		cursor = page.NextCursor
	}
	want := []string{"visible-a", "visible-b", "visible-c", "visible-d", "visible-e"}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("paged slugs = %v, want %v", seen, want)
	}
	if code := getJSON(t, h, "/acme/kb/articles?cursor=garbage!", nil); code != http.StatusBadRequest {
		t.Errorf("a bad cursor = %d, want 400", code)
	}
}

func TestPublicRoutesByTenantSlug(t *testing.T) {
	f, repo := newFixture(1)
	h := publicRouter(f, repo)

	var article map[string]any
	if code := getJSON(t, h, "/acme/kb/articles/visible-a", &article); code != http.StatusOK {
		t.Fatalf("visible article: %d", code)
	}
	if article["content"] != "<p>body</p>" || article["tenant_id"] != nil || article["metadata"] != nil {
		t.Errorf("article page shape: %v", article)
	}
	for _, slug := range []string{"draft", "archived", "private", "other-tenant"} {
		if code := getJSON(t, h, "/acme/kb/articles/"+slug, nil); code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", slug, code)
		}
	}
	if code := getJSON(t, h, "/nobody/kb/articles", nil); code != http.StatusNotFound {
		t.Errorf("unknown tenant: %d, want 404", code)
	}

	// The other tenant's list holds only its own article.
	var other struct {
		Data []map[string]any `json:"data"`
	}
	getJSON(t, h, "/other/kb/articles", &other)
	if len(other.Data) != 1 || other.Data[0]["slug"] != "other-tenant" {
		t.Errorf("other tenant list = %v", other.Data)
	}

	var sugg struct {
		Data []map[string]any `json:"data"`
	}
	if code := getJSON(t, h, "/acme/kb/suggestions?q=reset+password", &sugg); code != http.StatusOK {
		t.Fatalf("suggestions: %d", code)
	}
	if len(sugg.Data) != 1 || sugg.Data[0]["slug"] != "visible-a" || sugg.Data[0]["content"] != nil {
		t.Errorf("suggestions = %v", sugg.Data)
	}

	var cats struct {
		Data []CategoryCount `json:"data"`
	}
	getJSON(t, h, "/acme/kb/categories", &cats)
	if len(cats.Data) != 1 || cats.Data[0].Name != "Account" {
		t.Errorf("categories = %v", cats.Data)
	}
}

type denyLimiter struct{ keys []string }

func (d *denyLimiter) Allow(_ context.Context, key string, _ ratelimit.Options, _ int) (bool, time.Duration) {
	d.keys = append(d.keys, key)
	return false, 30 * time.Second
}

func TestPublicRoutesAreRateLimited(t *testing.T) {
	f, repo := newFixture(1)
	h := NewHandler(NewService(repo, zap.NewNop()), zap.NewNop())
	h.SetTenantLookup(fakeTenants{"acme": f.tenant.String()})
	lim := &denyLimiter{}
	h.SetLimiter(lim)
	r := chi.NewRouter()
	r.Route("/{tenant}", h.RegisterPublicRoutes)
	for _, path := range []string{"/acme/kb/articles", "/acme/kb/articles/visible-a", "/acme/kb/categories", "/acme/kb/suggestions?q=reset"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "30" {
			t.Errorf("%s: %d Retry-After %q, want 429 and 30", path, rec.Code, rec.Header().Get("Retry-After"))
		}
	}
	if len(lim.keys) != 4 || !strings.HasPrefix(lim.keys[0], "acme:") {
		t.Errorf("limiter keys = %v, want tenant:ip", lim.keys)
	}
	if repo.lastSlug != "" {
		t.Error("a limited request must not reach the service")
	}
}

// The repository's predicates always carry the tenant, not deleted, published and public for a
// public filter, and the keyset condition as one row comparison.
func TestFilterPredicatesSQL(t *testing.T) {
	tenant := uuid.New()
	filter := publicFilter(tenant)
	filter.After = &Cursor{CreatedAt: time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC), ID: uuid.New()}
	sel := entsql.Dialect(dialect.Postgres).Select("*").From(entsql.Table("kb_articles"))
	for _, p := range filterPredicates(filter) {
		p(sel)
	}
	afterCursor(filter.After)(sel)
	query, args := sel.Query()
	for _, want := range []string{
		`"kb_articles"."tenant_id" = $1`,
		`"kb_articles"."deleted_at" IS NULL`,
		`"kb_articles"."status" = $2`,
		`"kb_articles"."is_public"`,
		`("kb_articles"."created_at", "kb_articles"."id") < (`,
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query lacks %s:\n%s", want, query)
		}
	}
	if args[0] != tenant {
		t.Errorf("first arg = %v, want the tenant", args[0])
	}
}

// The suggestion query binds every argument once, numbered in order: 2 filter args (tenant and
// status; is_public is a bare column) and 6 match args, with no ORDER BY expression reusing low
// placeholder numbers.
func TestSuggestQueryNumbersItsArguments(t *testing.T) {
	sel := entsql.Dialect(dialect.Postgres).Select("*").From(entsql.Table("kb_articles"))
	for _, p := range filterPredicates(publicFilter(uuid.New())) {
		p(sel)
	}
	words := []string{"reset", "password"}
	suggestMatch(words)(sel)
	query, args := sel.Query()
	if len(args) != 8 || !strings.Contains(query, "$8") || strings.Contains(query, "$9") || strings.Contains(query, "ORDER BY") {
		t.Fatalf("args %d, query:\n%s", len(args), query)
	}
}

// Suggestions rank title matches above excerpt and body matches, keep the database order between
// equal scores, and are capped at the limit.
func TestRankSuggestions(t *testing.T) {
	body := &Article{Title: "Billing", Content: "how to reset a card"}
	excerpt := &Article{Title: "Account", Excerpt: "Reset steps"}
	title := &Article{Title: "Reset your password"}
	tie := &Article{Title: "Reset the router"}
	got := rankSuggestions([]*Article{body, excerpt, title, tie}, []string{"reset", "password"}, 3)
	if len(got) != 3 || got[0] != title || got[1] != tie || got[2] != excerpt {
		t.Fatalf("ranked %v", []string{got[0].Title, got[1].Title, got[2].Title})
	}
}

func TestPageOf(t *testing.T) {
	base := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	rows := make([]*Article, 3)
	for i := range rows {
		rows[i] = &Article{ID: uuid.New(), CreatedAt: base.Add(-time.Duration(i) * time.Minute)}
	}
	p := pageOf(rows, 2)
	if !p.HasMore || len(p.Articles) != 2 {
		t.Fatalf("limit+1 rows give a full page with more: %+v", p)
	}
	c, err := ParseCursor(p.NextCursor)
	if err != nil || c.ID != rows[1].ID || !c.CreatedAt.Equal(rows[1].CreatedAt) {
		t.Fatalf("next cursor must point at the last row kept: %v %v", c, err)
	}
	if last := pageOf(rows[:2], 2); last.HasMore || last.NextCursor != "" {
		t.Fatalf("a short page is the last: %+v", last)
	}
	if empty := pageOf(nil, 2); empty.Articles == nil {
		t.Fatal("an empty page encodes as [], not null")
	}
}
