package ical

import (
	"strings"
	"testing"
	"time"
)

func cal(events ...string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" + strings.Join(events, "") + "END:VCALENDAR\r\n"
}

func ev(lines ...string) string {
	return "BEGIN:VEVENT\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VEVENT\r\n"
}

func events(t *testing.T, ics string, from, to time.Time) []Event {
	t.Helper()
	out, err := Events(strings.NewReader(ics), from, to)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func denver(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skip("no zone database")
	}
	return l
}

func TestOneOffAndAllDay(t *testing.T) {
	old := time.Local
	time.Local = denver(t)
	defer func() { time.Local = old }()
	ics := cal(
		ev("UID:a", "SUMMARY:Dentist", "DTSTART;TZID=America/Denver:20261005T140000", "DTEND;TZID=America/Denver:20261005T150000"),
		ev("UID:b", "SUMMARY:Trip", "DTSTART;VALUE=DATE:20261010", "DTEND;VALUE=DATE:20261013", "LOCATION:Lake\\, cabin"),
		ev("UID:c", "SUMMARY:Call", "DTSTART:20261007T200000Z", "DURATION:PT30M"),
	)
	got := events(t, ics, time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local), time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local))
	if len(got) != 3 {
		t.Fatalf("got %d events: %+v", len(got), got)
	}
	if got[0].Summary != "Dentist" || got[0].Start.Hour() != 14 || got[0].End.Sub(got[0].Start) != time.Hour {
		t.Errorf("dentist: %+v", got[0])
	}
	if got[1].Summary != "Call" || got[1].Start.Hour() != 14 || got[1].End.Sub(got[1].Start) != 30*time.Minute {
		t.Errorf("call at 20:00 UTC is 14:00 in Denver: %+v", got[1])
	}
	if !got[2].AllDay || got[2].Start.Day() != 10 || got[2].End.Day() != 13 || got[2].Location != "Lake, cabin" {
		t.Errorf("trip: %+v", got[2])
	}
}

// A weekly class on Tuesdays and Thursdays, one week skipped, keeps its hour across the change of clocks.
func TestWeeklyWithAnExceptionAcrossTheClockChange(t *testing.T) {
	old := time.Local
	time.Local = denver(t)
	defer func() { time.Local = old }()
	ics := cal(ev("UID:w", "SUMMARY:Class", "DTSTART;TZID=America/Denver:20261020T180000", "DTEND;TZID=America/Denver:20261020T190000",
		"RRULE:FREQ=WEEKLY;BYDAY=TU,TH;UNTIL=20261115T235959Z", "EXDATE;TZID=America/Denver:20261029T180000"))
	got := events(t, ics, time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local), time.Date(2026, 12, 1, 0, 0, 0, 0, time.Local))
	var days []int
	for _, e := range got {
		days = append(days, e.Start.Day())
		if e.Start.Hour() != 18 {
			t.Errorf("%v is not at six in the evening", e.Start)
		}
	}
	want := []int{20, 22, 27, 3, 5, 10, 12} // the 29th is skipped; the rule ends on the 15th
	if len(days) != len(want) {
		t.Fatalf("days %v, want %v", days, want)
	}
	for i := range want {
		if days[i] != want[i] {
			t.Fatalf("days %v, want %v", days, want)
		}
	}
}

func TestMonthlyAndYearly(t *testing.T) {
	ics := cal(
		ev("UID:m", "SUMMARY:Payday", "DTSTART:20260130T090000", "RRULE:FREQ=MONTHLY;BYDAY=-1FR"),
		ev("UID:y", "SUMMARY:Birthday", "DTSTART;VALUE=DATE:19900314", "RRULE:FREQ=YEARLY"),
		ev("UID:t", "SUMMARY:Thanksgiving", "DTSTART;VALUE=DATE:20201126", "RRULE:FREQ=YEARLY;BYMONTH=11;BYDAY=4TH"),
		ev("UID:31", "SUMMARY:Rent", "DTSTART:20260131T080000", "RRULE:FREQ=MONTHLY;COUNT=4"),
	)
	got := events(t, ics, time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local), time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local))
	count := map[string][]string{}
	for _, e := range got {
		count[e.Summary] = append(count[e.Summary], e.Start.Format("01-02"))
	}
	if len(count["Payday"]) != 12 || count["Payday"][9] != "10-30" {
		t.Errorf("last Fridays: %v", count["Payday"])
	}
	if len(count["Birthday"]) != 1 || count["Birthday"][0] != "03-14" {
		t.Errorf("birthday: %v", count["Birthday"])
	}
	if len(count["Thanksgiving"]) != 1 || count["Thanksgiving"][0] != "11-26" {
		t.Errorf("thanksgiving 2026: %v", count["Thanksgiving"])
	}
	// The 31st happens in January, March, May and July: four times, none of them moved into another month.
	if strings.Join(count["Rent"], " ") != "01-31 03-31 05-31 07-31" {
		t.Errorf("rent: %v", count["Rent"])
	}
}

func TestMovedAndCanceledOccurrences(t *testing.T) {
	ics := cal(
		ev("UID:s", "SUMMARY:Standup", "DTSTART:20261005T090000", "DTEND:20261005T091500", "RRULE:FREQ=DAILY;COUNT=5"),
		ev("UID:s", "SUMMARY:Standup (late)", "RECURRENCE-ID:20261006T090000", "DTSTART:20261006T110000", "DTEND:20261006T111500"),
		ev("UID:s", "SUMMARY:Standup", "RECURRENCE-ID:20261008T090000", "DTSTART:20261008T090000", "STATUS:CANCELLED"),
	)
	got := events(t, ics, time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local), time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local))
	var s []string
	for _, e := range got {
		s = append(s, e.Start.Format("02 15:04")+" "+e.Summary)
	}
	want := "05 09:00 Standup|06 11:00 Standup (late)|07 09:00 Standup|09 09:00 Standup"
	if strings.Join(s, "|") != want {
		t.Errorf("got %q\nwant %q", strings.Join(s, "|"), want)
	}
}

// Outlook names zones its own way, long lines are folded, and alarms inside an event are not events.
func TestOutlookZonesFoldingAndAlarms(t *testing.T) {
	old := time.Local
	time.Local = denver(t)
	defer func() { time.Local = old }()
	ics := cal(ev("UID:o", "SUMMARY:A meeting with a very long", " name that was folded",
		"DTSTART;TZID=Eastern Standard Time:20261012T100000", "DTEND;TZID=Eastern Standard Time:20261012T110000",
		"BEGIN:VALARM", "TRIGGER:-PT15M", "SUMMARY:not an event", "END:VALARM"))
	got := events(t, ics, time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local), time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local))
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Summary != "A meeting with a very longname that was folded" || got[0].Start.Hour() != 8 {
		t.Errorf("got %+v (10:00 Eastern is 8:00 Mountain)", got[0])
	}
}
