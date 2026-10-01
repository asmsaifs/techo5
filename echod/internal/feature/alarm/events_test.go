package alarm

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/ring"
)

// events records what an alarm puts on Home Assistant's bus.
func events(t *testing.T) func() []map[string]string {
	t.Helper()
	var mu sync.Mutex
	var got []map[string]string
	stop := component.Fire.Listen(func(e component.Event) {
		if e.Name == Event {
			mu.Lock()
			got = append(got, e.Data)
			mu.Unlock()
		}
	})
	t.Cleanup(stop)
	return func() []map[string]string {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]string(nil), got...)
	}
}

func endRing(t *testing.T, a *Alarms) {
	t.Cleanup(func() {
		a.Stop()
		ring.End()
		for end := time.Now().Add(2 * time.Second); ring.IsSounding() && time.Now().Before(end); {
			time.Sleep(time.Millisecond)
		}
	})
}

// waitFor waits for the nth event to arrive: the stop is the bell's, from its own goroutine.
func waitFor(t *testing.T, got func() []map[string]string, n int) []map[string]string {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if e := got(); len(e) >= n {
			return e
		}
	}
	t.Fatalf("%d events arrived, wanted %d: %v", len(got()), n, got())
	return nil
}

// An alarm that rings says so, and says so again when it is stopped, with its id and label.
func TestAnAlarmTellsHomeAssistant(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	got := events(t)
	a := build()
	endRing(t, a)
	due := time.Date(2026, 10, 1, 6, 30, 0, 0, time.Local)
	a.fire(source{key: "wake", label: "Wake up"}, due)
	a.Stop()
	e := waitFor(t, got, 2)
	if e[0]["event"] != "ringing" || e[1]["event"] != "stopped" {
		t.Fatalf("events %v", e)
	}
	if e[0]["id"] != "wake" || e[0]["label"] != "Wake up" || e[0]["due"] != due.Format(time.RFC3339) {
		t.Errorf("ringing carried %v", e[0])
	}
}

// A snoozed alarm says snoozed, not stopped, and its snooze rings under the same id.
func TestASnoozeTellsHomeAssistant(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	got := events(t)
	a := build()
	endRing(t, a)
	a.fire(source{key: "wake", label: "Wake up"}, time.Now())
	if !a.Snooze() {
		t.Fatal("nothing to snooze")
	}
	time.Sleep(200 * time.Millisecond) // the bell winding the ring down
	a.fire(source{key: "snooze:wake", label: "Wake up"}, time.Now())
	e := waitFor(t, got, 3)
	if e[1]["event"] != "snoozed" || e[2]["event"] != "ringing" || e[2]["id"] != "wake" {
		t.Errorf("events %v", e)
	}
	for _, x := range e {
		if x["event"] == "stopped" {
			t.Errorf("a snooze was told as a stop: %v", e)
		}
	}
}

// A silent alarm makes no sound and is only the event.
func TestASilentAlarmIsOnlyTheEvent(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	got := events(t)
	a := build()
	endRing(t, a)
	a.fire(source{key: "lights", label: "Blinds", silent: true}, time.Now())
	if a.Ringing() || ring.IsSounding() {
		t.Error("a silent alarm rang")
	}
	e := waitFor(t, got, 1)
	if len(e) != 1 || e[0]["event"] != "silent" || e[0]["label"] != "Blinds" {
		t.Errorf("events %v", e)
	}
}

// alarm_set_silent and alarm_set turn the same alarm one way and the other.
func TestSettingSilentFromHomeAssistant(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()
	al, err := a.setFromHA(6, 30, config.DaysOnce, "Blinds", "", true)
	if err != nil || !al.Silent {
		t.Fatalf("%+v %v", al, err)
	}
	al, err = a.setFromHA(6, 30, config.DaysOnce, "Blinds", "", false)
	if err != nil || al.Silent || len(config.Get().Alarms.List) != 1 {
		t.Errorf("%+v %v, %d alarms", al, err, len(config.Get().Alarms.List))
	}
}

// An alarm that goes off while another rings joins its ring, and is told stopped with it; a silent
// alarm brings up no wake light.
func TestAFoldedAlarmIsToldStoppedToo(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	got := events(t)
	a := build()
	endRing(t, a)
	a.fire(source{key: "first", label: "One"}, time.Now())
	a.fire(source{key: "second", label: "Two"}, time.Now())
	a.Stop()
	e := waitFor(t, got, 4)
	stopped := map[string]bool{}
	for _, x := range e {
		if x["event"] == "stopped" {
			stopped[x["id"]] = true
		}
	}
	if !stopped["first"] || !stopped["second"] {
		t.Errorf("events %v", e)
	}
	if n := (config.Alarms{SunriseMinutes: 20}).SunriseFor(config.Alarm{Silent: true}); n != 0 {
		t.Errorf("a silent alarm brings %d minutes of light", n)
	}
}
