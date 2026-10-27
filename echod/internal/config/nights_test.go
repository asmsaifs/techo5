package config

import (
	"testing"
	"time"
)

func TestInNights(t *testing.T) {
	// 2026-10-23 is a Friday.
	at := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, time.UTC) }
	for _, c := range []struct {
		nights, weekend string
		t               time.Time
		want            bool
	}{
		{"22-7", "", at(22, 23, 0), true},     // Thursday night
		{"22-7", "", at(23, 6, 59), true},     // Friday morning
		{"22-7", "", at(23, 7, 0), false},     // Friday, day
		{"22-7", "0-9", at(23, 23, 0), false}, // Friday evening: Saturday's night starts at midnight
		{"22-7", "0-9", at(24, 0, 30), true},  // Saturday's small hours
		{"22-7", "0-9", at(24, 8, 0), true},   // Saturday morning, sleeping in
		{"22-7", "0-9", at(23, 6, 0), true},   // Friday morning is a weeknight's, to 7
		{"22-7", "0-9", at(23, 8, 0), false},
		{"22-7", "23:30-09:30", at(23, 23, 0), false}, // Friday 23:00: Saturday's night starts at 23:30
		{"22-7", "23:30-09:30", at(23, 23, 45), true},
		{"22-7", "23:30-09:30", at(25, 9, 0), true},   // Sunday morning
		{"22-7", "23:30-09:30", at(25, 22, 30), true}, // Sunday evening: Monday's night, a weeknight
		{"22-7", "23:30-09:30", at(26, 8, 0), false},  // Monday morning ends at 7
		{"", "23-9", at(24, 1, 0), true},              // weekend nights only
		{"", "23-9", at(22, 23, 30), false},
		{"1-6", "", at(22, 2, 0), true}, // a night that starts after midnight
		{"1-6", "", at(22, 0, 30), false},
	} {
		if got := InNights(c.nights, c.weekend, c.t); got != c.want {
			t.Errorf("%q/%q at %s: %v, want %v", c.nights, c.weekend, c.t.Format("Mon 15:04"), got, c.want)
		}
	}
	// With no weekend hours it is InWindow.
	for _, w := range []string{"22-7", "1-6", "19:30-09:15", ""} {
		for h := range 24 {
			tm := at(24, h, 15)
			if InNights(w, "", tm) != InWindow(w, tm) {
				t.Errorf("%q at %s differs from InWindow", w, tm.Format("15:04"))
			}
		}
	}
}

func TestNightChanges(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) // Friday noon
	ch := NightChanges("22-7", "0-9", at)
	want := map[string]bool{
		"Wed 22:00": true, "Thu 07:00": true, // Thursday morning's
		"Thu 22:00": true, "Fri 07:00": true, // Friday morning's
		"Sat 00:00": true, "Sat 09:00": true, // Saturday's, the weekend's
		"Sun 00:00": true, "Sun 09:00": true, // Sunday's
	}
	got := map[string]bool{}
	for _, c := range ch {
		got[c.Format("Mon 15:04")] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing %s in %v", k, got)
		}
	}
	if len(ch) != len(want) {
		t.Errorf("%d changes, want %d", len(ch), len(want))
	}
	if to, ok := NightEndOn("22-7", "0-9", time.Date(2026, 10, 25, 3, 0, 0, 0, time.UTC)); !ok || to != 9*60 {
		t.Errorf("Sunday's night ends at %d %v", to, ok)
	}
}
