package sla

import (
	"time"

	"github.com/google/uuid"
)

// Priority mirrors tickets.TicketPriority to avoid a circular dependency.
type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityNormal   Priority = "normal"
	PriorityHigh     Priority = "high"
	PriorityUrgent   Priority = "urgent"
	PriorityCritical Priority = "critical"
)

// Policy defines SLA response and resolution time targets for a given priority.
type Policy struct {
	ID                          uuid.UUID      `json:"id"`
	TenantID                    uuid.UUID      `json:"tenant_id"`
	Name                        string         `json:"name"`
	Description                 string         `json:"description,omitempty"`
	Priority                    Priority       `json:"priority"`
	CategoryID                  *uuid.UUID     `json:"category_id,omitempty"`
	FirstResponseMinutes        int            `json:"first_response_minutes"`
	FirstResponseBusinessHours  bool           `json:"first_response_business_hours"`
	ResolutionMinutes           int            `json:"resolution_minutes"`
	ResolutionBusinessHours     bool           `json:"resolution_business_hours"`
	IsActive                    bool           `json:"is_active"`
	SortOrder                   int            `json:"sort_order"`
	BusinessHours               map[string]any `json:"business_hours,omitempty"`
	Metadata                    map[string]any `json:"metadata,omitempty"`
	CreatedAt                   time.Time      `json:"created_at"`
	UpdatedAt                   time.Time      `json:"updated_at"`
}

// DeadlineResult is the computed SLA deadlines for a ticket.
type DeadlineResult struct {
	PolicyID       uuid.UUID
	ResponseDueAt  time.Time
	ResolutionDueAt time.Time
}

// CreatePolicyRequest is the input for creating an SLA policy.
type CreatePolicyRequest struct {
	TenantID                   uuid.UUID      `json:"-"`
	Name                       string         `json:"name"`
	Description                string         `json:"description,omitempty"`
	Priority                   Priority       `json:"priority"`
	CategoryID                 *uuid.UUID     `json:"category_id,omitempty"`
	FirstResponseMinutes       int            `json:"first_response_minutes"`
	FirstResponseBusinessHours bool           `json:"first_response_business_hours"`
	ResolutionMinutes          int            `json:"resolution_minutes"`
	ResolutionBusinessHours    bool           `json:"resolution_business_hours"`
	SortOrder                  int            `json:"sort_order,omitempty"`
	BusinessHours              map[string]any `json:"business_hours,omitempty"`
	Metadata                   map[string]any `json:"metadata,omitempty"`
}

// UpdatePolicyRequest is the input for updating an SLA policy.
type UpdatePolicyRequest struct {
	Name                       *string        `json:"name,omitempty"`
	Description                *string        `json:"description,omitempty"`
	Priority                   *Priority      `json:"priority,omitempty"`
	CategoryID                 *uuid.UUID     `json:"category_id,omitempty"`
	FirstResponseMinutes       *int           `json:"first_response_minutes,omitempty"`
	FirstResponseBusinessHours *bool          `json:"first_response_business_hours,omitempty"`
	ResolutionMinutes          *int           `json:"resolution_minutes,omitempty"`
	ResolutionBusinessHours    *bool          `json:"resolution_business_hours,omitempty"`
	IsActive                   *bool          `json:"is_active,omitempty"`
	SortOrder                  *int           `json:"sort_order,omitempty"`
	BusinessHours              map[string]any `json:"business_hours,omitempty"`
	Metadata                   map[string]any `json:"metadata,omitempty"`
}

// PolicyFilter is used to filter SLA policy list queries.
type PolicyFilter struct {
	TenantID uuid.UUID
	Priority *Priority
	IsActive *bool
	Offset   int
	Limit    int
}
