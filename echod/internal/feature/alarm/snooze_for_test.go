package alarm

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// ringingFor makes a look like it is ringing an alarm, as the ring loop would, and counts the silences.
func ringingFor(a *Alarms, silenced *int) {
	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", Label: "Wake up", At: time.Now()}
	a.silence = func() {
		*silenced++
		a.mu.Lock()
		a.ringing, a.silence = nil, nil
		a.mu.Unlock()
	}
	a.mu.Unlock()
}

// A snooze asked for with a length is put off for that length, held to the snooze length's limits,
// and 0 is the length set on the device.
func TestSnoozeForALength(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()

	for _, c := range []struct {
		asked, want int
	}{
		{10, 10},
		{0, config.Get().Alarms.Snooze()},
		{45, config.MaxSnoozeMinutes},
	} {
		silenced := 0
		ringingFor(a, &silenced)
		before := time.Now()
		if !a.SnoozeFor(c.asked) {
			t.Fatalf("SnoozeFor(%d) found nothing ringing", c.asked)
		}
		if silenced != 1 {
			t.Errorf("SnoozeFor(%d) silenced the ring %d times", c.asked, silenced)
		}
		a.mu.Lock()
		last, n := a.lastSnooze, len(a.snoozed)
		at := a.snoozed[n-1].once
		a.mu.Unlock()
		if last.minutes != c.want {
			t.Errorf("SnoozeFor(%d) snoozed %d minutes, want %d", c.asked, last.minutes, c.want)
		}
		if got := at.Sub(before.Truncate(time.Second)); got < time.Duration(c.want)*time.Minute-time.Second || got > time.Duration(c.want)*time.Minute+time.Second {
			t.Errorf("SnoozeFor(%d) rings again in %v, want %d minutes", c.asked, got, c.want)
		}
	}

	if a.SnoozeFor(5) {
		t.Error("SnoozeFor snoozed with nothing ringing")
	}
}

// A ring is snoozed once, however many times it is asked while the bell is still winding it down.
func TestARingIsSnoozedOnce(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()

	// As the real bell does it: silencing does not clear the ring at once, rang does, later.
	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", Label: "Wake up", At: time.Now()}
	a.silence = func() {}
	a.mu.Unlock()

	if !a.SnoozeFor(10) {
		t.Fatal("the first snooze found nothing ringing")
	}
	if a.SnoozeFor(10) {
		t.Error("a ring already snoozed was snoozed again")
	}
	if got := a.snoozeAnswer("snooze for ten minutes"); got["snoozed"] != true || got["minutes"] != 10 {
		t.Errorf("the action answered %v", got)
	}
	a.mu.Lock()
	n := len(a.snoozed)
	a.mu.Unlock()
	if n != 1 {
		t.Errorf("%d snoozes saved, want 1", n)
	}
}

// The action answers with the snooze the device made a moment before from what it heard, and says so
// when nothing was snoozed.
func TestSnoozeActionAnswers(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()

	run := a.snoozeAnswer

	if got := run("snooze for ten minutes"); got["snoozed"] != false {
		t.Errorf("with nothing ringing it answered %v", got)
	}

	// Heard on the device first: the action finds nothing ringing, and answers with that snooze.
	silenced := 0
	ringingFor(a, &silenced)
	a.SnoozeFor(10)
	if got := run("snooze for ten minutes"); got["snoozed"] != true || got["minutes"] != 10 {
		t.Errorf("after the device snoozed it, the action answered %v", got)
	}

	// Not heard on the device: the action snoozes it itself.
	ringingFor(a, &silenced)
	if got := run("snooze for fifteen minutes"); got["snoozed"] != true || got["minutes"] != 15 {
		t.Errorf("the action snoozing it answered %v", got)
	}
}
