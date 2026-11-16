package clock

import (
	"sort"
	"time"
)

// Kenyan public holidays under the Public Holidays Act (Cap. 110). Dates were checked on
// 17 November 2026 against https://www.timeanddate.com/holidays/kenya/2026 and
// https://www.timeanddate.com/holidays/kenya/2027.
//
// 10 October is Mazingira Day. The Statute Law (Miscellaneous Amendments) Act 2024, assented on
// 24 April 2024, renamed Utamaduni Day to Mazingira Day
// (https://citizen.digital/article/utamaduni-day-renamed-to-mazingira-day-as-ruto-signs-new-law-n340968).
//
// A holiday that falls on a Sunday is also observed on the next day that is not itself a
// holiday, as the 2027 listing shows for 10 October, 12 December and 26 December.

// fixedKE are the holidays with the same date every year, as (month, day, name).
var fixedKE = []struct {
	month time.Month
	day   int
	name  string
}{
	{time.January, 1, "New Year's Day"},
	{time.May, 1, "Labour Day"},
	{time.June, 1, "Madaraka Day"},
	{time.October, 10, "Mazingira Day"},
	{time.October, 20, "Mashujaa Day"},
	{time.December, 12, "Jamhuri Day"},
	{time.December, 25, "Christmas Day"},
	{time.December, 26, "Boxing Day"},
}

// eidAlFitrKE are the Eid al-Fitr (Idd ul-Fitr) dates published for Kenya. The day depends on
// the sighting of the moon and is gazetted shortly before, so every entry is seeded as
// moon-dependent for the tenant to confirm. Only years with a published date are listed; no date
// is guessed for other years.
var eidAlFitrKE = map[int]string{
	2026: "2026-03-20",
	2027: "2027-03-10",
}

// KenyaHolidays returns Kenya's public holidays for a year in date order, with Good Friday and
// Easter Monday computed and Sunday holidays carried to the next free day.
func KenyaHolidays(year int) []Holiday {
	type entry struct {
		date time.Time
		h    Holiday
	}
	var list []entry
	add := func(d time.Time, name string, moon bool) {
		list = append(list, entry{d, Holiday{Date: d.Format(time.DateOnly), Name: name, MoonDependent: moon}})
	}
	for _, f := range fixedKE {
		add(time.Date(year, f.month, f.day, 0, 0, 0, 0, time.UTC), f.name, false)
	}
	easter := EasterSunday(year)
	add(easter.AddDate(0, 0, -2), "Good Friday", false)
	add(easter.AddDate(0, 0, 1), "Easter Monday", false)
	if d, ok := eidAlFitrKE[year]; ok {
		t, _ := time.Parse(time.DateOnly, d)
		add(t, "Idd ul-Fitr", true)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].date.Before(list[j].date) })

	taken := make(map[string]bool, len(list))
	for _, e := range list {
		taken[e.h.Date] = true
	}
	out := make([]Holiday, 0, len(list)+3)
	for _, e := range list {
		out = append(out, e.h)
		if e.date.Weekday() != time.Sunday {
			continue
		}
		next := e.date.AddDate(0, 0, 1)
		for taken[next.Format(time.DateOnly)] {
			next = next.AddDate(0, 0, 1)
		}
		taken[next.Format(time.DateOnly)] = true
		out = append(out, Holiday{Date: next.Format(time.DateOnly), Name: e.h.Name + " (observed)", MoonDependent: e.h.MoonDependent})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// EasterSunday is the Gregorian Easter Sunday of a year (the anonymous Gregorian algorithm of
// Meeus, Jones and Butcher).
func EasterSunday(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// MergeHolidays returns base with extra added; an extra entry replaces a base entry on the same
// date. The result is in date order.
func MergeHolidays(base, extra []Holiday) []Holiday {
	byDate := make(map[string]Holiday, len(base)+len(extra))
	for _, h := range base {
		byDate[h.Date] = h
	}
	for _, h := range extra {
		byDate[h.Date] = h
	}
	out := make([]Holiday, 0, len(byDate))
	for _, h := range byDate {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}
