package ring

import "testing"

func TestSnoozeAsked(t *testing.T) {
	for _, c := range []struct {
		text    string
		minutes int
		ok      bool
	}{
		{"Snooze.", 0, true},
		{"snooze the alarm", 0, true},
		{"Snooze it, please.", 0, true},
		{"Snooze for ten minutes.", 10, true},
		{"snooze for 10 minutes", 10, true},
		{"Snooze the alarm for fifteen minutes", 15, true},
		{"snooze another 5 minutes", 5, true},
		{"Snooze for twenty five minutes.", 25, true},
		{"snooze for twenty-five minutes", 25, true},
		{"snooze for a minute", 1, true},
		{"snooze for half an hour", 30, true},
		{"give me ten more minutes, snooze", 10, true},

		// Not a snooze, or not one this reads: left to Home Assistant.
		{"", 0, false},
		{"ten minutes", 0, false},
		{"set a timer for ten minutes", 0, false},
		{"snooze my phone", 0, false},
		{"snooze for ten", 0, false},
		{"snooze for ten minutes and five minutes", 0, false},
		{"snooze for an hour", 0, false},
		{"stop the alarm", 0, false},
	} {
		minutes, ok := SnoozeAsked(c.text)
		if minutes != c.minutes || ok != c.ok {
			t.Errorf("SnoozeAsked(%q) = %d, %v; want %d, %v", c.text, minutes, ok, c.minutes, c.ok)
		}
	}
}
