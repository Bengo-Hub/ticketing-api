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
			Unique().
			Comment("Human-readable ticket number"),
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
			Values("web", "email", "api", "chat", "phone", "internal").
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
	return []ent.Index{
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "priority"),
		index.Fields("tenant_id", "customer_id"),
		index.Fields("tenant_id", "assigned_agent_id"),
		index.Fields("tenant_id", "assigned_team_id"),
		index.Fields("category_id"),
		index.Fields("order_id"),
		index.Fields("external_reference"),
		index.Fields("sla_response_due_at"),
		index.Fields("sla_resolution_due_at"),
		index.Fields("created_at"),
		index.Fields("deleted_at"),
	}
}
