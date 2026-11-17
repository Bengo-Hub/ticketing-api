package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Ticket holds the schema definition for the Ticket entity.
// Core ticket entity for customer support requests.
type Ticket struct {
	ent.Schema
}

// Fields of the Ticket.
func (Ticket) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Reference to tenant"),
		field.String("ticket_number").
			MaxLen(20).
			Comment("Human-readable ticket number, unique within the tenant"),
		field.UUID("customer_id", uuid.UUID{}).
			Comment("Customer who created/owns the ticket"),
		field.UUID("category_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Ticket category"),
		field.UUID("assigned_agent_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Currently assigned agent"),
		field.UUID("assigned_team_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Currently assigned team"),
		field.String("subject").
			MaxLen(255).
			NotEmpty().
			Comment("Ticket subject line"),
		field.Text("description").
			NotEmpty().
			Comment("Initial ticket description"),
		field.Enum("status").
			Values("new", "open", "in_progress", "pending_customer", "pending_internal", "resolved", "closed", "cancelled").
			Default("new").
			Comment("Ticket status"),
		field.Enum("priority").
			Values("low", "normal", "high", "urgent", "critical").
			Default("normal").
			Comment("Ticket priority"),
		field.Enum("channel").
			Values("web", "email", "api", "chat", "whatsapp", "phone", "internal").
			Default("web").
			Comment("Channel ticket was created through"),
		// External references for integration
		field.UUID("order_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Related order ID (from ordering/POS service)"),
		field.String("external_reference").
			MaxLen(255).
			Optional().
			Comment("External reference ID"),
		// CRM references
		field.UUID("crm_contact_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("MarketFlow CRM contact reference — never duplicate contact data here"),
		field.UUID("crm_lead_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("MarketFlow CRM lead reference"),
		// SLA tracking
		field.Time("sla_response_due_at").
			Optional().
			Nillable().
			Comment("SLA first response deadline"),
		field.Time("sla_resolution_due_at").
			Optional().
			Nillable().
			Comment("SLA resolution deadline"),
		field.Bool("sla_response_breached").
			Default(false).
			Comment("Whether first response SLA was breached"),
		field.Bool("sla_resolution_breached").
			Default(false).
			Comment("Whether resolution SLA was breached"),
		field.UUID("sla_policy_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("SLA policy that set the deadlines; the worker joins it for warnings and escalation"),
		field.Time("sla_paused_at").
			Optional().
			Nillable().
			Comment("When the SLA clock stopped (pending status); null while it runs"),
		field.Int("sla_paused_minutes").
			Default(0).
			Comment("Total minutes the SLA clock was stopped, in the policy's clock"),
		field.Int("sla_first_response_minutes").
			Optional().
			Nillable().
			Comment("Minutes from creation to first response in the policy's clock, net of pauses"),
		field.Int("sla_resolution_minutes").
			Optional().
			Nillable().
			Comment("Minutes from creation to resolution in the policy's clock, net of pauses"),
		// Timestamps
		field.Time("first_response_at").
			Optional().
			Nillable().
			Comment("When first agent response was made"),
		field.Time("resolved_at").
			Optional().
			Nillable().
			Comment("When ticket was resolved"),
		field.Time("closed_at").
			Optional().
			Nillable().
			Comment("When ticket was closed"),
		field.Time("reopened_at").
			Optional().
			Nillable().
			Comment("When ticket was last reopened"),
		// Escalation
		field.Int("escalation_level").
			Default(0).
			Comment("Current escalation level"),
		field.Time("escalated_at").
			Optional().
			Nillable().
			Comment("When ticket was last escalated"),
		// Metadata
		field.JSON("tags", []string{}).
			Optional().
			Comment("Ticket tags for organization"),
		field.JSON("metadata", map[string]any{}).
			Optional().
			Comment("Additional ticket metadata"),
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

// Edges of the Ticket.
func (Ticket) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("category", TicketCategory.Type).
			Ref("tickets").
			Unique().
			Field("category_id"),
		edge.From("assigned_agent", Agent.Type).
			Ref("assigned_tickets").
			Unique().
			Field("assigned_agent_id"),
		edge.To("comments", TicketComment.Type),
		edge.To("attachments", TicketAttachment.Type),
	}
}

// Annotations of the Ticket.
func (Ticket) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "tickets",
		},
	}
}

// Indexes of the Ticket.
func (Ticket) Indexes() []ent.Index {
	// The internal-create idempotency key (tenant_id, metadata source, source_ref.type, source_ref.id) is a
	// unique expression index that ent cannot declare; it lives in the versioned migration
	// 20261116090100_phase0.sql as ticket_tenant_source_ref. The requester search index
	// ticket_requester_trgm is an expression index too and lives in 20261116100000_sprint9.sql.
	// ticket_tenant_requester_phone (tenant_id, metadata requester_phone), the WhatsApp threading
	// lookup, lives in 20261117120000_sprint11.sql. ticket_tenant_sla_due (tenant_id, SLA rank,
	// SLA due key, id) WHERE deleted_at IS NULL, the sort=sla_due order, is an expression index
	// too; its exact definition is in internal/modules/tickets/sla_sort.go.
	return []ent.Index{
		index.Fields("tenant_id", "ticket_number").Unique(),
		// Keyset paging of the ticket list: (created_at, id) within the tenant.
		index.Fields("tenant_id", "created_at", "id"),
		index.Fields("tenant_id", "channel"),
		index.Fields("tenant_id", "category_id"),
		// The analytics rollup recomputes an hour from each moment a ticket can count at: created_at
		// (above), resolved_at, first_response_at and the two SLA deadlines. resolved_at also
		// serves the live resolved_today figure.
		index.Fields("tenant_id", "resolved_at"),
		index.Fields("tenant_id", "first_response_at"),
		index.Fields("tenant_id", "sla_response_due_at"),
		index.Fields("tenant_id", "sla_resolution_due_at"),
		// The rollup reads changed tickets across tenants in (updated_at, id) order from its watermark.
		index.Fields("updated_at", "id"),
		// The live backlog (open by status, overdue, unassigned, open per agent) is an index-only
		// scan of the open tickets: every column those counts read, the five dimension filters
		// included, is in the index, and the predicate matches the queries' WHERE
		// (analytics.openStatuses). A team or any other filter therefore never visits the heap.
		index.Fields("tenant_id", "status", "assigned_team_id", "assigned_agent_id", "category_id", "channel", "priority",
			"sla_resolution_due_at", "sla_paused_at").
			StorageKey("ticket_open_backlog").
			// Written in Postgres' own normalised form so Atlas sees no drift on replay (the IN form
			// is rewritten differently by each pass). The planner still matches the queries'
			// "status IN (...)" against it: verified as an index-only scan on 2026-11-17.
			Annotations(entsql.IndexWhere("((deleted_at IS NULL) AND ((status)::text = ANY ('{new,open,in_progress,pending_customer,pending_internal}'::text[])))")),
		trgmIndex("subject", "ticket_subject_trgm"),
		trgmIndex("description", "ticket_description_trgm"),
		ginIndex("tags", "ticket_tags_gin"),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "priority"),
		index.Fields("tenant_id", "customer_id"),
		index.Fields("tenant_id", "assigned_agent_id"),
		index.Fields("tenant_id", "assigned_team_id"),
		index.Fields("category_id"),
		index.Fields("order_id"),
		index.Fields("external_reference"),
		index.Fields("crm_contact_id"),
		index.Fields("crm_lead_id"),
		index.Fields("sla_response_due_at"),
		index.Fields("sla_resolution_due_at"),
		index.Fields("created_at"),
		index.Fields("deleted_at"),
	}
}
