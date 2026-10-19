package home

import (
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
)

// The same event on two calendars shows once, the first calendar's copy, naming the other;
// one differing in its name or its times, or two copies on one calendar, are left as they are.
func TestTheSameEventOnTwoCalendarsShowsOnce(t *testing.T) {
	at := time.Date(2026, 10, 17, 18, 0, 0, 0, time.Local)
	ev := func(cal, name string, start time.Time, mins int) hass.Event {
		return hass.Event{Calendar: cal, Summary: name, Start: start, End: start.Add(time.Duration(mins) * time.Minute)}
	}
	got := mergeSame([]hass.Event{
		ev("calendar.family", "Dinner", at, 60),
		ev("calendar.work", "Dinner ", at, 60), // the same, spaces aside
		ev("calendar.kids", "Dinner", at, 60),
		ev("calendar.work", "Dinner out", at, 60),
		ev("calendar.work", "Dinner", at, 75),   // ends later: another event
		ev("calendar.family", "Dinner", at, 60), // a second copy on one calendar
	})
	if len(got) != 4 {
		t.Fatalf("%d events, want 4: %+v", len(got), got)
	}
	first := got[0]
	if first.Calendar != "calendar.family" || len(first.Also) != 2 || first.Also[0] != "calendar.work" || first.Also[1] != "calendar.kids" {
		t.Errorf("merged %+v, want the family copy naming the work and kids ones", first)
	}
	if got[3].Calendar != "calendar.family" || got[3].Summary != "Dinner" {
		t.Errorf("a second copy on one calendar was merged: %+v", got[3])
	}
}

// An all-day event and a timed one with the same name and times are not the same.
func TestAllDayIsNotMergedWithTimed(t *testing.T) {
	day := time.Date(2026, 10, 18, 0, 0, 0, 0, time.Local)
	got := mergeSame([]hass.Event{
		{Calendar: "calendar.family", Summary: "Trip", Start: day, End: day.AddDate(0, 0, 1), AllDay: true},
		{Calendar: "calendar.work", Summary: "Trip", Start: day, End: day.AddDate(0, 0, 1)},
	})
	if len(got) != 2 {
		t.Errorf("%d events, want 2", len(got))
	}
}

// A merged event is on each of its calendars.
func TestOnCalendar(t *testing.T) {
	e := hass.Event{Calendar: "calendar.family", Also: []string{"calendar.work"}}
	if !OnCalendar(e, "calendar.family") || !OnCalendar(e, "calendar.work") || OnCalendar(e, "calendar.kids") {
		t.Error("OnCalendar is wrong")
	}
}
