package alarm

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Home Assistant hears the wake light come on and go off at once, and in between no more than every
// lightEvery, however often the ring is repainted.
func TestHomeAssistantFollowsTheWakeLight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()
	start := time.Date(2026, 10, 16, 7, 30, 0, 0, time.UTC)
	told := func() (int, string) { return int(a.lightLevel.Get()), a.lightColor.Get() }

	for _, st := range []struct {
		after   time.Duration
		p       float64
		percent int
		color   string
	}{
		{0, 0, 0, ""},                          // the first look says it is off
		{2 * time.Second, 0.1, 3, "#ff3f0a"},   // on: told at once
		{4 * time.Second, 0.2, 3, "#ff3f0a"},   // two seconds on: not yet
		{7 * time.Second, 0.3, 11, "#ff6d0a"},  // five seconds on
		{8 * time.Second, 1, 11, "#ff6d0a"},    // full, a second later: waits
		{12 * time.Second, 1, 100, "#fff0be"},  // and is told
		{13 * time.Second, 0, 0, ""},           // off: told at once
		{14 * time.Second, 0.5, 27, "#ff9b1a"}, // on again: at once
	} {
		a.tellLight(st.p, start.Add(st.after))
		if percent, color := told(); percent != st.percent || color != st.color {
			t.Errorf("at +%v with the light %.2f along, Home Assistant has %d%% %q, want %d%% %q",
				st.after, st.p, percent, color, st.percent, st.color)
		}
	}
}
