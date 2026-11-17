package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// RollupWatermark is how far a rollup job has read. The ticket rollup reads tickets in
// (updated_at, id) order across tenants and keeps its place here, so each run recomputes only the
// buckets touched since the last one. The first run starts at the epoch, which is the backfill.
type RollupWatermark struct {
	ent.Schema
}

// Fields of the RollupWatermark.
func (RollupWatermark) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			MaxLen(100).
			Unique().
			Immutable().
			Comment("The job, for example ticket_rollups"),
		field.Time("watermark_at").
			Comment("updated_at of the last ticket read"),
		field.UUID("watermark_id", uuid.UUID{}).
			Comment("id of the last ticket read, the tie-break within one updated_at"),
		field.Time("caught_up_at").
			Optional().
			Nillable().
			Comment("When a run last read everything up to its settle horizon; null while the backfill runs"),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Annotations of the RollupWatermark.
func (RollupWatermark) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "rollup_watermarks"}}
}
