package media

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// As quiet hours start a loud device comes down to the night volume and remembers where it was; a quiet
// one, one with no limit, or one already turned down is left alone.
func TestNightTurnsDownOnce(t *testing.T) {
	for _, c := range []struct {
		limit, step, day, to, keep int
	}{
		{limit: 8, step: 20, day: 0, to: 8, keep: 20},
		{limit: 8, step: 5, day: 0, to: 5, keep: 0},
		{limit: 8, step: 8, day: 0, to: 8, keep: 0},
		{limit: 0, step: 20, day: 0, to: 20, keep: 0},
		{limit: 8, step: 20, day: 12, to: 20, keep: 12}, // turned up overnight after it came down
	} {
		if to, keep := turnDown(c.limit, c.step, c.day); to != c.to || keep != c.keep {
			t.Errorf("turnDown(%d, %d, %d) = %d, %d; want %d, %d", c.limit, c.step, c.day, to, keep, c.to, c.keep)
		}
	}
}

// As they end it goes back up if it is still at the level night volume set, whatever the limit says
// now; a level somebody chose stays.
func TestNightTurnsBackUpUnlessChanged(t *testing.T) {
	for _, c := range []struct {
		set, step, day, to int
	}{
		{set: 8, step: 8, day: 20, to: 20},
		{set: 8, step: 14, day: 20, to: 14}, // somebody turned it up
		{set: 8, step: 3, day: 20, to: 3},   // or down
		{set: 8, step: 8, day: 0, to: 8},    // it was never turned down
	} {
		if to := turnUp(c.set, c.step, c.day); to != c.to {
			t.Errorf("turnUp(%d, %d, %d) = %d; want %d", c.set, c.step, c.day, to, c.to)
		}
	}
}

// A night volume changed while the device is turned down moves a device still at the old one, and none
// or one above the daytime level puts it straight back; a level somebody chose is left.
func TestNightVolumeChangedInTheNight(t *testing.T) {
	for _, c := range []struct {
		limit, step, day, set, to, keepDay, keepSet int
	}{
		{limit: 5, step: 8, day: 20, set: 8, to: 5, keepDay: 20, keepSet: 5},
		{limit: 12, step: 8, day: 20, set: 8, to: 12, keepDay: 20, keepSet: 12},
		{limit: 0, step: 8, day: 20, set: 8, to: 20},
		{limit: 25, step: 8, day: 20, set: 8, to: 20},
		{limit: 5, step: 14, day: 20, set: 8, to: 14, keepDay: 20, keepSet: 8}, // turned up by hand
		{limit: 5, step: 8, day: 0, set: 0, to: 8},                             // not turned down
	} {
		to, day, set := relimit(c.limit, c.step, c.day, c.set)
		if to != c.to || day != c.keepDay || set != c.keepSet {
			t.Errorf("relimit(%d, %d, %d, %d) = %d, %d, %d; want %d, %d, %d",
				c.limit, c.step, c.day, c.set, to, day, set, c.to, c.keepDay, c.keepSet)
		}
	}
}

// An alarm's volume starts from the daytime level, not the night's.
func TestDaytimeIsTheLevelBeforeTheNight(t *testing.T) {
	s := config.Speaker{Volume: 8, DayVolume: 20}
	if got := s.Daytime(); got != 20 {
		t.Errorf("turned down: daytime %d, want 20", got)
	}
	s.DayVolume = 0
	if got := s.Daytime(); got != 8 {
		t.Errorf("not turned down: daytime %d, want 8", got)
	}
}

// window is quiet hours around now, or well away from it.
func window(around bool) string {
	now := time.Now()
	if !around {
		now = now.Add(12 * time.Hour)
	}
	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	return fmt.Sprintf("%02d:%02d-%02d:%02d", from.Hour(), from.Minute(), to.Hour(), to.Minute())
}

// Through a whole night on a muted device: turned down, the limit moved in the night, and back up in
// the morning, all saved, and the speaker never unmuted along the way.
func TestNightVolumeThroughANight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	p := &Player{mp: &esphome.MediaPlayer{}, night: newNight()}
	p.muted.Store(true)
	p.step.Store(20)
	if err := config.Set().Speaker().QuietHours(window(true)); err != nil {
		t.Fatal(err)
	}

	p.SetNightVolume(8) // inside the hours: applies at once
	if got, c := p.Volume(), config.Get().Speaker; got != 8 || c.DayVolume != 20 || c.NightSet != 8 || c.Volume != 8 {
		t.Fatalf("turned down to %d, saved %+v", got, c)
	}

	p.SetNightVolume(5)
	if got, c := p.Volume(), config.Get().Speaker; got != 5 || c.DayVolume != 20 || c.NightSet != 5 {
		t.Fatalf("moved with the setting to %d, saved %+v", got, c)
	}

	p.nightStart(false) // a restart in the night: already turned down, so nothing
	if got := p.Volume(); got != 5 {
		t.Fatalf("turned down again to %d", got)
	}

	if err := config.Set().Speaker().QuietHours(window(false)); err != nil {
		t.Fatal(err)
	}
	p.nightEnd()
	if got, c := p.Volume(), config.Get().Speaker; got != 20 || c.DayVolume != 0 || c.NightSet != 0 || c.Volume != 20 {
		t.Fatalf("morning: %d, saved %+v", got, c)
	}
	if !p.muted.Load() {
		t.Error("night volume unmuted the speaker")
	}
}

// Turning the night volume off in the night puts the device straight back.
func TestNightVolumeOffInTheNight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	p := &Player{mp: &esphome.MediaPlayer{}, night: newNight()}
	p.muted.Store(true)
	p.step.Store(20)
	if err := config.Set().Speaker().QuietHours(window(true)); err != nil {
		t.Fatal(err)
	}
	p.SetNightVolume(8)
	p.SetNightVolume(0)
	if got, c := p.Volume(), config.Get().Speaker; got != 20 || c.DayVolume != 0 || c.NightSet != 0 {
		t.Fatalf("off in the night: %d, saved %+v", got, c)
	}
}

// The small hours of a window across midnight belong to the night that began the evening before.
func TestNightOf(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 2, h, m, 0, 0, time.Local) }
	for _, c := range []struct {
		window string
		now    time.Time
		want   string
	}{
		{"22-7", at(23, 0), "2026-10-02"},
		{"22-7", at(2, 0), "2026-10-01"},
		{"22-7", at(12, 0), "2026-10-02"}, // outside the hours: the coming night
		{"1-6", at(3, 0), "2026-10-02"},
		{"", at(3, 0), ""},
	} {
		if got := nightOf(c.window, c.now); got != c.want {
			t.Errorf("nightOf(%q, %s) = %q, want %q", c.window, c.now.Format("15:04"), got, c.want)
		}
	}
}

// Turned up by hand in the night, a device that then restarts is not turned down again: the night was
// already looked at. One that starts inside a night it has not looked at is turned down.
func TestARestartInTheNightKeepsATurnUp(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	p := &Player{mp: &esphome.MediaPlayer{}, night: newNight()}
	p.muted.Store(true)
	p.step.Store(5)
	if err := config.Set().Speaker().QuietHours(window(true)); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Speaker().NightVolume(8); err != nil {
		t.Fatal(err)
	}
	p.nightStart(true) // the hours begin with the device under the limit: nothing to do
	if got := p.Volume(); got != 5 {
		t.Fatalf("turned to %d", got)
	}
	p.step.Store(25)    // turned up at two in the morning
	p.nightStart(false) // and then a restart
	if got := p.Volume(); got != 25 {
		t.Fatalf("a restart turned it down to %d", got)
	}

	if err := config.Set().Speaker().NightOf("2000-01-01"); err != nil { // a night long gone
		t.Fatal(err)
	}
	p.nightStart(false)
	if got, c := p.Volume(), config.Get().Speaker; got != 8 || c.DayVolume != 25 {
		t.Fatalf("starting inside a new night: %d, saved %+v", got, c)
	}
}

// Run looks at the hours once the clock has had time to settle, and turns a device down then.
func TestRunTurnsDownAtStart(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	was := nightSettle
	nightSettle = 10 * time.Millisecond
	t.Cleanup(func() { nightSettle = was })
	p := &Player{mp: &esphome.MediaPlayer{}, night: newNight()}
	p.muted.Store(true)
	p.step.Store(20)
	if err := config.Set().Speaker().QuietHours(window(true)); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Speaker().NightVolume(8); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for p.Volume() != 8 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := p.Volume(); got != 8 {
		t.Fatalf("at start: %d, want 8", got)
	}
}

// Turned down by a build that did not save the level it set, the morning still puts it back.
func TestMorningAfterAnOlderBuild(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	p := &Player{mp: &esphome.MediaPlayer{}, night: newNight()}
	p.muted.Store(true)
	p.step.Store(6)
	if err := config.Set().Speaker().NightVolume(6); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Speaker().Night(14, 0); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Speaker().QuietHours(window(false)); err != nil {
		t.Fatal(err)
	}
	p.nightEnd()
	if got := p.Volume(); got != 14 {
		t.Fatalf("morning: %d, want 14", got)
	}
}
