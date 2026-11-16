package sla

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

// DefaultCalendarName is the name of the calendar EnsureDefaultCalendar creates.
const DefaultCalendarName = "Business hours"

func invalidCalendar(err error) error {
	return fmt.Errorf("%w: %s", ErrInvalidCalendar, strings.TrimPrefix(err.Error(), clock.ErrInvalidCalendar.Error()+": "))
}

// validateCalendar checks the fields a calendar is stored with.
func validateCalendar(c *Calendar) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 100 {
		return fmt.Errorf("%w: name is required, up to 100 characters", ErrInvalidCalendar)
	}
	if _, err := clock.New(c.Timezone, c.WeeklyHours, c.Holidays); err != nil {
		return invalidCalendar(err)
	}
	return nil
}

// CreateCalendar creates a calendar. A tenant's first calendar becomes its default and gets the
// Kenyan public holidays of this year and next, with the holidays in the request taking their
// dates' place.
func (s *Service) CreateCalendar(ctx context.Context, tenantID uuid.UUID, in CalendarInput) (*Calendar, error) {
	c := &Calendar{ID: uuid.New(), TenantID: tenantID, Timezone: clock.DefaultTimezone, WeeklyHours: in.WeeklyHours}
	if in.Name != nil {
		c.Name = *in.Name
	}
	if in.Timezone != nil && *in.Timezone != "" {
		c.Timezone = *in.Timezone
	}
	if c.WeeklyHours == nil {
		c.WeeklyHours = clock.DefaultWeek()
	}
	var holidays []clock.Holiday
	if in.Holidays != nil {
		holidays = *in.Holidays
	}
	c.IsDefault = in.IsDefault != nil && *in.IsDefault
	count, err := s.repo.CountCalendars(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		year := yearIn(s.now(), c.Timezone)
		seeded := append(clock.KenyaHolidays(year), clock.KenyaHolidays(year+1)...)
		holidays = clock.MergeHolidays(seeded, holidays)
		c.IsDefault = true
	}
	c.Holidays = holidays
	if c.Holidays == nil {
		c.Holidays = []clock.Holiday{}
	}
	if err := validateCalendar(c); err != nil {
		return nil, err
	}
	if err := s.repo.CreateCalendar(ctx, c); err != nil {
		return nil, err
	}
	if c.IsDefault {
		if err := s.repo.ClearDefaultCalendar(ctx, tenantID, c.ID); err != nil {
			return nil, err
		}
	}
	s.logger.Info("business calendar created", zap.String("calendar_id", c.ID.String()), zap.Int("holidays", len(c.Holidays)))
	return c, nil
}

// EnsureDefaultCalendar creates the tenant's first calendar (Monday to Friday, 08:00 to 17:00,
// Africa/Nairobi, Kenyan holidays) unless the tenant has one. It reports whether it created one.
func (s *Service) EnsureDefaultCalendar(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	count, err := s.repo.CountCalendars(ctx, tenantID)
	if err != nil || count > 0 {
		return false, err
	}
	name := DefaultCalendarName
	if _, err := s.CreateCalendar(ctx, tenantID, CalendarInput{Name: &name}); err != nil {
		return false, err
	}
	return true, nil
}

// GetCalendar returns one of the tenant's calendars.
func (s *Service) GetCalendar(ctx context.Context, tenantID, calendarID uuid.UUID) (*Calendar, error) {
	return s.repo.GetCalendar(ctx, tenantID, calendarID)
}

// ListCalendars pages the tenant's calendars, the default first, then by name.
func (s *Service) ListCalendars(ctx context.Context, filter CalendarFilter) ([]*Calendar, int, error) {
	return s.repo.ListCalendars(ctx, filter)
}

// UpdateCalendar changes the fields given. Making a calendar the default unsets the others.
func (s *Service) UpdateCalendar(ctx context.Context, tenantID, calendarID uuid.UUID, in CalendarInput) (*Calendar, error) {
	c, err := s.repo.GetCalendar(ctx, tenantID, calendarID)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		c.Name = *in.Name
	}
	if in.Timezone != nil {
		c.Timezone = *in.Timezone
		if c.Timezone == "" {
			c.Timezone = clock.DefaultTimezone
		}
	}
	if in.WeeklyHours != nil {
		c.WeeklyHours = in.WeeklyHours
	}
	if in.Holidays != nil {
		c.Holidays = *in.Holidays
	}
	if in.IsDefault != nil {
		c.IsDefault = *in.IsDefault
	}
	if err := validateCalendar(c); err != nil {
		return nil, err
	}
	if err := s.repo.SaveCalendar(ctx, c); err != nil {
		return nil, err
	}
	if c.IsDefault {
		if err := s.repo.ClearDefaultCalendar(ctx, tenantID, c.ID); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// DeleteCalendar deletes a calendar no policy uses.
func (s *Service) DeleteCalendar(ctx context.Context, tenantID, calendarID uuid.UUID) error {
	inUse, err := s.repo.CalendarInUse(ctx, tenantID, calendarID)
	if err != nil {
		return err
	}
	if inUse {
		return ErrCalendarInUse
	}
	return s.repo.DeleteCalendar(ctx, tenantID, calendarID)
}

// yearIn is the calendar year of t in the timezone; UTC when the zone is unknown (validation
// rejects it afterwards).
func yearIn(t time.Time, timezone string) int {
	if loc, err := time.LoadLocation(timezone); err == nil {
		return t.In(loc).Year()
	}
	return t.UTC().Year()
}
