package clock

import (
	"errors"
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

func mustCal(t *testing.T, tz string, week Week, holidays []Holiday) *Calendar {
	t.Helper()
	c, err := New(tz, week, holidays)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func everyDay(intervals ...Interval) Week {
	w := Week{}
	for _, k := range weekdayKeys {
		w[k] = intervals
	}
	return w
}

func TestAddNairobiWeekdays(t *testing.T) {
	eat := mustLoc(t, "Africa/Nairobi")
	cal := mustCal(t, "", DefaultWeek(), KenyaHolidays(2026))
	at := func(m time.Month, d, h, min int) time.Time { return time.Date(2026, m, d, h, min, 0, 0, eat) }
	cases := []struct {
		name    string
		start   time.Time
		minutes int
		want    time.Time
	}{
		// The sprint 10 acceptance case: Friday 17:00, 2 business hours, due Monday 10:00.
		{"friday close to monday", at(11, 13, 17, 0), 120, at(11, 16, 10, 0)},
		{"friday afternoon over the weekend", at(11, 13, 16, 0), 120, at(11, 16, 9, 0)},
		{"saturday start", at(11, 14, 11, 0), 30, at(11, 16, 8, 30)},
		{"before opening", at(11, 16, 6, 0), 30, at(11, 16, 8, 30)},
		{"lunch hours are working hours", at(11, 16, 12, 0), 60, at(11, 16, 13, 0)},
		{"deadline on the closing minute stays there", at(11, 16, 16, 0), 60, at(11, 16, 17, 0)},
		{"overnight between weekdays", at(11, 16, 16, 30), 60, at(11, 17, 8, 30)},
		{"full working day", at(11, 16, 8, 0), 9 * 60, at(11, 16, 17, 0)},
		// Christmas Day (Friday) and Boxing Day (Saturday) and the Sunday are skipped.
		{"christmas holiday", at(12, 24, 16, 0), 120, at(12, 28, 9, 0)},
		// Jamhuri Day 2026 is a Saturday: no weekday is lost.
		{"jamhuri on a saturday", at(12, 11, 16, 0), 120, at(12, 14, 9, 0)},
		{"mashujaa day tuesday", at(10, 19, 16, 0), 120, at(10, 21, 9, 0)},
		{"zero minutes", at(11, 14, 11, 0), 0, at(11, 14, 11, 0)},
	}
	for _, c := range cases {
		if got := cal.Add(c.start, c.minutes); !got.Equal(c.want) {
			t.Errorf("%s: Add(%s, %d) = %s, want %s", c.name, c.start, c.minutes, got.In(eat), c.want)
		}
	}
}

func TestAddKeepsCallerLocation(t *testing.T) {
	cal := mustCal(t, "Africa/Nairobi", DefaultWeek(), nil)
	// 05:00 UTC on Monday 16 November 2026 is 08:00 in Nairobi.
	start := time.Date(2026, 11, 16, 5, 0, 0, 0, time.UTC)
	got := cal.Add(start, 60)
	want := time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("Add = %s (%s), want %s in UTC", got, got.Location(), want)
	}
	// 15:00 UTC Friday is 18:00 in Nairobi, after closing: the clock starts Monday 08:00 EAT.
	friday := time.Date(2026, 11, 13, 15, 0, 0, 0, time.UTC)
	if got := cal.Add(friday, 30); !got.Equal(time.Date(2026, 11, 16, 5, 30, 0, 0, time.UTC)) {
		t.Fatalf("Add after closing = %s", got)
	}
}

func TestAddOvernightShift(t *testing.T) {
	eat := mustLoc(t, "Africa/Nairobi")
	cal := mustCal(t, "Africa/Nairobi", everyDay(Interval{"00:00", "06:00"}, Interval{"22:00", "24:00"}), nil)
	start := time.Date(2026, 11, 16, 23, 0, 0, 0, eat)
	if got, want := cal.Add(start, 120), time.Date(2026, 11, 17, 1, 0, 0, 0, eat); !got.Equal(want) {
		t.Errorf("across midnight = %s, want %s", got, want)
	}
	if got, want := cal.Add(time.Date(2026, 11, 17, 5, 30, 0, 0, eat), 60), time.Date(2026, 11, 17, 22, 30, 0, 0, eat); !got.Equal(want) {
		t.Errorf("morning to night = %s, want %s", got, want)
	}
	if got := cal.Minutes(start, time.Date(2026, 11, 17, 1, 0, 0, 0, eat)); got != 120 {
		t.Errorf("Minutes across midnight = %d, want 120", got)
	}
}

func TestDSTZone(t *testing.T) {
	london := mustLoc(t, "Europe/London")
	// Clocks go forward at 01:00 on Sunday 29 March 2026, so that day has 23 hours.
	allDay := mustCal(t, "Europe/London", everyDay(Interval{"00:00", "24:00"}), nil)
	start := time.Date(2026, 3, 28, 12, 0, 0, 0, london)
	if got, want := allDay.Add(start, 24*60), time.Date(2026, 3, 29, 13, 0, 0, 0, london); !got.Equal(want) {
		t.Errorf("24 hours across the spring change = %s, want %s", got.In(london), want)
	}
	if got := allDay.Minutes(time.Date(2026, 3, 29, 0, 0, 0, 0, london), time.Date(2026, 3, 30, 0, 0, 0, 0, london)); got != 23*60 {
		t.Errorf("the short day has %d minutes, want %d", got, 23*60)
	}
	// Office hours keep their wall-clock times on either side of the change.
	office := mustCal(t, "Europe/London", Week{"mon": {{"09:00", "17:00"}}, "fri": {{"09:00", "17:00"}}}, nil)
	friday := time.Date(2026, 3, 27, 16, 0, 0, 0, london)
	if got, want := office.Add(friday, 120), time.Date(2026, 3, 30, 10, 0, 0, 0, london); !got.Equal(want) {
		t.Errorf("office hours over the change = %s, want %s", got.In(london), want)
	}
	// Clocks go back on Sunday 25 October 2026, a 25-hour day.
	if got := allDay.Minutes(time.Date(2026, 10, 25, 0, 0, 0, 0, london), time.Date(2026, 10, 26, 0, 0, 0, 0, london)); got != 25*60 {
		t.Errorf("the long day has %d minutes, want %d", got, 25*60)
	}
}

func TestMinutes(t *testing.T) {
	eat := mustLoc(t, "Africa/Nairobi")
	cal := mustCal(t, "Africa/Nairobi", DefaultWeek(), KenyaHolidays(2026))
	at := func(m time.Month, d, h, min int) time.Time { return time.Date(2026, m, d, h, min, 0, 0, eat) }
	cases := []struct {
		from, to time.Time
		want     int
	}{
		{at(11, 13, 16, 0), at(11, 16, 9, 0), 120},
		{at(11, 14, 9, 0), at(11, 15, 18, 0), 0},
		{at(11, 16, 9, 0), at(11, 16, 9, 0), 0},
		{at(11, 16, 10, 0), at(11, 16, 9, 0), 0},
		{at(12, 24, 8, 0), at(12, 29, 8, 0), 18 * 60},
		{at(11, 16, 8, 0), at(11, 20, 17, 0), 5 * 9 * 60},
	}
	for _, c := range cases {
		if got := cal.Minutes(c.from, c.to); got != c.want {
			t.Errorf("Minutes(%s, %s) = %d, want %d", c.from, c.to, got, c.want)
		}
	}
}

// Minutes undoes Add from any start, inside or outside working hours.
func TestAddMinutesRoundTrip(t *testing.T) {
	eat := mustLoc(t, "Africa/Nairobi")
	cal := mustCal(t, "Africa/Nairobi", DefaultWeek(), KenyaHolidays(2026))
	start := time.Date(2026, 12, 20, 3, 17, 0, 0, eat)
	for i := 0; i < 200; i++ {
		s := start.Add(time.Duration(i*97) * time.Minute)
		n := (i * 53) % 4000
		if got := cal.Minutes(s, cal.Add(s, n)); got != n {
			t.Fatalf("Minutes(s, Add(s, %d)) = %d from %s", n, got, s)
		}
	}
}

func TestNilCalendarIs24x7(t *testing.T) {
	var cal *Calendar
	start := time.Date(2026, 11, 14, 23, 0, 0, 0, time.UTC)
	if got := cal.Add(start, 90); !got.Equal(start.Add(90 * time.Minute)) {
		t.Errorf("Add = %s", got)
	}
	if got := cal.Minutes(start, start.Add(90*time.Minute+30*time.Second)); got != 90 {
		t.Errorf("Minutes = %d, want 90", got)
	}
	if cal.Location() != time.UTC {
		t.Error("the 24x7 clock reports UTC")
	}
}

func TestNewRejects(t *testing.T) {
	cases := []struct {
		name     string
		tz       string
		week     Week
		holidays []Holiday
	}{
		{"unknown zone", "Mars/Olympus", DefaultWeek(), nil},
		{"no hours", "", Week{}, nil},
		{"unknown day", "", Week{"funday": {{"08:00", "17:00"}}}, nil},
		{"end before start", "", Week{"mon": {{"17:00", "08:00"}}}, nil},
		{"bad clock", "", Week{"mon": {{"8:00", "17:00"}}}, nil},
		{"past midnight", "", Week{"mon": {{"08:00", "24:30"}}}, nil},
		{"overlap", "", Week{"mon": {{"08:00", "12:00"}, {"11:00", "17:00"}}}, nil},
		{"bad holiday", "", DefaultWeek(), []Holiday{{Date: "25/12/2026", Name: "Christmas"}}},
	}
	for _, c := range cases {
		if _, err := New(c.tz, c.week, c.holidays); !errors.Is(err, ErrInvalidCalendar) {
			t.Errorf("%s: err = %v, want ErrInvalidCalendar", c.name, err)
		}
	}
	if c, err := New("", DefaultWeek(), nil); err != nil || c.Location().String() != DefaultTimezone {
		t.Errorf("an empty timezone is %s: %v", DefaultTimezone, err)
	}
}
