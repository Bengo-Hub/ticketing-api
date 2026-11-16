package sla

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the data access for SLA policies and business calendars. Every method is scoped
// to the tenant.
type Repository interface {
	Create(ctx context.Context, policy *Policy) error
	Get(ctx context.Context, tenantID, policyID uuid.UUID) (*Policy, error)
	List(ctx context.Context, filter PolicyFilter) ([]*Policy, int, error)
	Update(ctx context.Context, tenantID, policyID uuid.UUID, req *UpdatePolicyRequest) error
	Delete(ctx context.Context, tenantID, policyID uuid.UUID) error

	// FindMatchingPolicy returns the best-matching active policy for a given
	// priority and optional category. Lower sort_order wins.
	FindMatchingPolicy(ctx context.Context, tenantID uuid.UUID, priority Priority, categoryID *uuid.UUID) (*Policy, error)

	// TeamExists reports whether the team belongs to the tenant (escalation rules name teams).
	TeamExists(ctx context.Context, tenantID, teamID uuid.UUID) (bool, error)

	CreateCalendar(ctx context.Context, c *Calendar) error
	GetCalendar(ctx context.Context, tenantID, calendarID uuid.UUID) (*Calendar, error)
	ListCalendars(ctx context.Context, filter CalendarFilter) ([]*Calendar, int, error)
	CountCalendars(ctx context.Context, tenantID uuid.UUID) (int, error)
	// SaveCalendar writes every field of an existing calendar.
	SaveCalendar(ctx context.Context, c *Calendar) error
	DeleteCalendar(ctx context.Context, tenantID, calendarID uuid.UUID) error
	// ClearDefaultCalendar unsets is_default on the tenant's calendars other than keepID.
	ClearDefaultCalendar(ctx context.Context, tenantID, keepID uuid.UUID) error
	// CalendarInUse reports whether a policy of the tenant names the calendar.
	CalendarInUse(ctx context.Context, tenantID, calendarID uuid.UUID) (bool, error)
}
