package alarm

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/hastate"
)

// Turning an alarm off drops its snooze, and only its own (#159): a snooze pressed by mistake is undone
// the way a phone's is. A change that leaves it on keeps the snooze, and so does a save of one already
// off, such as a one-off that turned itself off as it rang.
func TestTurningAnAlarmOffDropsItsSnooze(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()
	later := time.Now().Add(9 * time.Minute).Truncate(time.Second)
	wake := config.Alarm{ID: "wake", Hour: 6, Minute: 30, Days: 0b0111110, Label: "Wake up", On: true}
	once := config.Alarm{ID: "once", Hour: 7, Minute: 0, Days: config.DaysOnce, Label: "Bread"}
	for _, al := range []config.Alarm{wake, once} {
		if err := a.Put(al); err != nil {
			t.Fatal(err)
		}
	}
	snoozes := func(keys ...string) {
		a.mu.Lock()
		a.snoozed = nil
		for _, k := range keys {
			a.snoozed = append(a.snoozed, source{key: "snooze:" + k, once: later})
		}
		a.mu.Unlock()
		a.saveSnoozes()
	}
	left := func() []string {
		a.mu.Lock()
		defer a.mu.Unlock()
		var out []string
		for _, s := range a.snoozed {
			out = append(out, s.key)
		}
		if saved := config.Get().Alarms.Snoozed; len(saved) != len(out) {
			t.Errorf("saved %d snoozes, holding %d", len(saved), len(out))
		}
		return out
	}

	snoozes("wake", "once")
	wake.Label = "Up"
	if err := a.Put(wake); err != nil {
		t.Fatal(err)
	}
	if got := left(); len(got) != 2 {
		t.Errorf("a change that left it on dropped a snooze: %v", got)
	}
	if err := a.Put(once); err != nil { // already off
		t.Fatal(err)
	}
	if got := left(); len(got) != 2 {
		t.Errorf("saving an alarm already off dropped a snooze: %v", got)
	}

	wake.On = false
	if err := a.Put(wake); err != nil {
		t.Fatal(err)
	}
	if got := left(); len(got) != 1 || got[0] != "snooze:once" {
		t.Errorf("after turning wake off: %v, want only the other alarm's snooze", got)
	}
	wake.On = true
	if err := a.Put(wake); err != nil {
		t.Fatal(err)
	}
	if got := left(); len(got) != 1 {
		t.Errorf("turning it back on brought a snooze back: %v", got)
	}

	if err := a.Delete("once"); err != nil {
		t.Fatal(err)
	}
	if got := left(); len(got) != 0 {
		t.Errorf("a deleted alarm kept its snooze: %v", got)
	}
}

// A followed helper's arm switch turned off in Home Assistant drops its snooze, as turning off an
// alarm on the device does, and only that helper's. Its first report after a start counts: the
// switch was turned off while the device was down.
func TestAHelperTurnedOffDropsItsSnooze(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Alarms().Follow([]string{"input_datetime.wake=input_boolean.wake_on", "input_datetime.gym"}); err != nil {
		t.Fatal(err)
	}
	a := build()
	later := time.Now().Add(9 * time.Minute).Truncate(time.Second)
	a.mu.Lock()
	a.snoozed = []source{{key: "snooze:input_datetime.wake", once: later}, {key: "snooze:input_datetime.gym", once: later}}
	a.mu.Unlock()
	a.saveSnoozes()
	left := func() []source {
		a.mu.Lock()
		defer a.mu.Unlock()
		return append([]source(nil), a.snoozed...)
	}
	a.helperChanged(hastate.Update{Key: hastate.Key{Entity: "input_boolean.wake_on"}, Value: "on", First: true})
	a.helperChanged(hastate.Update{Key: hastate.Key{Entity: "input_boolean.wake_on"}, Value: "unavailable"})
	time.Sleep(50 * time.Millisecond)
	if n := len(left()); n != 2 {
		t.Fatalf("a switch that did not go off dropped a snooze: %d left", n)
	}
	a.helperChanged(hastate.Update{Key: hastate.Key{Entity: "input_boolean.wake_on"}, Value: "off", First: true})
	// The drop runs on its own goroutine; done once the saved list has it too.
	for end := time.Now().Add(2 * time.Second); len(config.Get().Alarms.Snoozed) != 1 && time.Now().Before(end); {
		time.Sleep(time.Millisecond)
	}
	if got := left(); len(got) != 1 || got[0].key != "snooze:input_datetime.gym" {
		t.Errorf("after the switch went off: %v, want only the other helper's snooze", got)
	}
}

// Snooze on an alarm deleted or turned off while it rang stops it instead: nothing comes back.
func TestSnoozingAnAlarmThatWentAwayStopsIt(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()
	endRing(t, a)
	// No label: a label is said over the ring on a goroutine of its own, which would outlive the test.
	wake := config.Alarm{ID: "wake", Hour: 6, Minute: 30, Days: 0b0111110, On: true}
	if err := a.Put(wake); err != nil {
		t.Fatal(err)
	}
	a.fire(source{key: "wake", days: wake.Days, local: true}, time.Now())
	wake.On = false
	if err := a.Put(wake); err != nil {
		t.Fatal(err)
	}
	if !a.Snooze() {
		t.Fatal("the snooze was not taken as a stop")
	}
	a.mu.Lock()
	n := len(a.snoozed)
	a.mu.Unlock()
	if n != 0 {
		t.Errorf("a turned-off alarm was snoozed back: %d snoozes", n)
	}
	// Home Assistant's alarm_snooze_for, arriving a moment after the spoken snooze, says it stopped.
	if got := a.snoozeAnswer("snooze"); got["stopped"] != true || got["snoozed"] != false {
		t.Errorf("alarm_snooze_for answered %v, want stopped", got)
	}
}
