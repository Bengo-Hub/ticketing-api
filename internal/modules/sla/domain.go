package sla

import (
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
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

// Policy defines SLA response and resolution targets for a priority. Both targets count in the
// policy's calendar when CalendarID is set, and every minute (24x7) when it is null. Metadata
// holds notify_before_breach_min, pause_on_pending_internal and escalation_rules (clock.Settings).
type Policy struct {
	ID                   uuid.UUID      `json:"id"`
	TenantID             uuid.UUID      `json:"tenant_id"`
	Name                 string         `json:"name"`
	Description          string         `json:"description,omitempty"`
	Priority             Priority       `json:"priority"`
	CategoryID           *uuid.UUID     `json:"category_id,omitempty"`
	CalendarID           *uuid.UUID     `json:"calendar_id"`
	FirstResponseMinutes int            `json:"first_response_minutes"`
	ResolutionMinutes    int            `json:"resolution_minutes"`
	IsActive             bool           `json:"is_active"`
	SortOrder            int            `json:"sort_order"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

// DeadlineResult is the computed SLA deadlines for a ticket.
type DeadlineResult struct {
	PolicyID        uuid.UUID
	ResponseDueAt   time.Time
	ResolutionDueAt time.Time
}

// PolicyClock is the clock a ticket's SLA counts in and whether pending_internal stops it.
type PolicyClock struct {
	Calendar               *clock.Calendar
	PauseOnPendingInternal bool
}

// CreatePolicyRequest is the input for creating an SLA policy.
type CreatePolicyRequest struct {
	TenantID             uuid.UUID      `json:"-"`
	Name                 string         `json:"name"`
	Description          string         `json:"description,omitempty"`
	Priority             Priority       `json:"priority"`
	CategoryID           *uuid.UUID     `json:"category_id,omitempty"`
	CalendarID           *uuid.UUID     `json:"calendar_id,omitempty"`
	FirstResponseMinutes int            `json:"first_response_minutes"`
	ResolutionMinutes    int            `json:"resolution_minutes"`
	SortOrder            int            `json:"sort_order,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

// UpdatePolicyRequest is the input for updating an SLA policy. ClearCalendar switches the policy
// back to 24x7.
type UpdatePolicyRequest struct {
	Name                 *string        `json:"name,omitempty"`
	Description          *string        `json:"description,omitempty"`
	Priority             *Priority      `json:"priority,omitempty"`
	CategoryID           *uuid.UUID     `json:"category_id,omitempty"`
	CalendarID           *uuid.UUID     `json:"calendar_id,omitempty"`
	ClearCalendar        bool           `json:"clear_calendar,omitempty"`
	FirstResponseMinutes *int           `json:"first_response_minutes,omitempty"`
	ResolutionMinutes    *int           `json:"resolution_minutes,omitempty"`
	IsActive             *bool          `json:"is_active,omitempty"`
	SortOrder            *int           `json:"sort_order,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

// PolicyFilter is used to filter SLA policy list queries.
type PolicyFilter struct {
	TenantID uuid.UUID
	Priority *Priority
	IsActive *bool
	Offset   int
	Limit    int
}

// Calendar is a tenant's business calendar.
type Calendar struct {
	ID          uuid.UUID       `json:"id"`
	TenantID    uuid.UUID       `json:"tenant_id"`
	Name        string          `json:"name"`
	Timezone    string          `json:"timezone"`
	WeeklyHours clock.Week      `json:"weekly_hours"`
	Holidays    []clock.Holiday `json:"holidays"`
	IsDefault   bool            `json:"is_default"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// CalendarInput creates a calendar or, with nil fields left alone, updates one. A nil
// WeeklyHours on create is Monday to Friday, 08:00 to 17:00; an empty Timezone is Africa/Nairobi.
type CalendarInput struct {
	Name        *string          `json:"name,omitempty"`
	Timezone    *string          `json:"timezone,omitempty"`
	WeeklyHours clock.Week       `json:"weekly_hours,omitempty"`
	Holidays    *[]clock.Holiday `json:"holidays,omitempty"`
	IsDefault   *bool            `json:"is_default,omitempty"`
}

// CalendarFilter pages a tenant's calendars by name.
type CalendarFilter struct {
	TenantID uuid.UUID
	Offset   int
	Limit    int
}
