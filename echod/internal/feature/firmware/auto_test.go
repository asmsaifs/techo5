package firmware

import (
	"testing"
	"time"
)

// Overnight, once a night, only while quiet, only when switched on.
func TestAutoInstallIsDueOvernightOnce(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 9, 30, h, m, 0, 0, time.Local) }
	cases := []struct {
		on    bool
		now   time.Time
		tried string
		busy  bool
		want  bool
		why   string
	}{
		{true, at(2, 0), "", false, true, "the window opens"},
		{true, at(4, 59), "", false, true, "the window's last minutes"},
		{true, at(5, 0), "", false, false, "the window has closed"},
		{true, at(1, 59), "", false, false, "not yet"},
		{true, at(14, 0), "", false, false, "the afternoon"},
		{true, at(3, 0), "2026-09-30", false, false, "tried tonight already"},
		{true, at(3, 0), "2026-09-29", false, true, "tried last night"},
		{true, at(3, 0), "", true, false, "music playing"},
		{false, at(3, 0), "", false, false, "switched off"},
	}
	for _, c := range cases {
		if got := autoDue(c.on, c.now, c.tried, c.busy); got != c.want {
			t.Errorf("%s: autoDue = %v, want %v", c.why, got, c.want)
		}
	}
}
