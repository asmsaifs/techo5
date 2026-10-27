package config

import "time"

// The night hours can differ at the weekend (#305): Screen.NightWeekend, when set, is the window of
// the nights that end on a Saturday or a Sunday morning, the ones somebody stays up later and sleeps
// in, and Screen.Night that of every other night. A night is the morning's it ends on: one that
// crosses midnight starts the evening before, and one that starts after midnight ("0-9") is all that
// morning's. So Friday's late evening is Saturday's night, and Sunday evening a weeknight's.

// weekendMorning is whether day's morning ends a weekend night.
func weekendMorning(day time.Time) bool {
	return day.Weekday() == time.Saturday || day.Weekday() == time.Sunday
}

// NightBefore is the window of the night that ends on morning's day: the weekend's on a Saturday or a
// Sunday when it is set, nights' otherwise.
func NightBefore(nights, weekend string, morning time.Time) string {
	if weekend != "" && weekendMorning(morning) {
		return weekend
	}
	return nights
}

// InNights is whether t falls in a night: the one that ends this morning, or, crossing midnight, the
// one that starts this evening and ends tomorrow morning.
func InNights(nights, weekend string, t time.Time) bool {
	m := t.Hour()*60 + t.Minute()
	if from, to, ok := ParseWindow(NightBefore(nights, weekend, t)); ok {
		if from < to && m >= from && m < to {
			return true
		}
		if from > to && m < to {
			return true
		}
	}
	tomorrow := time.Date(t.Year(), t.Month(), t.Day()+1, 12, 0, 0, 0, t.Location())
	if from, to, ok := ParseWindow(NightBefore(nights, weekend, tomorrow)); ok && from > to && m >= from {
		return true
	}
	return false
}

// NightChanges is when the nights ending from the day before at's to two days after start and end, by
// the clock on the wall, in no particular order.
func NightChanges(nights, weekend string, at time.Time) []time.Time {
	var out []time.Time
	for d := -1; d <= 2; d++ {
		morning := time.Date(at.Year(), at.Month(), at.Day()+d, 12, 0, 0, 0, at.Location())
		from, to, ok := ParseWindow(NightBefore(nights, weekend, morning))
		if !ok {
			continue
		}
		start := morning.Day()
		if from > to {
			start-- // it starts the evening before
		}
		out = append(out,
			time.Date(morning.Year(), morning.Month(), start, from/60, from%60, 0, 0, at.Location()),
			time.Date(morning.Year(), morning.Month(), morning.Day(), to/60, to%60, 0, 0, at.Location()))
	}
	return out
}

// NightEndOn is the minute the night ending on morning's day ends, and whether there is one.
func NightEndOn(nights, weekend string, morning time.Time) (int, bool) {
	_, to, ok := ParseWindow(NightBefore(nights, weekend, morning))
	return to, ok
}
