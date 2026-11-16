package clock

import (
	"strings"
	"testing"
	"time"
)

func TestEasterSunday(t *testing.T) {
	for year, want := range map[int]string{2024: "2024-03-31", 2025: "2025-04-20", 2026: "2026-04-05", 2027: "2027-03-28"} {
		if got := EasterSunday(year).Format(time.DateOnly); got != want {
			t.Errorf("Easter %d = %s, want %s", year, got, want)
		}
	}
}

func holidayDates(hs []Holiday) string {
	parts := make([]string, len(hs))
	for i, h := range hs {
		parts[i] = h.Date
	}
	return strings.Join(parts, " ")
}

// The expected lists match https://www.timeanddate.com/holidays/kenya/2026 and /2027 (read
// 17 November 2026).
func TestKenyaHolidays(t *testing.T) {
	cases := map[int]string{
		2026: "2026-01-01 2026-03-20 2026-04-03 2026-04-06 2026-05-01 2026-06-01 2026-10-10 2026-10-20 2026-12-12 2026-12-25 2026-12-26",
		2027: "2027-01-01 2027-03-10 2027-03-26 2027-03-29 2027-05-01 2027-06-01 2027-10-10 2027-10-11 2027-10-20 2027-12-12 2027-12-13 2027-12-25 2027-12-26 2027-12-27",
	}
	for year, want := range cases {
		if got := holidayDates(KenyaHolidays(year)); got != want {
			t.Errorf("%d:\n got %s\nwant %s", year, got, want)
		}
	}
}

func TestKenyaHolidayFlags(t *testing.T) {
	for _, h := range KenyaHolidays(2027) {
		moon := strings.HasPrefix(h.Name, "Idd ul-Fitr")
		if h.MoonDependent != moon {
			t.Errorf("%s %s: moon_dependent = %v", h.Date, h.Name, h.MoonDependent)
		}
		if h.Date == "2027-12-27" && h.Name != "Boxing Day (observed)" {
			t.Errorf("27 December 2027 is %q, want Boxing Day (observed)", h.Name)
		}
		if h.Date == "2027-10-10" && h.Name != "Mazingira Day" {
			t.Errorf("10 October is %q, want Mazingira Day", h.Name)
		}
	}
	// No Eid date is guessed for a year without a published one.
	for _, h := range KenyaHolidays(2030) {
		if h.MoonDependent {
			t.Errorf("2030 should carry no moon-dependent date, got %s", h.Date)
		}
	}
}

// When Christmas falls on a Sunday, Boxing Day takes the Monday and Christmas moves to Tuesday.
func TestSundaySubstituteSkipsTakenDays(t *testing.T) {
	got := holidayDates(KenyaHolidays(2022))
	if !strings.Contains(got, "2022-12-25 2022-12-26 2022-12-27") {
		t.Errorf("2022 Christmas substitute missing: %s", got)
	}
}

func TestMergeHolidays(t *testing.T) {
	base := []Holiday{{Date: "2026-03-20", Name: "Idd ul-Fitr", MoonDependent: true}, {Date: "2026-01-01", Name: "New Year's Day"}}
	extra := []Holiday{{Date: "2026-03-21", Name: "Idd ul-Fitr"}, {Date: "2026-01-01", Name: "New Year"}}
	got := MergeHolidays(base, extra)
	if holidayDates(got) != "2026-01-01 2026-03-20 2026-03-21" || got[0].Name != "New Year" {
		t.Errorf("merge = %+v", got)
	}
}
