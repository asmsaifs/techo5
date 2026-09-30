// Package ical reads an iCalendar file (RFC 5545) - what Google, iCloud and Outlook give as a
// calendar's private address - into the events that fall between two times, repeating ones expanded.
// Enough of the format for real calendars: all-day and timed events, time zones by name, RRULE by day,
// week, month and year with BYDAY, BYMONTHDAY and BYMONTH, COUNT and UNTIL, EXDATE, and occurrences
// moved or canceled on their own (RECURRENCE-ID).
package ical

import (
	"bufio"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Event is one occurrence.
type Event struct {
	UID                            string
	Summary, Description, Location string
	Start, End                     time.Time
	AllDay                         bool
}

// maxOccurrences bounds how far one repeating event is walked: a rule with no end that started
// decades ago still finishes.
const maxOccurrences = 20000

type prop struct {
	name   string
	params map[string]string
	value  string
}

type vevent struct {
	uid, summary, desc, loc string
	start, end              time.Time
	allDay                  bool
	hasEnd                  bool
	duration                time.Duration
	rrule                   string
	exdates                 []time.Time
	recurrenceID            time.Time
	canceled                bool
}

// Events is every occurrence in r that touches [from, to), in start order.
func Events(r io.Reader, from, to time.Time) ([]Event, error) {
	vs, err := parse(r)
	if err != nil {
		return nil, err
	}
	// Moved or canceled occurrences, by the event they belong to and the start they replace.
	type key struct {
		uid string
		at  int64
	}
	overrides := map[key]*vevent{}
	var masters []*vevent
	for _, v := range vs {
		if !v.recurrenceID.IsZero() {
			overrides[key{v.uid, v.recurrenceID.Unix()}] = v
			continue
		}
		masters = append(masters, v)
	}

	var out []Event
	add := func(v *vevent, start time.Time) {
		end := v.endFor(start)
		if v.canceled || !start.Before(to) || !(end.After(from) || (end.Equal(start) && !start.Before(from))) {
			return
		}
		out = append(out, Event{UID: v.uid, Summary: v.summary, Description: v.desc, Location: v.loc,
			Start: start.In(time.Local), End: end.In(time.Local), AllDay: v.allDay})
	}
	for _, v := range masters {
		for _, s := range v.occurrences(to) {
			if o, ok := overrides[key{v.uid, s.Unix()}]; ok {
				add(o, o.start)
				delete(overrides, key{v.uid, s.Unix()})
				continue
			}
			add(v, s)
		}
	}
	// An override of an occurrence the rule did not produce here still stands on its own.
	for _, o := range overrides {
		add(o, o.start)
	}
	slices.SortStableFunc(out, func(a, b Event) int { return a.Start.Compare(b.Start) })
	return out, nil
}

func (v *vevent) endFor(start time.Time) time.Time {
	switch {
	case v.hasEnd:
		if v.allDay {
			days := int(v.end.Sub(v.start).Hours()/24 + 0.5)
			return time.Date(start.Year(), start.Month(), start.Day()+days, 0, 0, 0, 0, start.Location())
		}
		return start.Add(v.end.Sub(v.start))
	case v.duration > 0:
		return start.Add(v.duration)
	case v.allDay:
		return time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 0, 0, start.Location())
	}
	return start
}

// parse reads the VEVENTs.
func parse(r io.Reader) ([]*vevent, error) {
	lines, err := unfold(r)
	if err != nil {
		return nil, err
	}
	var out []*vevent
	var cur *vevent
	depth := 0 // components inside a VEVENT (a VALARM) are skipped
	for _, line := range lines {
		p, ok := parseLine(line)
		if !ok {
			continue
		}
		switch {
		case p.name == "BEGIN" && strings.EqualFold(p.value, "VEVENT") && cur == nil:
			cur = &vevent{}
		case p.name == "BEGIN" && cur != nil:
			depth++
		case p.name == "END" && cur != nil && depth > 0:
			depth--
		case p.name == "END" && strings.EqualFold(p.value, "VEVENT") && cur != nil:
			if !cur.start.IsZero() {
				out = append(out, cur)
			}
			cur = nil
		case cur != nil && depth == 0:
			cur.set(p)
		}
	}
	return out, nil
}

func (v *vevent) set(p prop) {
	switch p.name {
	case "UID":
		v.uid = p.value
	case "SUMMARY":
		v.summary = text(p.value)
	case "DESCRIPTION":
		v.desc = text(p.value)
	case "LOCATION":
		v.loc = text(p.value)
	case "DTSTART":
		v.start, v.allDay = when(p)
	case "DTEND":
		v.end, _ = when(p)
		v.hasEnd = !v.end.IsZero()
	case "DURATION":
		v.duration = duration(p.value)
	case "RRULE":
		v.rrule = p.value
	case "EXDATE":
		for _, one := range strings.Split(p.value, ",") {
			q := p
			q.value = one
			if t, _ := when(q); !t.IsZero() {
				v.exdates = append(v.exdates, t)
			}
		}
	case "RECURRENCE-ID":
		v.recurrenceID, _ = when(p)
	case "STATUS":
		v.canceled = strings.EqualFold(p.value, "CANCELLED")
	}
}

// unfold joins the lines a long property was folded across.
func unfold(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var lines []string
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	return lines, sc.Err()
}

// parseLine splits NAME;PARAM=V;PARAM=V:VALUE, with quoted parameter values.
func parseLine(l string) (prop, bool) {
	p := prop{params: map[string]string{}}
	inQuote := false
	colon := -1
	for i, c := range l {
		if c == '"' {
			inQuote = !inQuote
		}
		if c == ':' && !inQuote {
			colon = i
			break
		}
	}
	if colon < 0 {
		return p, false
	}
	head, value := l[:colon], l[colon+1:]
	parts := strings.Split(head, ";")
	p.name = strings.ToUpper(parts[0])
	for _, kv := range parts[1:] {
		if k, v, ok := strings.Cut(kv, "="); ok {
			p.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	p.value = value
	return p, true
}

func text(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return strings.TrimSpace(r.Replace(s))
}

// when reads a DATE or DATE-TIME, in its TZID, in UTC with a Z, or on the device's own clock when it
// names neither; and whether it was a date (an all-day event).
func when(p prop) (time.Time, bool) {
	v := strings.TrimSpace(p.value)
	if p.params["VALUE"] == "DATE" || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, time.Local)
		if err != nil {
			return time.Time{}, false
		}
		return t, true
	}
	loc := time.Local
	if strings.HasSuffix(v, "Z") {
		loc = time.UTC
		v = strings.TrimSuffix(v, "Z")
	} else if tz := p.params["TZID"]; tz != "" {
		loc = zone(tz)
	}
	t, err := time.ParseInLocation("20060102T150405", v, loc)
	if err != nil {
		return time.Time{}, false
	}
	// Kept in its own zone: a repeating event repeats at its own wall clock time, across the change of
	// clocks, and only what comes out is put on the device's.
	return t, false
}

// windowsZones is Outlook's names for the zones it is most likely to send.
var windowsZones = map[string]string{
	"Eastern Standard Time": "America/New_York", "Central Standard Time": "America/Chicago",
	"Mountain Standard Time": "America/Denver", "US Mountain Standard Time": "America/Phoenix",
	"Pacific Standard Time": "America/Los_Angeles", "Alaskan Standard Time": "America/Anchorage",
	"Hawaiian Standard Time": "Pacific/Honolulu", "Atlantic Standard Time": "America/Halifax",
	"GMT Standard Time": "Europe/London", "W. Europe Standard Time": "Europe/Berlin",
	"Romance Standard Time": "Europe/Paris", "Central Europe Standard Time": "Europe/Budapest",
	"UTC": "UTC",
}

func zone(name string) *time.Location {
	name = strings.Trim(name, `"`)
	if l, err := time.LoadLocation(name); err == nil {
		return l
	}
	if iana, ok := windowsZones[name]; ok {
		if l, err := time.LoadLocation(iana); err == nil {
			return l
		}
	}
	// Names like "/mozilla.org/20050126_1/America/Denver" end in one that loads.
	if i := strings.Index(name, "/"); i >= 0 {
		for s := name; s != ""; {
			if l, err := time.LoadLocation(s); err == nil {
				return l
			}
			j := strings.Index(s, "/")
			if j < 0 {
				break
			}
			s = s[j+1:]
		}
	}
	return time.Local
}

// duration reads P1DT2H30M, P2W and the like.
func duration(s string) time.Duration {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "+"), "P")
	var d time.Duration
	num := ""
	inTime := false
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			num += string(c)
		case c == 'T':
			inTime = true
		default:
			n, _ := strconv.Atoi(num)
			num = ""
			switch {
			case c == 'W':
				d += time.Duration(n) * 7 * 24 * time.Hour
			case c == 'D':
				d += time.Duration(n) * 24 * time.Hour
			case c == 'H' && inTime:
				d += time.Duration(n) * time.Hour
			case c == 'M' && inTime:
				d += time.Duration(n) * time.Minute
			case c == 'S' && inTime:
				d += time.Duration(n) * time.Second
			}
		}
	}
	return d
}
