package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

// BusinessCalendar is a tenant's working week and holidays in one timezone. An SLA policy that
// names a calendar counts its targets in that calendar's business time.
type BusinessCalendar struct {
	ent.Schema
}

// Fields of the BusinessCalendar.
func (BusinessCalendar) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Comment("Calendar name, unique within the tenant"),
		field.String("timezone").
			MaxLen(64).
			Default(clock.DefaultTimezone).
			Comment("IANA timezone the working hours and holidays are in"),
		field.JSON("weekly_hours", clock.Week{}).
			Comment("Working periods per weekday (mon to sun), HH:MM local time"),
		field.JSON("holidays", []clock.Holiday{}).
			Optional().
			Comment("Dates with no business time, with names; moon_dependent marks dates to confirm"),
		field.Bool("is_default").
			Default(false).
			Comment("The calendar the admin pages and the seed preselect"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Annotations of the BusinessCalendar.
func (BusinessCalendar) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "business_calendars"}}
}

// Indexes of the BusinessCalendar.
func (BusinessCalendar) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").Unique(),
		index.Fields("tenant_id", "is_default"),
	}
}
