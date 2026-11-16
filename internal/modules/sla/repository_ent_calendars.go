package sla

import (
	"context"

	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	entcal "github.com/bengobox/ticketing-service/internal/ent/businesscalendar"
	entpkg "github.com/bengobox/ticketing-service/internal/ent/slapolicy"
)

func (r *EntRepository) CreateCalendar(ctx context.Context, c *Calendar) error {
	row, err := r.client.BusinessCalendar.Create().
		SetID(c.ID).
		SetTenantID(c.TenantID).
		SetName(c.Name).
		SetTimezone(c.Timezone).
		SetWeeklyHours(c.WeeklyHours).
		SetHolidays(c.Holidays).
		SetIsDefault(c.IsDefault).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ErrCalendarExists
		}
		return err
	}
	c.CreatedAt, c.UpdatedAt = row.CreatedAt, row.UpdatedAt
	return nil
}

func (r *EntRepository) GetCalendar(ctx context.Context, tenantID, calendarID uuid.UUID) (*Calendar, error) {
	row, err := r.client.BusinessCalendar.Query().
		Where(entcal.ID(calendarID), entcal.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrCalendarNotFound
		}
		return nil, err
	}
	return toCalendar(row), nil
}

func (r *EntRepository) ListCalendars(ctx context.Context, filter CalendarFilter) ([]*Calendar, int, error) {
	q := r.client.BusinessCalendar.Query().Where(entcal.TenantID(filter.TenantID))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := q.Order(ent.Desc(entcal.FieldIsDefault), ent.Asc(entcal.FieldName)).
		Offset(filter.Offset).Limit(filter.Limit).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*Calendar, len(rows))
	for i, row := range rows {
		out[i] = toCalendar(row)
	}
	return out, total, nil
}

func (r *EntRepository) CountCalendars(ctx context.Context, tenantID uuid.UUID) (int, error) {
	return r.client.BusinessCalendar.Query().Where(entcal.TenantID(tenantID)).Count(ctx)
}

func (r *EntRepository) SaveCalendar(ctx context.Context, c *Calendar) error {
	n, err := r.client.BusinessCalendar.Update().
		Where(entcal.ID(c.ID), entcal.TenantID(c.TenantID)).
		SetName(c.Name).
		SetTimezone(c.Timezone).
		SetWeeklyHours(c.WeeklyHours).
		SetHolidays(c.Holidays).
		SetIsDefault(c.IsDefault).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ErrCalendarExists
		}
		return err
	}
	if n == 0 {
		return ErrCalendarNotFound
	}
	return nil
}

func (r *EntRepository) DeleteCalendar(ctx context.Context, tenantID, calendarID uuid.UUID) error {
	n, err := r.client.BusinessCalendar.Delete().
		Where(entcal.ID(calendarID), entcal.TenantID(tenantID)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCalendarNotFound
	}
	return nil
}

func (r *EntRepository) ClearDefaultCalendar(ctx context.Context, tenantID, keepID uuid.UUID) error {
	_, err := r.client.BusinessCalendar.Update().
		Where(entcal.TenantID(tenantID), entcal.IsDefault(true), entcal.IDNEQ(keepID)).
		SetIsDefault(false).
		Save(ctx)
	return err
}

func (r *EntRepository) CalendarInUse(ctx context.Context, tenantID, calendarID uuid.UUID) (bool, error) {
	return r.client.SLAPolicy.Query().
		Where(entpkg.TenantID(tenantID), entpkg.CalendarID(calendarID)).
		Exist(ctx)
}

func toCalendar(row *ent.BusinessCalendar) *Calendar {
	return &Calendar{
		ID:          row.ID,
		TenantID:    row.TenantID,
		Name:        row.Name,
		Timezone:    row.Timezone,
		WeeklyHours: row.WeeklyHours,
		Holidays:    row.Holidays,
		IsDefault:   row.IsDefault,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
