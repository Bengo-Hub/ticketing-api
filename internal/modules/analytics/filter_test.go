package analytics

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
)

var tenant = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func parse(t *testing.T, q string, tz string, now time.Time) Filter {
	t.Helper()
	v, _ := url.ParseQuery(q)
	f, err := ParseFilter(v, tenant, tz, now)
	if err != nil {
		t.Fatalf("ParseFilter(%q): %v", q, err)
	}
	return f
}

func TestDefaultWindowIsThirtyLocalDaysFromHourCells(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 20, 0, 0, time.UTC) // 13:20 in Nairobi
	f := parse(t, "", "Africa/Nairobi", now)
	nbo, _ := time.LoadLocation("Africa/Nairobi")
	wantFrom := time.Date(2026, 10, 19, 0, 0, 0, 0, nbo)
	if !f.From.Equal(wantFrom) {
		t.Fatalf("from = %v, want %v", f.From, wantFrom)
	}
	if want := time.Date(2026, 11, 17, 11, 0, 0, 0, time.UTC); !f.To.Equal(want) {
		t.Fatalf("to = %v, want %v (now widened to the hour)", f.To, want)
	}
	if f.Source != SourceHour || f.Interval != IntervalDay || f.Loc.String() != "Africa/Nairobi" {
		t.Fatalf("source %s interval %s tz %s", f.Source, f.Interval, f.Loc)
	}
}

func TestLongWindowUsesUTCDays(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	f := parse(t, "from=2026-01-01&to=2026-06-30", "UTC", now)
	if f.Source != SourceDay || f.Interval != IntervalWeek {
		t.Fatalf("source %s interval %s", f.Source, f.Interval)
	}
	if !f.From.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || !f.To.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("window %v to %v", f.From, f.To)
	}
	if _, err := ParseFilter(url.Values{"from": {"2026-01-01"}, "to": {"2026-06-30"}, "interval": {"hour"}}, tenant, "", now); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("hour interval on a long window = %v", err)
	}
}

func TestFilterRejectsBadInput(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	for _, q := range []string{
		"from=2026-11-10&to=2026-11-01",
		"from=2024-01-01&to=2026-01-01",
		"tz=Mars/Base",
		"team_id=nope",
		"channel=fax",
		"priority=meh",
		"interval=year",
		"from=yesterday",
	} {
		v, _ := url.ParseQuery(q)
		if _, err := ParseFilter(v, tenant, "", now); !errors.Is(err, ErrInvalidFilter) {
			t.Errorf("%q: err = %v, want ErrInvalidFilter", q, err)
		}
	}
}

func TestFilterDimensionsAndPrevious(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	team := uuid.New()
	f := parse(t, "from=2026-11-10&to=2026-11-16&team_id="+team.String()+"&channel=whatsapp&priority=high", "UTC", now)
	if f.TeamID == nil || *f.TeamID != team || f.Channel != "whatsapp" || f.Priority != "high" {
		t.Fatalf("dims = %+v", f)
	}
	p := f.Previous()
	if !p.To.Equal(f.From) || p.To.Sub(p.From) != f.To.Sub(f.From) {
		t.Fatalf("previous %v to %v for %v to %v", p.From, p.To, f.From, f.To)
	}
}

func TestBucketsCoverTheWindowInLocalTime(t *testing.T) {
	nbo, _ := time.LoadLocation("Africa/Nairobi")
	f := Filter{From: time.Date(2026, 11, 1, 0, 0, 0, 0, nbo), To: time.Date(2026, 11, 4, 0, 0, 0, 0, nbo), Loc: nbo, Interval: IntervalDay}
	b := f.Buckets()
	if len(b) != 3 || b[0].Format(bucketLabel) != "2026-11-01T00:00:00" || b[2].Format(bucketLabel) != "2026-11-03T00:00:00" {
		t.Fatalf("buckets = %v", b)
	}
	// 2026-11-04 is a Wednesday; its week starts on Monday 2 November.
	if w := truncLocal(time.Date(2026, 11, 4, 15, 0, 0, 0, nbo), IntervalWeek, nbo); w.Format(bucketLabel) != "2026-11-02T00:00:00" {
		t.Fatalf("week start = %v", w)
	}
}
