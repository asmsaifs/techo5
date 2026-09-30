package ical

import (
	"strconv"
	"strings"
	"time"
)

// rule is an RRULE, the parts of it real calendars use.
type rule struct {
	freq       string // DAILY, WEEKLY, MONTHLY, YEARLY
	interval   int
	count      int
	until      time.Time
	byDay      []byDay
	byMonthDay []int
	byMonth    []int
}

// byDay is a weekday, and for a month or a year which one of them: 2 is the second, -1 the last, 0 every.
type byDay struct {
	n   int
	day time.Weekday
}

var weekdays = map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday,
	"WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}

func parseRule(s string, loc *time.Location) (rule, bool) {
	r := rule{interval: 1}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(k) {
		case "FREQ":
			r.freq = strings.ToUpper(v)
		case "INTERVAL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				r.interval = n
			}
		case "COUNT":
			r.count, _ = strconv.Atoi(v)
		case "UNTIL":
			p := prop{value: v, params: map[string]string{}}
			if !strings.HasSuffix(v, "Z") && len(v) > 8 {
				p.params["TZID"] = loc.String()
			}
			t, allDay := when(p)
			if allDay {
				t = t.AddDate(0, 0, 1).Add(-time.Second) // the whole of that day
			}
			r.until = t
		case "BYDAY":
			for _, d := range strings.Split(v, ",") {
				d = strings.ToUpper(strings.TrimSpace(d))
				if len(d) < 2 {
					continue
				}
				wd, ok := weekdays[d[len(d)-2:]]
				if !ok {
					continue
				}
				n, _ := strconv.Atoi(d[:len(d)-2])
				r.byDay = append(r.byDay, byDay{n: n, day: wd})
			}
		case "BYMONTHDAY":
			for _, d := range strings.Split(v, ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(d)); err == nil && n != 0 {
					r.byMonthDay = append(r.byMonthDay, n)
				}
			}
		case "BYMONTH":
			for _, m := range strings.Split(v, ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(m)); err == nil && n >= 1 && n <= 12 {
					r.byMonth = append(r.byMonth, n)
				}
			}
		}
	}
	switch r.freq {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
		return r, true
	}
	return r, false
}

// occurrences is every start of the event before to: the one start, or the rule's, less its EXDATEs.
func (v *vevent) occurrences(to time.Time) []time.Time {
	if v.rrule == "" {
		return []time.Time{v.start}
	}
	r, ok := parseRule(v.rrule, v.start.Location())
	if !ok {
		return []time.Time{v.start}
	}
	skip := map[int64]bool{}
	for _, x := range v.exdates {
		skip[x.Unix()] = true
		if v.allDay { // a date excludes the day whatever time it is read at
			skip[time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, v.start.Location()).Unix()] = true
		}
	}
	var out []time.Time
	n := 0
	emit := func(t time.Time) bool {
		if t.Before(v.start) {
			return true
		}
		if (!r.until.IsZero() && t.After(r.until)) || !t.Before(to) {
			return false
		}
		n++
		if r.count > 0 && n > r.count {
			return false
		}
		if !skip[t.Unix()] {
			out = append(out, t)
		}
		return n < maxOccurrences
	}
	s := v.start
	at := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, s.Hour(), s.Minute(), s.Second(), 0, s.Location())
	}

	switch r.freq {
	case "DAILY":
		for i := 0; ; i++ {
			if !emit(at(s.Year(), s.Month(), s.Day()+i*r.interval)) {
				return out
			}
		}

	case "WEEKLY":
		days := r.byDay
		if len(days) == 0 {
			days = []byDay{{day: s.Weekday()}}
		}
		// The week the event starts in, from its Monday (the default week start).
		monday := s.Day() - (int(s.Weekday())+6)%7
		for w := 0; ; w++ {
			var week []time.Time
			for _, d := range days {
				week = append(week, at(s.Year(), s.Month(), monday+w*7*r.interval+(int(d.day)+6)%7))
			}
			sortTimes(week)
			for _, t := range week {
				if !emit(t) {
					return out
				}
			}
		}

	case "MONTHLY":
		for i := 0; ; i++ {
			y, m := s.Year(), s.Month()+time.Month(i*r.interval)
			for _, t := range monthDays(r, y, m, s, at) {
				if !emit(t) {
					return out
				}
			}
			if i > maxOccurrences {
				return out
			}
		}

	case "YEARLY":
		months := r.byMonth
		if len(months) == 0 {
			months = []int{int(s.Month())}
		}
		for i := 0; ; i++ {
			y := s.Year() + i*r.interval
			var year []time.Time
			for _, m := range months {
				year = append(year, monthDays(r, y, time.Month(m), s, at)...)
			}
			sortTimes(year)
			for _, t := range year {
				if !emit(t) {
					return out
				}
			}
			if i > maxOccurrences {
				return out
			}
		}
	}
	return out
}

// monthDays is the starts in one month: by weekday (the nth, or every), by day of the month (from
// the end when negative), or the start's own day. A day the month does not have is not moved into
// the next: the 31st does not happen in April.
func monthDays(r rule, y int, m time.Month, s time.Time, at func(int, time.Month, int) time.Time) []time.Time {
	first := time.Date(y, m, 1, 0, 0, 0, 0, s.Location())
	y, m = first.Year(), first.Month() // a month past December is next year's
	last := first.AddDate(0, 1, -1).Day()
	var out []time.Time
	switch {
	case len(r.byDay) > 0:
		for _, d := range r.byDay {
			var matches []int
			for day := 1; day <= last; day++ {
				if time.Date(y, m, day, 0, 0, 0, 0, s.Location()).Weekday() == d.day {
					matches = append(matches, day)
				}
			}
			switch {
			case d.n == 0:
				for _, day := range matches {
					out = append(out, at(y, m, day))
				}
			case d.n > 0 && d.n <= len(matches):
				out = append(out, at(y, m, matches[d.n-1]))
			case d.n < 0 && -d.n <= len(matches):
				out = append(out, at(y, m, matches[len(matches)+d.n]))
			}
		}
	case len(r.byMonthDay) > 0:
		for _, d := range r.byMonthDay {
			if d < 0 {
				d = last + 1 + d
			}
			if d >= 1 && d <= last {
				out = append(out, at(y, m, d))
			}
		}
	default:
		if s.Day() <= last {
			out = append(out, at(y, m, s.Day()))
		}
	}
	sortTimes(out)
	return out
}

func sortTimes(ts []time.Time) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Before(ts[j-1]); j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}
