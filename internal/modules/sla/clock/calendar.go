// Package clock is the SLA business-time arithmetic: a calendar of weekly working hours and
// holidays in one IANA timezone, adding business minutes to an instant and counting the business
// minutes between two instants. It has no dependencies inside the service, so the SLA module, the
// ticket module and the Ent schema can all use its types.
package clock

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	// The timezone database is embedded so a container image without tzdata still loads zones.
	_ "time/tzdata"
)

// DefaultTimezone is the timezone a calendar gets when none is given.
const DefaultTimezone = "Africa/Nairobi"

// Weekday keys of a Week, indexed by time.Weekday (Sunday is 0).
var weekdayKeys = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Interval is one working period of a day in local wall time, "08:00" to "17:00". End may be
// "24:00" for a period that runs to midnight.
type Interval struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// Week maps a weekday key (mon, tue, wed, thu, fri, sat, sun) to its working periods. A missing
// or empty day is not worked.
type Week map[string][]Interval

// Holiday is a date on which no business time passes. MoonDependent marks a holiday whose date is
// announced close to the day (Eid al-Fitr), so the tenant is expected to confirm or edit it.
type Holiday struct {
	Date          string `json:"date"`
	Name          string `json:"name"`
	MoonDependent bool   `json:"moon_dependent,omitempty"`
}

// DefaultWeek is Monday to Friday, 08:00 to 17:00.
func DefaultWeek() Week {
	day := []Interval{{Start: "08:00", End: "17:00"}}
	return Week{"mon": day, "tue": day, "wed": day, "thu": day, "fri": day}
}

// span is a working period in minutes after local midnight, [start, end).
type span struct{ start, end int }

// Calendar is a parsed business calendar. A nil *Calendar is the 24x7 clock: every minute counts.
type Calendar struct {
	loc      *time.Location
	days     [7][]span
	holidays map[string]bool
}

// ErrInvalidCalendar wraps every validation failure of New.
var ErrInvalidCalendar = errors.New("invalid business calendar")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCalendar, fmt.Sprintf(format, args...))
}

// New validates and parses a calendar. The week must have at least one working period.
func New(timezone string, week Week, holidays []Holiday) (*Calendar, error) {
	if timezone == "" {
		timezone = DefaultTimezone
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, invalid("unknown timezone %q", timezone)
	}
	c := &Calendar{loc: loc, holidays: make(map[string]bool, len(holidays))}
	worked := false
	for key, intervals := range week {
		idx := weekdayIndex(key)
		if idx < 0 {
			return nil, invalid("unknown weekday %q", key)
		}
		spans, err := parseDay(key, intervals)
		if err != nil {
			return nil, err
		}
		c.days[idx] = spans
		worked = worked || len(spans) > 0
	}
	if !worked {
		return nil, invalid("the week has no working hours")
	}
	for _, h := range holidays {
		if _, err := time.Parse(time.DateOnly, h.Date); err != nil {
			return nil, invalid("holiday %q has date %q, want YYYY-MM-DD", h.Name, h.Date)
		}
		c.holidays[h.Date] = true
	}
	return c, nil
}

func weekdayIndex(key string) int {
	for i, k := range weekdayKeys {
		if k == key {
			return i
		}
	}
	return -1
}

func parseDay(key string, intervals []Interval) ([]span, error) {
	spans := make([]span, 0, len(intervals))
	for _, iv := range intervals {
		start, okStart := parseClock(iv.Start)
		end, okEnd := parseClock(iv.End)
		if !okStart || !okEnd || start >= end {
			return nil, invalid("%s period %s-%s is not a valid HH:MM range", key, iv.Start, iv.End)
		}
		spans = append(spans, span{start, end})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return nil, invalid("%s periods overlap", key)
		}
	}
	return spans, nil
}

// parseClock reads "HH:MM" as minutes after midnight; "24:00" is the end of the day.
func parseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, false
	}
	hh, errH := strconv.Atoi(h)
	mm, errM := strconv.Atoi(m)
	if errH != nil || errM != nil || hh < 0 || mm < 0 || mm > 59 || hh > 24 || (hh == 24 && mm != 0) {
		return 0, false
	}
	return hh*60 + mm, true
}

// Location is the calendar's timezone; UTC for the 24x7 clock.
func (c *Calendar) Location() *time.Location {
	if c == nil {
		return time.UTC
	}
	return c.loc
}

// maxDays bounds the day walk, so a calendar whose every working day is a holiday cannot loop.
const maxDays = 3660

// periods returns the working periods of the local day that starts at midnight, as instants.
func (c *Calendar) periods(midnight time.Time) [][2]time.Time {
	if c.holidays[midnight.Format(time.DateOnly)] {
		return nil
	}
	spans := c.days[midnight.Weekday()]
	out := make([][2]time.Time, 0, len(spans))
	y, mo, d := midnight.Date()
	for _, s := range spans {
		// time.Date normalises 24:00 to the next midnight and resolves DST gaps and overlaps.
		out = append(out, [2]time.Time{
			time.Date(y, mo, d, s.start/60, s.start%60, 0, 0, c.loc),
			time.Date(y, mo, d, s.end/60, s.end%60, 0, 0, c.loc),
		})
	}
	return out
}

func localMidnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func nextMidnight(midnight time.Time) time.Time {
	y, m, d := midnight.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, midnight.Location())
}

// Add returns the instant at which minutes of business time have passed after start. A start
// outside working hours begins counting at the next working period, and a deadline that lands
// exactly on the end of a period stays there. The result is in start's location.
func (c *Calendar) Add(start time.Time, minutes int) time.Time {
	if minutes <= 0 {
		return start
	}
	if c == nil {
		return start.Add(time.Duration(minutes) * time.Minute)
	}
	remaining := time.Duration(minutes) * time.Minute
	cursor := start
	day := localMidnight(start, c.loc)
	for i := 0; i < maxDays; i++ {
		for _, p := range c.periods(day) {
			if !cursor.Before(p[1]) {
				continue
			}
			from := p[0]
			if cursor.After(from) {
				from = cursor
			}
			avail := p[1].Sub(from)
			if remaining <= avail {
				return from.Add(remaining).In(start.Location())
			}
			remaining -= avail
		}
		day = nextMidnight(day)
	}
	return cursor.Add(remaining).In(start.Location())
}

// Minutes counts the whole business minutes in [from, to); zero when to is not after from.
func (c *Calendar) Minutes(from, to time.Time) int {
	if !to.After(from) {
		return 0
	}
	if c == nil {
		return int(to.Sub(from) / time.Minute)
	}
	var total time.Duration
	day := localMidnight(from, c.loc)
	for i := 0; i < maxDays*10 && day.Before(to); i++ {
		for _, p := range c.periods(day) {
			lo, hi := p[0], p[1]
			if from.After(lo) {
				lo = from
			}
			if to.Before(hi) {
				hi = to
			}
			if hi.After(lo) {
				total += hi.Sub(lo)
			}
		}
		day = nextMidnight(day)
	}
	return int(total / time.Minute)
}
