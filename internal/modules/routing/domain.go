package routing

import (
	"time"

	"github.com/google/uuid"
)

// ConditionField is the ticket field to match against.
type ConditionField string

const (
	FieldPriority           ConditionField = "priority"
	FieldCategoryID         ConditionField = "category_id"
	FieldChannel            ConditionField = "channel"
	FieldSubjectContains    ConditionField = "subject_contains"
	FieldDescriptionContains ConditionField = "description_contains"
)

// ConditionOperator is how to compare a field value.
type ConditionOperator string

const (
	OperatorEq       ConditionOperator = "eq"
	OperatorIn       ConditionOperator = "in"
	OperatorContains ConditionOperator = "contains"
)

// ActionType is what to do when a rule matches.
type ActionType string

const (
	ActionAssignTeam  ActionType = "assign_team"
	ActionAssignAgent ActionType = "assign_agent"
	ActionSetPriority ActionType = "set_priority"
)

// Condition is a single match condition in a routing rule.
type Condition struct {
	Field    ConditionField    `json:"field"`
	Operator ConditionOperator `json:"operator"`
	Value    any               `json:"value"`
}

// Action specifies what to do when a rule fires.
type Action struct {
	Type  ActionType `json:"type"`
	Value any        `json:"value"` // UUID string (team/agent) or priority string
}

// RoutingRule is a tenant-scoped auto-routing rule for tickets.
type RoutingRule struct {
	ID          uuid.UUID   `json:"id"`
	TenantID    uuid.UUID   `json:"tenant_id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	IsActive    bool        `json:"is_active"`
	SortOrder   int         `json:"sort_order"`
	Conditions  []Condition `json:"conditions"`
	Action      Action      `json:"action"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// TicketContext holds the ticket fields used to evaluate routing conditions.
type TicketContext struct {
	TenantID    uuid.UUID
	Priority    string
	CategoryID  *uuid.UUID
	Channel     string
	Subject     string
	Description string
}

// AppliedAction is the result of evaluating routing rules against a ticket.
type AppliedAction struct {
	RuleID    uuid.UUID
	RuleName  string
	Action    Action
}

// CreateRuleRequest is the input for creating a routing rule.
type CreateRuleRequest struct {
	TenantID    uuid.UUID   `json:"-"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	IsActive    bool        `json:"is_active"`
	SortOrder   int         `json:"sort_order,omitempty"`
	Conditions  []Condition `json:"conditions"`
	Action      Action      `json:"action"`
}

// UpdateRuleRequest is the input for updating a routing rule.
type UpdateRuleRequest struct {
	Name        *string      `json:"name,omitempty"`
	Description *string      `json:"description,omitempty"`
	IsActive    *bool        `json:"is_active,omitempty"`
	SortOrder   *int         `json:"sort_order,omitempty"`
	Conditions  []Condition  `json:"conditions,omitempty"`
	Action      *Action      `json:"action,omitempty"`
}

// RuleFilter is used to filter routing rule list queries.
type RuleFilter struct {
	TenantID uuid.UUID
	IsActive *bool
	Offset   int
	Limit    int
}
