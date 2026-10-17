//go:build !dot && !spot

package display

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The Alarms card with the wake light held after an alarm, for looking at with SHOW_PREVIEW set.
func TestHeldLightRowDraws(t *testing.T) {
	at := time.Date(2026, 10, 16, 8, 12, 0, 0, time.Local)
	for name, hold := range map[string]struct {
		until  time.Time
		snooze bool
	}{
		"settings-alarms-light-held":    {at.Add(18 * time.Minute), false},
		"settings-alarms-light-snoozed": {at.Add(6 * time.Minute), true},
	} {
		heldLight(t, hold.until, hold.snooze, true)
		img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
		newRenderer(img).draw(scene{now: at, phase: "idle", showSheet: true, snooze: 9,
			sheet: settings{cat: catAlarms, cardScroll: 1000}})
		dir := os.Getenv("SHOW_PREVIEW")
		if dir == "" {
			continue
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// The screen turned off under the held wake light stays off for that light, and shows it again once
// it is over or an alarm rings (#161). It used to be turned straight back on.
func TestTheScreenTurnedOffUnderTheHeldLightStaysOff(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	now := time.Date(2026, 10, 16, 8, 12, 0, 0, time.Local)
	d := &Display{}

	heldLight(t, time.Time{}, false, false)
	d.turnedOff(now)
	if d.wakeLight(now) == 0 {
		t.Error("turned off while the light only comes up, the light is gone from the screen")
	}

	heldLight(t, now.Add(18*time.Minute), false, true)
	d.turnedOff(now)
	if got := d.wakeLight(now); got != 0 {
		t.Errorf("turned off under the held light, the screen still shows %.2f of it", got)
	}
	d.lightOffOver(now, false)
	if d.wakeLight(now) != 0 {
		t.Error("the screen took the light back while it was still held")
	}
	d.lightOffOver(now, true)
	if d.wakeLight(now) != 1 {
		t.Error("an alarm rang and the screen does not show the light after it")
	}

	d.turnedOff(now)
	sunriseProgress = func(time.Time) float64 { return 0 }
	d.lightOffOver(now, false)
	sunriseProgress = func(time.Time) float64 { return 1 }
	if d.wakeLight(now) != 1 {
		t.Error("the light ended and the next one stays off the screen")
	}
}

// The screen turned off under the held wake light stays off after a restart, including the moment
// before Home Assistant says what a helper is set to, when nothing holds the light yet; and lights
// for it again once that light is over. It used to come back on with the restart.
func TestTheScreenTurnedOffUnderTheHeldLightStaysOffAfterARestart(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	now := time.Date(2026, 10, 16, 8, 12, 0, 0, time.Local)
	until := now.Add(18 * time.Minute)

	heldLight(t, until, false, true)
	(&Display{}).turnedOff(now)
	if got := config.Get().Alarms.HeldDark; !got.Equal(until) {
		t.Fatalf("saved %v for the screen turned off, want %v", got, until)
	}

	d := &Display{}
	d.restoreLightOff(config.Get().Alarms.HeldDark, now)
	heldLight(t, time.Time{}, false, false)
	sunriseProgress = func(time.Time) float64 { return 0 } // Home Assistant has not said yet
	d.lightOffOver(now, false)
	heldLight(t, until, false, true)
	if got := d.wakeLight(now); got != 0 {
		t.Errorf("after a restart the screen shows %.2f of the light it was turned off under", got)
	}
	d.lightOffOver(now, false)
	if d.wakeLight(now) != 0 {
		t.Error("after a restart the screen took the light back while it was still held")
	}

	sunriseProgress = func(time.Time) float64 { return 0 }
	d.lightOffOver(now, false)
	if !config.Get().Alarms.HeldDark.IsZero() {
		t.Error("the light is over and the screen turned off under it is still saved")
	}
	sunriseProgress = func(time.Time) float64 { return 1 }
	if d.wakeLight(now) != 1 {
		t.Error("the light ended and the next one stays off the screen")
	}

	// Saved for a moment that went by while the device was down, it does not come back.
	if err := config.Set().Alarms().HeldDark(now.Add(-5 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	late := &Display{}
	late.restoreLightOff(config.Get().Alarms.HeldDark, now)
	if late.wakeLight(now) != 1 {
		t.Error("restored for a light whose moment went by, the screen stays off")
	}
	if !config.Get().Alarms.HeldDark.IsZero() {
		t.Error("a screen turned off under a light whose moment went by is still saved")
	}

	// Never seen again, the light it waited for gone by unseen, it lets go too.
	sunriseProgress = func(time.Time) float64 { return 0 }
	gone := &Display{}
	gone.restoreLightOff(until, now)
	gone.lightOffOver(until.Add(2*time.Minute), false)
	if gone.lightOff.Load() {
		t.Error("restored, the screen stays off past the moment its light waited for")
	}
}

// Restored after a restart, the screen stays off only for the hold it was turned off under. Another
// alarm's own ramp lit first is a new light, and the screen shows it.
func TestARestoredDarkScreenShowsAnotherAlarmsLight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	now := time.Date(2026, 10, 16, 8, 12, 0, 0, time.Local)
	until := now.Add(18 * time.Minute)
	if err := config.Set().Alarms().HeldDark(until); err != nil {
		t.Fatal(err)
	}

	d := &Display{}
	d.restoreLightOff(config.Get().Alarms.HeldDark, now)
	heldLight(t, time.Time{}, false, false) // another alarm's ramp, at 0.4, and nothing held
	d.lightOffOver(now, false)
	if got := d.wakeLight(now); got != 0.4 {
		t.Errorf("another alarm's light is %.2f on the screen, want 0.40", got)
	}
	if !config.Get().Alarms.HeldDark.IsZero() {
		t.Error("the screen shows another light and the dark one is still saved")
	}

	// Held for some other moment than the one saved, the same.
	if err := config.Set().Alarms().HeldDark(until); err != nil {
		t.Fatal(err)
	}
	other := &Display{}
	other.restoreLightOff(until, now)
	heldLight(t, until.Add(30*time.Minute), false, true)
	other.lightOffOver(now, false)
	if other.wakeLight(now) != 1 {
		t.Error("a light held for another alarm stays off the screen")
	}
}
