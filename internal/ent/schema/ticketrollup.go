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

// TicketRollup is one cell of the analytics cube: the ticket figures of one tenant for one hour or
// one UTC day, for one combination of team, agent, category, channel and priority (the ticket's
// values when the cell was computed). ticketing-worker fills it and derives the compact
// ticket_rollup_dims from it. The /analytics routes read these fine cells only for a query with two
// or more filters, or a breakdown by one dimension under a filter on another; every other path
// reads ticket_rollup_dims.
//
// Each figure is dated by its own moment: created by created_at, resolved by resolved_at (so a
// ticket resolved today counts today whatever day it was created), first responses by
// first_response_at, a met target when it was met, a breach by its deadline, and CSAT by the
// response's submitted_at. An unassigned team or agent and a ticket with no category are the zero
// UUID, so the cell key has no nulls and one unique index covers it.
type TicketRollup struct {
	ent.Schema
}

// Fields of the TicketRollup.
func (TicketRollup) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Immutable(),
		field.Enum("granularity").
			Values("hour", "day").
			Immutable().
			Comment("hour cells come from tickets; day cells are the sum of a UTC day's hour cells"),
		field.Time("bucket_start").
			Immutable().
			Comment("Start of the hour or UTC day"),
		field.UUID("team_id", uuid.UUID{}).
			Immutable().
			Comment("Assigned team; the zero UUID when none"),
		field.UUID("agent_id", uuid.UUID{}).
			Immutable().
			Comment("Assigned agent; the zero UUID when none"),
		field.UUID("category_id", uuid.UUID{}).
			Immutable().
			Comment("Category; the zero UUID when none"),
		field.String("channel").
			MaxLen(20).
			Immutable(),
		field.String("priority").
			MaxLen(20).
			Immutable(),
		field.Int("created_count").
			Default(0).
			Comment("Tickets created in the bucket"),
		field.Int("resolved_count").
			Default(0).
			Comment("Tickets whose resolved_at is in the bucket"),
		field.Int("first_response_count").
			Default(0).
			Comment("Tickets whose first_response_at is in the bucket"),
		field.Int64("first_response_minutes").
			Default(0).
			Comment("Sum of wall-clock minutes from creation to first response of those tickets"),
		field.Int64("resolution_minutes").
			Default(0).
			Comment("Sum of wall-clock minutes from creation to resolution of the tickets resolved in the bucket"),
		field.Int("fr_met").
			Default(0).
			Comment("First responses in the bucket with a deadline and no breach"),
		field.Int("fr_breached").
			Default(0).
			Comment("Breached first-response deadlines that fell in the bucket"),
		field.Int("res_met").
			Default(0).
			Comment("Resolutions in the bucket with a deadline and no breach"),
		field.Int("res_breached").
			Default(0).
			Comment("Breached resolution deadlines that fell in the bucket"),
		field.Int("csat_1").Default(0).Comment("CSAT responses scoring 1 submitted in the bucket"),
		field.Int("csat_2").Default(0),
		field.Int("csat_3").Default(0),
		field.Int("csat_4").Default(0),
		field.Int("csat_5").Default(0),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the TicketRollup. The cell key leads with (tenant_id, granularity, bucket_start), so
// it serves every unfiltered window and the worker's upsert; each filter has its own index ending
// in bucket_start, so a filtered window is one range scan.
func (TicketRollup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "granularity", "bucket_start", "team_id", "agent_id", "category_id", "channel", "priority").
			Unique().
			StorageKey("ticketrollup_cell"),
		index.Fields("tenant_id", "granularity", "team_id", "bucket_start").StorageKey("ticketrollup_team"),
		index.Fields("tenant_id", "granularity", "agent_id", "bucket_start").StorageKey("ticketrollup_agent"),
		index.Fields("tenant_id", "granularity", "category_id", "bucket_start").StorageKey("ticketrollup_category"),
		index.Fields("tenant_id", "granularity", "channel", "bucket_start").StorageKey("ticketrollup_channel"),
		index.Fields("tenant_id", "granularity", "priority", "bucket_start").StorageKey("ticketrollup_priority"),
		// A team filter combined with a second filter reads only the cells of both values. Team
		// with agent needs none: the agent index is already narrower than the team's.
		index.Fields("tenant_id", "granularity", "team_id", "category_id", "bucket_start").StorageKey("ticketrollup_team_category"),
		index.Fields("tenant_id", "granularity", "team_id", "channel", "bucket_start").StorageKey("ticketrollup_team_channel"),
		index.Fields("tenant_id", "granularity", "team_id", "priority", "bucket_start").StorageKey("ticketrollup_team_priority"),
	}
}

// Annotations of the TicketRollup.
func (TicketRollup) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticket_rollups"}}
}
