package kb

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Bengo-Hub/httpware/richtext"
	pagination "github.com/Bengo-Hub/pagination"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Service struct {
	repo Repository
	log  *zap.Logger
}

func NewService(repo Repository, log *zap.Logger) *Service {
	return &Service{repo: repo, log: log}
}

func (s *Service) CreateArticle(ctx context.Context, req CreateArticleRequest) (*Article, error) {
	if req.Slug == "" {
		req.Slug = slugify(req.Title)
	}
	// Article bodies come from the shared rich text editor and are shown on public help pages.
	req.Content = richtext.Sanitize(req.Content)
	return s.repo.Create(ctx, req)
}

func (s *Service) GetArticle(ctx context.Context, tenantID, id uuid.UUID) (*Article, error) {
	return s.repo.GetByID(ctx, tenantID, id)
}

// GetArticleBySlug serves the public KB page: only a public, published article of the tenant is
// found (a draft or archived article, or one marked private, is not found) and only then counted.
func (s *Service) GetArticleBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*Article, error) {
	article, err := s.ReadArticleBySlug(ctx, tenantID, slug)
	if err != nil {
		return nil, err
	}
	// Fire-and-forget view count increment
	go func() {
		if ierr := s.repo.IncrementViewCount(context.Background(), tenantID, article.ID); ierr != nil {
			s.log.Warn("failed to increment view count", zap.Error(ierr))
		}
	}()
	return article, nil
}

// ReadArticleBySlug is GetArticleBySlug without counting a view. ticketing-ui's server reads this
// way for its short page cache; the visitor's browser then counts the view with RecordPublicView,
// so a view is one page view and not one cache miss.
func (s *Service) ReadArticleBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*Article, error) {
	article, err := s.repo.GetBySlug(ctx, tenantID, slug)
	if err != nil {
		return nil, err
	}
	if !visibleIn(tenantID, article) {
		return nil, ErrArticleNotFound
	}
	return article, nil
}

// RecordPublicView counts one view of a published, public article of the tenant. Anything the
// public routes would not show is not found and not counted.
func (s *Service) RecordPublicView(ctx context.Context, tenantID, id uuid.UUID) error {
	article, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if !visibleIn(tenantID, article) {
		return ErrArticleNotFound
	}
	return s.repo.IncrementViewCount(ctx, tenantID, article.ID)
}

func (s *Service) UpdateArticle(ctx context.Context, tenantID, id uuid.UUID, req UpdateArticleRequest) (*Article, error) {
	if req.Content != nil {
		clean := richtext.Sanitize(*req.Content)
		req.Content = &clean
	}
	return s.repo.Update(ctx, tenantID, id, req)
}

func (s *Service) DeleteArticle(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.repo.Delete(ctx, tenantID, id)
}

// ListArticles is one keyset page of the filtered articles, newest first.
func (s *Service) ListArticles(ctx context.Context, filter ArticleFilter) (*ArticleList, error) {
	filter.Limit = normalLimit(filter.Limit)
	rows, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return pageOf(rows, filter.Limit), nil
}

// publicFilter is the only filter the public routes use: the tenant's published, public articles.
func publicFilter(tenantID uuid.UUID) ArticleFilter {
	isPublic := true
	return ArticleFilter{TenantID: tenantID, Status: StatusPublished, IsPublic: &isPublic}
}

// visibleIn is publiclyVisible for one tenant. The public calls check every row they return with
// it on top of the query filter, so a repository fault shows less, never a draft, a private
// article or another tenant's article.
func visibleIn(tenantID uuid.UUID, a *Article) bool {
	return publiclyVisible(a) && a.TenantID == tenantID
}

// ListPublicArticles is one keyset page of the tenant's public knowledge base, newest first.
func (s *Service) ListPublicArticles(ctx context.Context, tenantID uuid.UUID, search, category string, after *Cursor, limit int) (*ArticleList, error) {
	filter := publicFilter(tenantID)
	filter.Search, filter.Category, filter.After, filter.Limit = search, category, after, limit
	list, err := s.ListArticles(ctx, filter)
	if err != nil {
		return nil, err
	}
	kept := list.Articles[:0:0]
	for _, a := range list.Articles {
		if visibleIn(tenantID, a) {
			kept = append(kept, a)
		}
	}
	list.Articles = kept
	return list, nil
}

// PublicCategories lists the categories of the tenant's public knowledge base with their counts.
func (s *Service) PublicCategories(ctx context.Context, tenantID uuid.UUID) ([]CategoryCount, error) {
	cats, err := s.repo.Categories(ctx, publicFilter(tenantID))
	if err != nil {
		return nil, err
	}
	if cats == nil {
		cats = []CategoryCount{}
	}
	return cats, nil
}

// MaxSuggestions is the most articles the public form's suggestions return.
const MaxSuggestions = 5

// SuggestPublicArticles ranks the tenant's public articles for a subject the visitor is typing.
// It is a plain database search: no model call and no AI credit, so anyone may use it.
func (s *Service) SuggestPublicArticles(ctx context.Context, tenantID uuid.UUID, subject string, limit int) ([]*Article, error) {
	if limit <= 0 || limit > MaxSuggestions {
		limit = MaxSuggestions
	}
	words := suggestionWords(subject)
	if len(words) == 0 {
		return []*Article{}, nil
	}
	rows, err := s.repo.Suggest(ctx, publicFilter(tenantID), words, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*Article, 0, len(rows))
	for _, a := range rows {
		if visibleIn(tenantID, a) && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

// maxSuggestionWords bounds the words one suggestion query matches, so a long subject stays a
// cheap query.
const maxSuggestionWords = 6

// stopWords are common English and Swahili words that say nothing about the problem.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "not": true, "can": true, "cannot": true,
	"cant": true, "how": true, "what": true, "why": true, "when": true, "where": true,
	"does": true, "doesn": true, "don": true, "dont": true, "this": true, "that": true, "from": true,
	"have": true, "has": true, "was": true, "are": true, "you": true, "your": true, "our": true,
	"its": true, "into": true, "about": true, "please": true, "help": true, "need": true, "get": true,
	"una": true, "kwa": true, "kwenye": true, "hii": true, "hiyo": true, "nini": true, "jinsi": true,
	"tafadhali": true, "naomba": true, "msaada": true, "sana": true, "lakini": true, "pia": true,
}

// suggestionWords are the distinct words of a subject worth matching: lower case, letters and
// digits only, at least three characters (what a trigram index serves), no stop words, at most
// maxSuggestionWords.
func suggestionWords(subject string) []string {
	subject = pagination.NormalizeSearch(subject)
	seen := map[string]bool{}
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(subject), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(out) == maxSuggestionWords {
			break
		}
		if utf8.RuneCountInString(w) < 3 || stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// VoteArticle records a public "was this helpful" vote, only on a public, published article of
// the tenant in the path.
func (s *Service) VoteArticle(ctx context.Context, tenantID, id uuid.UUID, helpful bool) error {
	article, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if !visibleIn(tenantID, article) {
		return ErrArticleNotFound
	}
	return s.repo.Vote(ctx, tenantID, id, helpful)
}

// publiclyVisible is what the public routes may show: public and published.
func publiclyVisible(a *Article) bool {
	return a != nil && a.IsPublic && a.Status == StatusPublished
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
