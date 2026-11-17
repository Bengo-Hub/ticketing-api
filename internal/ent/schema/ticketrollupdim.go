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

// TicketRollupDim is the compact rollup the common analytics paths read, derived by the worker
// from the fine cells of ticket_rollups with GROUPING SETS. Per tenant and bucket it holds:
//
//   - one total row (dim_kind all);
//   - one row per value of each single dimension (team, agent, category, channel, priority);
//   - on day rows only, one row per team and value of each other dimension (team_agent,
//     team_category, team_channel, team_priority), with the team in team_id.
//
// A view with no filter or one filter reads only single rows. A team filter on a window longer
// than 31 days (day rows) reads its breakdowns from the team pair rows, and a team filter with one
// other filter reads its totals and trend from them, so long team windows never read fine cells
// except for a breakdown by a third dimension. Hour rows have no pairs: short windows read few
// fine cells, and pairs would triple the hour rows.
//
// The counters mean exactly what they mean in ticket_rollups. team_id is the zero UUID except on
// pair rows. dim_value is the dimension's value: a UUID as text (the zero UUID for none), the
// channel or priority, or empty for all.
type TicketRollupDim struct {
	ent.Schema
}

// Fields of the TicketRollupDim.
func (TicketRollupDim) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Immutable(),
		field.Enum("granularity").
			Values("hour", "day").
			Immutable(),
		field.Time("bucket_start").
			Immutable().
			Comment("Start of the hour or UTC day"),
		field.Enum("dim_kind").
			Values("all", "team", "agent", "category", "channel", "priority",
				"team_agent", "team_category", "team_channel", "team_priority").
			Immutable(),
		field.UUID("team_id", uuid.UUID{}).
			Immutable().
			Comment("The team of a pair row; the zero UUID on every other row"),
		field.String("dim_value").
			MaxLen(64).
			Default("").
			Immutable(),
		field.Int("created_count").Default(0),
		field.Int("resolved_count").Default(0),
		field.Int("first_response_count").Default(0),
		field.Int64("first_response_minutes").Default(0),
		field.Int64("resolution_minutes").Default(0),
		field.Int("fr_met").Default(0),
		field.Int("fr_breached").Default(0),
		field.Int("res_met").Default(0),
		field.Int("res_breached").Default(0),
		field.Int("csat_1").Default(0),
		field.Int("csat_2").Default(0),
		field.Int("csat_3").Default(0),
		field.Int("csat_4").Default(0),
		field.Int("csat_5").Default(0),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the TicketRollupDim.
//   - ticketrollupdim_key serves one series over a window (the total, one filter value, or a team
//     and one other value): every equality, then the bucket range. It is also the upsert key.
//   - ticketrollupdim_kind serves an unfiltered breakdown: every value of one kind over a window.
//   - ticketrollupdim_pair serves a team-filtered breakdown: every value of one pair kind for one
//     team over a window.
//   - ticketrollupdim_bucket serves the worker's delete, by bucket.
func (TicketRollupDim) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "granularity", "dim_kind", "team_id", "dim_value", "bucket_start").
			Unique().
			StorageKey("ticketrollupdim_key"),
		index.Fields("tenant_id", "granularity", "dim_kind", "bucket_start").StorageKey("ticketrollupdim_kind"),
		index.Fields("tenant_id", "granularity", "dim_kind", "team_id", "bucket_start").StorageKey("ticketrollupdim_pair"),
		index.Fields("tenant_id", "granularity", "bucket_start").StorageKey("ticketrollupdim_bucket"),
	}
}

// Annotations of the TicketRollupDim.
func (TicketRollupDim) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticket_rollup_dims"}}
}
