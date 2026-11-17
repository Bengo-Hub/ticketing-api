package analytics

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata" // tenant time zones resolve in a container without zoneinfo

	"github.com/google/uuid"
)

// Rollup granularities, the values of ticket_rollups.granularity.
const (
	SourceHour = "hour"
	SourceDay  = "day"
)

// Trend intervals.
const (
	IntervalHour  = "hour"
	IntervalDay   = "day"
	IntervalWeek  = "week"
	IntervalMonth = "month"
)

const (
	// HourlyWindowMax is the longest window read from hour cells. Longer windows read UTC-day
	// cells and are widened to whole UTC days.
	HourlyWindowMax = 31 * 24 * time.Hour
	// MaxWindow is the longest window a request may ask for.
	MaxWindow = 367 * 24 * time.Hour
	// DefaultDays is the default window: today and the 29 days before it.
	DefaultDays = 30
)

// ErrInvalidFilter is a bad query parameter; the message says which.
var ErrInvalidFilter = errors.New("analytics: invalid filter")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidFilter, fmt.Sprintf(format, args...))
}

var (
	channels   = map[string]bool{"web": true, "email": true, "api": true, "chat": true, "whatsapp": true, "phone": true, "internal": true}
	priorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true, "critical": true}
)

// Filter is one analytics request: the tenant, the window and the dimension filters. From and To
// are the effective window after alignment to the rollup cells, From inclusive and To exclusive.
type Filter struct {
	TenantID   uuid.UUID
	From       time.Time
	To         time.Time
	Loc        *time.Location
	Interval   string
	Source     string
	TeamID     *uuid.UUID
	AgentID    *uuid.UUID
	CategoryID *uuid.UUID
	Channel    string
	Priority   string
}

// Previous is the same filter over the window of equal length that ends where this one starts.
func (f Filter) Previous() Filter {
	p := f
	p.To = f.From
	p.From = f.From.Add(-f.To.Sub(f.From))
	return p
}

// ParseFilter reads the query: from, to, tz, interval, team_id, agent_id, category_id, channel and
// priority. defaultTZ is the tenant's time zone; an empty one means UTC.
//
//   - from and to are RFC 3339 times or YYYY-MM-DD dates in tz; a date as to includes that whole
//     day. The default is today and the 29 days before it, up to now.
//   - Windows up to 31 days read hour cells and are widened to whole hours; longer ones read
//     UTC-day cells and are widened to whole UTC days. At most 367 days.
//   - interval is hour, day, week (Monday start) or month; by default hour up to 2 days, day up
//     to 92 days, week beyond. hour needs a window of 31 days or less.
func ParseFilter(q url.Values, tenantID uuid.UUID, defaultTZ string, now time.Time) (Filter, error) {
	f := Filter{TenantID: tenantID}
	tz := strings.TrimSpace(q.Get("tz"))
	if tz == "" {
		tz = strings.TrimSpace(defaultTZ)
	}
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		if q.Get("tz") != "" {
			return f, invalid("unknown time zone %q", tz)
		}
		loc = time.UTC
	}
	f.Loc = loc

	to := now
	if s := strings.TrimSpace(q.Get("to")); s != "" {
		if to, err = parseBound(s, loc, true); err != nil {
			return f, invalid("to: %v", err)
		}
	}
	local := to.In(loc)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(DefaultDays - 1))
	if s := strings.TrimSpace(q.Get("from")); s != "" {
		if from, err = parseBound(s, loc, false); err != nil {
			return f, invalid("from: %v", err)
		}
	}
	if !from.Before(to) {
		return f, invalid("from must be before to")
	}
	if to.Sub(from) > MaxWindow {
		return f, invalid("the window is longer than 367 days")
	}
	if to.Sub(from) <= HourlyWindowMax {
		f.Source = SourceHour
		f.From, f.To = from.UTC().Truncate(time.Hour), ceil(to.UTC(), time.Hour)
	} else {
		f.Source = SourceDay
		f.From, f.To = utcDay(from), utcDay(to.Add(24*time.Hour-time.Nanosecond))
	}

	f.Interval = strings.TrimSpace(q.Get("interval"))
	switch f.Interval {
	case "":
		switch span := f.To.Sub(f.From); {
		case span <= 48*time.Hour:
			f.Interval = IntervalHour
		case span <= 92*24*time.Hour:
			f.Interval = IntervalDay
		default:
			f.Interval = IntervalWeek
		}
	case IntervalHour:
		if f.Source != SourceHour {
			return f, invalid("interval hour needs a window of 31 days or less")
		}
	case IntervalDay, IntervalWeek, IntervalMonth:
	default:
		return f, invalid("interval must be hour, day, week or month")
	}

	for _, d := range []struct {
		name string
		dst  **uuid.UUID
	}{{"team_id", &f.TeamID}, {"agent_id", &f.AgentID}, {"category_id", &f.CategoryID}} {
		if s := strings.TrimSpace(q.Get(d.name)); s != "" {
			id, err := uuid.Parse(s)
			if err != nil {
				return f, invalid("%s is not a UUID", d.name)
			}
			*d.dst = &id
		}
	}
	if f.Channel = strings.TrimSpace(q.Get("channel")); f.Channel != "" && !channels[f.Channel] {
		return f, invalid("unknown channel %q", f.Channel)
	}
	if f.Priority = strings.TrimSpace(q.Get("priority")); f.Priority != "" && !priorities[f.Priority] {
		return f, invalid("unknown priority %q", f.Priority)
	}
	return f, nil
}

// parseBound reads an RFC 3339 time or a YYYY-MM-DD date in loc. A date as the upper bound means
// the end of that day (the next midnight).
func parseBound(s string, loc *time.Location, upper bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	d, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}, errors.New("use RFC 3339 or YYYY-MM-DD")
	}
	if upper {
		d = d.AddDate(0, 0, 1)
	}
	return d, nil
}

func ceil(t time.Time, d time.Duration) time.Time {
	if tr := t.Truncate(d); !tr.Equal(t) {
		return tr.Add(d)
	}
	return t
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// truncLocal truncates t to the start of its interval in loc, as Postgres date_trunc does on a
// local timestamp (weeks start on Monday).
func truncLocal(t time.Time, interval string, loc *time.Location) time.Time {
	t = t.In(loc)
	switch interval {
	case IntervalHour:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
	case IntervalWeek:
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
	case IntervalMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
	default:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
}

func nextBucket(t time.Time, interval string) time.Time {
	switch interval {
	case IntervalHour:
		return t.Add(time.Hour)
	case IntervalWeek:
		return t.AddDate(0, 0, 7)
	case IntervalMonth:
		return t.AddDate(0, 1, 0)
	default:
		return t.AddDate(0, 0, 1)
	}
}

// bucketLabel is a bucket's start as local wall-clock time, the key the trend query returns.
const bucketLabel = "2006-01-02T15:04:05"

// Buckets lists every trend bucket of the window in order, empty ones included, as local
// wall-clock labels and as times.
func (f Filter) Buckets() []time.Time {
	var out []time.Time
	for b := truncLocal(f.From, f.Interval, f.Loc); b.Before(f.To); b = nextBucket(b, f.Interval) {
		out = append(out, b)
		if len(out) > 10000 {
			break
		}
	}
	return out
}
