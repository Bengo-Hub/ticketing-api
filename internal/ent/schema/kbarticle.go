package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// KBArticle holds the schema definition for knowledge base articles.
type KBArticle struct {
	ent.Schema
}

// Fields of the KBArticle.
func (KBArticle) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.UUID("author_id", uuid.UUID{}).
			Comment("Agent who authored the article"),
		field.String("title").
			MaxLen(255).
			NotEmpty().
			Comment("Article title"),
		field.String("slug").
			MaxLen(255).
			NotEmpty().
			Comment("URL-friendly slug (unique per tenant)"),
		field.Text("content").
			NotEmpty().
			Comment("Article body in Markdown"),
		field.String("excerpt").
			MaxLen(500).
			Optional().
			Comment("Short summary shown in search results"),
		field.String("category").
			MaxLen(100).
			Optional().
			Comment("Top-level category label"),
		field.JSON("tags", []string{}).
			Optional().
			Comment("Searchable tags"),
		field.Enum("status").
			Values("draft", "published", "archived").
			Default("draft").
			Comment("Publication state"),
		field.Bool("is_public").
			Default(true).
			Comment("Visible to customers vs internal agents only"),
		field.Int("view_count").
			Default(0).
			Comment("Cumulative view count"),
		field.Int("helpful_count").
			Default(0).
			Comment("Cumulative helpful votes"),
		field.Int("not_helpful_count").
			Default(0).
			Comment("Cumulative not-helpful votes"),
		field.JSON("metadata", map[string]any{}).
			Optional().
			Comment("Extra metadata"),
		field.Time("published_at").
			Optional().
			Nillable().
			Comment("When the article was first published"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.Time("deleted_at").
			Optional().
			Nillable().
			Comment("Soft delete timestamp"),
	}
}

// Edges of the KBArticle.
func (KBArticle) Edges() []ent.Edge {
	return []ent.Edge{}
}

// Annotations of the KBArticle.
func (KBArticle) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "kb_articles",
		},
	}
}

// Indexes of the KBArticle.
func (KBArticle) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "slug").Unique(),
		index.Fields("tenant_id", "category"),
		index.Fields("tenant_id", "is_public"),
		trgmIndex("title", "kbarticle_title_trgm"),
		trgmIndex("excerpt", "kbarticle_excerpt_trgm"),
		trgmIndex("content", "kbarticle_content_trgm"),
		index.Fields("author_id"),
		index.Fields("created_at"),
		index.Fields("deleted_at"),
	}
}
