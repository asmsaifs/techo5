//go:build spot

package display

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/screen"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// spotNightDisplay is a lit Spot with a fresh config, inside night hours round now when night is true
// and outside them otherwise, set to the night clock, and nobody having touched it or spoken for an
// hour.
func spotNightDisplay(t *testing.T, night bool) *Display {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	h := time.Now().Hour()
	window := fmt.Sprintf("%d-%d", (h+23)%24, (h+2)%24) // an hour either side of now
	if !night {
		window = fmt.Sprintf("%d-%d", (h+20)%24, (h+22)%24) // ended a while ago
	}
	if err := config.Set().Screen().Night(window); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Screen().AtNight(true, true); err != nil {
		t.Fatal(err)
	}
	d := &Display{
		poke: make(chan struct{}, 1),
		view: voice.State{Phase: "idle"},
		light: &esphome.Light{
			Base:                esphome.Base{ObjectID: "screen", Name: "Screen", Icon: "mdi:monitor"},
			SupportedColorModes: []esphome.ColorMode{esphome.ColorModeBrightness},
		},
		on: true, ceiling: 60,
	}
	d.touchedAt = time.Now().Add(-time.Hour)
	d.viewAt = time.Now().Add(-time.Hour)
	return d
}

func (d *Display) glowingNow() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.nightGlow
}

// shownRed is the frame drawing the red clock, as frame records it.
func (d *Display) shownRed() {
	d.mu.Lock()
	d.redShown = true
	d.mu.Unlock()
}

// The night clock comes up on a face nobody is using, at the night light's level rather than the
// night's dimmed brightness; a tap takes it down to the face as it is by night, and the face stays up
// while it is being used.
func TestTheSpotNightClockComesUpAndATapLiftsIt(t *testing.T) {
	d := spotNightDisplay(t, true)
	if !d.night(time.Now(), true, voice.State{Phase: "idle"}) || !d.glowingNow() {
		t.Fatal("the night clock did not come up on an idle face")
	}
	d.mu.Lock()
	level := d.level
	d.mu.Unlock()
	if want := float64(glowSteps[glowDefault-1]); level != want {
		t.Errorf("the night clock's backlight is %v, want %v", level, want)
	}
	if level >= backlightMin {
		t.Errorf("the night clock is as bright as the dimmest face (%v)", level)
	}

	d.shownRed()
	d.gesture(touch.Gesture{Kind: touch.Tap, X: center, Y: center})
	if d.glowingNow() {
		t.Fatal("a tap left the night clock up")
	}
	d.mu.Lock()
	level, open := d.level, d.menuOpen
	d.mu.Unlock()
	if want := backlightMin + (screen.BacklightMax-backlightMin)*nightCeiling/100.0; level != want {
		t.Errorf("lifted, the backlight is %v, want the night's dimmed %v", level, want)
	}
	if open {
		t.Error("a tap on the night clock opened the menu")
	}
	if d.night(time.Now(), true, voice.State{Phase: "idle"}) || d.glowingNow() {
		t.Error("the night clock came back while the face was being used")
	}
}

// A long press on the night clock takes it down and opens the ring menu, which is the way to
// everything on the Spot.
func TestALongPressOnTheSpotNightClockOpensTheMenu(t *testing.T) {
	d := spotNightDisplay(t, true)
	d.night(time.Now(), true, voice.State{Phase: "idle"})
	d.shownRed()
	d.gesture(touch.Gesture{Kind: touch.Hold, X: center, Y: 40})
	d.mu.Lock()
	glowing, open, mode := d.nightGlow, d.menuOpen, d.menuMode
	d.closeMenu()
	d.mu.Unlock()
	if glowing || !open || mode != modeMain {
		t.Errorf("after a long press: glow=%v menu=%v mode=%v, want the menu up and the clock down", glowing, open, mode)
	}
}

// A turn leaves the night clock as it is (the turn is shown at its level, the clock coming back after);
// so does something playing, which is not somebody using the face.
func TestTheSpotNightClockStaysForATurn(t *testing.T) {
	d := spotNightDisplay(t, true)
	d.night(time.Now(), true, voice.State{Phase: "idle"})
	d.changed(voice.State{Phase: "replying", Reply: "It is 3:47."})
	if d.night(time.Now(), true, voice.State{Phase: "replying"}) || !d.glowingNow() {
		t.Error("a turn took the night clock down")
	}
}

// Set to Dim, the night only dims, as the Spot's night always has; and the end of the night, or the
// face put to sleep, takes the night clock down.
func TestTheSpotNightClockOnlyWhenChosenAndOnlyAtNight(t *testing.T) {
	d := spotNightDisplay(t, true)
	if err := config.Set().Screen().AtNight(false, false); err != nil {
		t.Fatal(err)
	}
	if d.night(time.Now(), true, voice.State{Phase: "idle"}) || d.glowingNow() {
		t.Error("the night clock came up on a Spot set to dim")
	}

	d = spotNightDisplay(t, false)
	d.nightGlow = true
	if !d.night(time.Now(), true, voice.State{Phase: "idle"}) || d.glowingNow() {
		t.Error("the end of the night left the night clock up")
	}

	d = spotNightDisplay(t, true)
	d.night(time.Now(), true, voice.State{Phase: "idle"})
	if !d.night(time.Now(), false, voice.State{Phase: "idle"}) || d.glowingNow() {
		t.Error("a face put to sleep kept the night clock for when it wakes")
	}
}

// Choosing Dim while the night clock is up takes it down at once.
func TestChoosingDimTakesTheSpotNightClockDown(t *testing.T) {
	d := spotNightDisplay(t, true)
	d.night(time.Now(), true, voice.State{Phase: "idle"})
	d.setAtNight(0)
	if d.glowingNow() {
		t.Error("the night clock stayed after choosing Dim")
	}
	if atNightOptions[atNightIndex()] != "Dim" {
		t.Errorf("Screen at night says %q", atNightOptions[atNightIndex()])
	}
	d.setAtNight(1)
	if !nightClockOn() || atNightOptions[atNightIndex()] != "Night clock" {
		t.Error("Night clock was not taken")
	}
}

// The Spot's hours: none set is 22:00 to 07:00, as it always was, and Never is a value of its own,
// which the action's two empty times set.
func TestTheSpotNightHours(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if nightHoursSet() != defaultNight || nightHoursLabel(nightHoursSet()) != "22:00 – 07:00" {
		t.Errorf("no hours set: %q, shown as %q", nightHoursSet(), nightHoursLabel(nightHoursSet()))
	}
	if !nightNow(at(10, 23, 0)) || nightNow(at(10, 7, 30)) {
		t.Error("the default hours are not 22:00 to 07:00")
	}
	d := &Display{poke: make(chan struct{}, 1)}
	if err := d.setNightHours("", ""); err != nil {
		t.Fatal(err)
	}
	if nightNow(at(10, 23, 0)) || nightHoursLabel(nightHoursSet()) != "Never" {
		t.Errorf("no night is still a night: %q", config.Get().Screen.Night)
	}
}

// Each night clock look draws the time on black and nothing brighter than its own ink, muted too, and
// it stays the whole face while something rings, as on the Show.
func TestTheSpotNightClockStaysDim(t *testing.T) {
	now := time.Date(2026, 9, 26, 3, 47, 0, 0, time.Local)
	for _, style := range []string{nightStylePlain, nightStyleLED, nightStyleFlip} {
		ink := nightRed
		if style == nightStyleFlip {
			ink = flipInk
		}
		for _, s := range []roundScene{
			{now: now, phase: "idle", redClock: true, redStyle: style},
			{now: now, phase: "idle", redClock: true, redStyle: style, muted: true, ringing: ringing{timerOn: true, timer: "Pasta"}},
		} {
			r := newRoundRenderer(image.NewRGBA(image.Rect(0, 0, side, side)))
			r.draw(s)
			lit := 0
			for y := range side {
				for x := range side {
					c := r.dst.RGBAAt(x, y)
					if luma(c.R, c.G, c.B) > luma(ink.R, ink.G, ink.B)+1 {
						t.Fatalf("%q: a pixel brighter than its ink at (%d, %d): %v", style, x, y, c)
					}
					if c == ink {
						lit++
					}
				}
			}
			if lit < 2000 {
				t.Errorf("%q: only %d pixels in its ink; is the time there?", style, lit)
			}
			if got := r.dst.RGBAAt(2, 2); got != (color.RGBA{0, 0, 0, 255}) {
				t.Errorf("%q: the corner is %v, not black", style, got)
			}
		}
	}
}

// What has to be seen is shown in the night clock's place: a reminder, an announcement, a call's
// face, a camera, the menu a touch opened.
func TestTheSpotNightClockGivesWay(t *testing.T) {
	base := roundScene{now: time.Now(), phase: "idle", redClock: true}
	if !base.nightClockFace() {
		t.Fatal("the night clock is not drawn on an idle face")
	}
	for name, s := range map[string]func(roundScene) roundScene{
		"turn":         func(s roundScene) roundScene { s.phase = "listening"; return s },
		"reminder":     func(s roundScene) roundScene { s.showReminder = true; return s },
		"announcement": func(s roundScene) roundScene { s.showAnnouncement = true; return s },
		"notification": func(s roundScene) roundScene { s.showNotification = true; return s },
		"camera":       func(s roundScene) roundScene { s.showCamera = true; return s },
		"menu":         func(s roundScene) roundScene { s.menuOpen = true; return s },
		"settings":     func(s roundScene) roundScene { s.sheetOpen = true; return s },
		"setup":        func(s roundScene) roundScene { s.setupAsking = true; return s },
	} {
		if s(base).nightClockFace() {
			t.Errorf("the night clock is drawn over the %s", name)
		}
	}
}

// spotDarkDisplay is spotNightDisplay set to Screen off.
func spotDarkDisplay(t *testing.T, night bool) *Display {
	t.Helper()
	d := spotNightDisplay(t, night)
	d.setAtNight(2)
	if atNightOptions[atNightIndex()] != "Screen off" || !nightDarkOn() || nightClockOn() {
		t.Fatalf("Screen at night says %q", atNightOptions[atNightIndex()])
	}
	return d
}

func (d *Display) lit() (on, dark bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.on, d.nightDark
}

// Set to Screen off, the night puts an idle face out, backlight and all; a tap lights it at the night's
// dimmed brightness, and the face stays lit while it is being used.
func TestTheSpotScreenOffAtNight(t *testing.T) {
	d := spotDarkDisplay(t, true)
	if !d.night(time.Now(), true, voice.State{Phase: "idle"}) {
		t.Fatal("the night left an idle face lit")
	}
	d.mu.Lock()
	level := d.level
	d.mu.Unlock()
	if on, dark := d.lit(); on || !dark || level != 0 {
		t.Fatalf("on=%v dark=%v backlight=%v, want the face out by the night", on, dark, level)
	}

	d.gesture(touch.Gesture{Kind: touch.Tap, X: center, Y: center})
	d.mu.Lock()
	level, open := d.level, d.menuOpen
	d.mu.Unlock()
	if on, _ := d.lit(); !on || open {
		t.Fatalf("after a tap: on=%v menu=%v, want the face lit and nothing else", on, open)
	}
	if want := backlightMin + (screen.BacklightMax-backlightMin)*nightCeiling/100.0; level != want {
		t.Errorf("lit, the backlight is %v, want the night's dimmed %v", level, want)
	}
	if d.night(time.Now(), true, voice.State{Phase: "idle"}) {
		t.Error("the face went out again while it was being used")
	}

	// Left alone again, it goes out again.
	d.touchedAt = time.Now().Add(-time.Hour)
	if !d.night(time.Now(), true, voice.State{Phase: "idle"}) {
		t.Error("the face stayed lit once nobody was using it")
	}
}

// A long press on a face the night put out lights it and opens the ring menu.
func TestALongPressOnTheSpotDarkAtNightOpensTheMenu(t *testing.T) {
	d := spotDarkDisplay(t, true)
	d.night(time.Now(), true, voice.State{Phase: "idle"})
	d.gesture(touch.Gesture{Kind: touch.Hold, X: center, Y: 40})
	d.mu.Lock()
	on, open, mode := d.on, d.menuOpen, d.menuMode
	d.closeMenu()
	d.mu.Unlock()
	if !on || !open || mode != modeMain {
		t.Errorf("after a long press: on=%v menu=%v mode=%v, want the face lit and the menu up", on, open, mode)
	}
}

// The night's end lights the face the night put out, but not one somebody put to sleep; choosing
// something other than Screen off in the night lights it too.
func TestTheSpotDarkNightEnds(t *testing.T) {
	d := spotDarkDisplay(t, false)
	d.on, d.nightDark = false, true
	if !d.night(time.Now(), false, voice.State{Phase: "idle"}) {
		t.Fatal("the end of the night left the face out")
	}
	if on, dark := d.lit(); !on || dark {
		t.Errorf("on=%v dark=%v after the night", on, dark)
	}

	d = spotDarkDisplay(t, false)
	d.on, d.nightDark = false, true
	d.apply(false, 50, true) // put to sleep by hand
	if d.night(time.Now(), false, voice.State{Phase: "idle"}) {
		t.Error("the end of the night lit a face somebody put to sleep")
	}

	d = spotDarkDisplay(t, true)
	d.night(time.Now(), true, voice.State{Phase: "idle"})
	d.setAtNight(0)
	if !d.night(time.Now(), false, voice.State{Phase: "idle"}) {
		t.Fatal("choosing Dim in the night left the face out")
	}
	if on, _ := d.lit(); !on {
		t.Error("the face is still out")
	}
}
