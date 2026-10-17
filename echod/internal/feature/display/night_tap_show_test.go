//go:build !dot && !spot

package display

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// A tap on the night light does nothing until Tap lifts the night light is on (#163); then it brings
// the screen back as by day, ahead of the clock's own tap, and the night light comes back once the
// room has been quiet for nightIdle.
func TestATapLiftsTheNightLightWhenSet(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	atNight(t)
	if err := config.Set().Screen().AtNight(true, true); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Dashboard().Mode(config.DashboardDrawn); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Screen().ClockTap("dashboard"); err != nil {
		t.Fatal(err)
	}
	tap := touch.Gesture{Kind: touch.Tap, X: 480, Y: 240}

	d := &Display{on: true, ceiling: 60, nightGlow: true, poke: make(chan struct{}, 1)}
	d.nightTapSw = nightTapSwitch(d)
	d.gesture(tap)
	if !d.nightGlow {
		t.Fatal("a tap lifted the night light with the setting off")
	}

	d = &Display{on: true, ceiling: 60, nightGlow: true, poke: make(chan struct{}, 1)}
	d.nightTapSw = nightTapSwitch(d)
	d.nightTapSw.OnCommand(true)
	if !config.Get().Screen.NightTapLifts {
		t.Fatal("the switch did not save the setting")
	}
	d.gesture(tap)
	if d.nightGlow {
		t.Fatal("a tap left the night light up with the setting on")
	}
	if d.dash {
		t.Error("the tap that lifted the night light also opened the dashboard under the finger")
	}

	idle := voice.State{Phase: "idle"}
	d.night(time.Now(), true, idle)
	if d.nightGlow {
		t.Error("the night light came back while the screen was still being used")
	}
	d.mu.Lock()
	d.touchedAt = time.Now().Add(-nightIdle - time.Second)
	d.mu.Unlock()
	d.night(time.Now(), true, idle)
	if !d.nightGlow {
		t.Error("the night light did not come back once the room was quiet")
	}
}
