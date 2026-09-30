//go:build !dot && !spot

package display

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 9, day, hour, minute, 0, 0, time.Local)
}

func turned(t *testing.T, v string, when time.Time) {
	t.Helper()
	if err := config.Set().Screen().NightOverride(v, when.Unix()); err != nil {
		t.Fatal(err)
	}
}

// With hours set, Night mode turned on or off holds until the hours next start or end the night.
func TestNightModeOverHours(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Screen().Night("22-6"); err != nil {
		t.Fatal(err)
	}
	if !nightNow(at(10, 23, 0)) || nightNow(at(10, 14, 0)) {
		t.Fatal("the hours alone are wrong")
	}

	// Turned off at night: day until the hours next start it.
	turned(t, "off", at(10, 23, 0))
	if nightNow(at(10, 23, 30)) || nightNow(at(11, 3, 0)) {
		t.Error("night mode off did not end the night")
	}
	if nightNow(at(11, 14, 0)) {
		t.Error("the day after, it is day")
	}
	if !nightNow(at(11, 22, 30)) {
		t.Error("the next night did not start on time")
	}

	// Turned on in the afternoon: night now, until the hours end the night in the morning.
	turned(t, "on", at(12, 15, 0))
	if !nightNow(at(12, 16, 0)) || !nightNow(at(12, 23, 0)) {
		t.Error("night mode on did not start the night")
	}
	if nightNow(at(13, 7, 0)) {
		t.Error("the morning after, night mode on still holds")
	}
}

// Left to Home Assistant, the hours do nothing and the switch holds until it is turned again.
func TestNightModeControlledByHA(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Screen().Night("22-6"); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Screen().NightByHA(true); err != nil {
		t.Fatal(err)
	}
	if nightNow(at(10, 23, 0)) {
		t.Error("the hours still start the night when Home Assistant has it")
	}
	turned(t, "on", at(10, 21, 0))
	if !nightNow(at(10, 21, 30)) || !nightNow(at(12, 14, 0)) {
		t.Error("night mode on does not hold")
	}
	turned(t, "off", at(12, 15, 0))
	if nightNow(at(12, 23, 0)) {
		t.Error("night mode off does not hold")
	}
}
