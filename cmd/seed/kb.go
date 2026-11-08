package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/kbarticle"
)

// Knowledge-base articles a tenant starts with, one JSON file per article under kb/<set>/. The set
// is chosen with KB_SET (for example "hadia"), so a tenant only ever gets its own articles.
//
//go:embed kb
var kbFiles embed.FS

// kbSpec is one seeded article. Content is HTML, the shape the tenant apps' editors save and their
// help pages render after their own sanitising.
type kbSpec struct {
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Excerpt  string   `json:"excerpt"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	Content  string   `json:"content_html"`
}

// seededAuthor marks an article the seed wrote rather than an agent (author_id is required).
var seededAuthor = uuid.Nil

// loadKBSet reads and checks every article of one set, sorted by file name.
func loadKBSet(set string) ([]kbSpec, error) {
	if set == "" || strings.ContainsAny(set, "/\\.") {
		return nil, fmt.Errorf("invalid KB_SET %q", set)
	}
	dir := path.Join("kb", set)
	entries, err := fs.ReadDir(kbFiles, dir)
	if err != nil {
		return nil, fmt.Errorf("unknown KB_SET %q: %w", set, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var out []kbSpec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := kbFiles.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var a kbSpec
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if err := a.check(); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("KB_SET %q has no articles", set)
	}
	return out, nil
}

func (a kbSpec) check() error {
	switch {
	case a.Slug == "" || slugify(a.Slug) != a.Slug:
		return fmt.Errorf("slug %q must be lower-case letters, digits and hyphens", a.Slug)
	case strings.TrimSpace(a.Title) == "" || len(a.Title) > 255:
		return fmt.Errorf("title must be 1 to 255 characters")
	case strings.TrimSpace(a.Content) == "":
		return fmt.Errorf("content_html is empty")
	case len(a.Excerpt) > 500:
		return fmt.Errorf("excerpt is over 500 characters")
	case len(a.Category) > 100:
		return fmt.Errorf("category is over 100 characters")
	}
	return nil
}

// seedKB creates each article of the set that the tenant does not have yet, published and public.
// An article with the same slug is never touched, even if staff edited, archived or deleted it, so
// running the seed again is safe and never undoes their changes.
func seedKB(ctx context.Context, client *ent.Client, tenantID uuid.UUID, set string) (created, skipped int, err error) {
	articles, err := loadKBSet(set)
	if err != nil {
		return 0, 0, err
	}
	now := time.Now().UTC()
	for _, a := range articles {
		exists, err := client.KBArticle.Query().
			Where(kbarticle.TenantID(tenantID), kbarticle.SlugEQ(a.Slug)).
			Exist(ctx)
		if err != nil {
			return created, skipped, fmt.Errorf("check article %q: %w", a.Slug, err)
		}
		if exists {
			log.Printf("  article %q already exists, skipping", a.Slug)
			skipped++
			continue
		}
		q := client.KBArticle.Create().
			SetTenantID(tenantID).
			SetAuthorID(seededAuthor).
			SetTitle(a.Title).
			SetSlug(a.Slug).
			SetContent(a.Content).
			SetStatus(kbarticle.StatusPublished).
			SetIsPublic(true).
			SetPublishedAt(now).
			SetMetadata(map[string]any{"seeded_from": "ticketing-seed", "kb_set": set})
		if a.Excerpt != "" {
			q.SetExcerpt(a.Excerpt)
		}
		if a.Category != "" {
			q.SetCategory(a.Category)
		}
		if len(a.Tags) > 0 {
			q.SetTags(a.Tags)
		}
		if _, err := q.Save(ctx); err != nil {
			return created, skipped, fmt.Errorf("create article %q: %w", a.Slug, err)
		}
		log.Printf("  created article %q", a.Slug)
		created++
	}
	return created, skipped, nil
}
