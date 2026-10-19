package timer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// stateOnce points the saves at a file of the tests' own, once: a ring started by one test is still
// reading the settings as the next begins, so the file is not swapped between them.
var stateOnce sync.Once

// useTestState points the settings at the tests' own file, the first time it is called; every test in
// this package that saves or reads settings goes through it.
func useTestState(t *testing.T) {
	t.Helper()
	stateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "techo5-timer-test")
		if err != nil {
			t.Fatal(err)
		}
		config.Use(filepath.Join(dir, "state.json"))
	})
}

// controlled is a set of timers whose saves go to the tests' own file.
func controlled(t *testing.T) *Timers {
	t.Helper()
	useTestState(t)
	ts := build()
	t.Cleanup(func() { ts.Stop() })
	return ts
}

func TestPauseHoldsAndResumeCarriesOn(t *testing.T) {
	ts := controlled(t)
	id := ts.Start("Pasta", 10*time.Minute)

	if err := ts.Pause(id); err != nil {
		t.Fatal(err)
	}
	c := ts.held[id]
	if c.active {
		t.Fatal("the timer still runs after a pause")
	}
	held := c.remaining(time.Now())
	if got := c.remaining(time.Now().Add(time.Hour)); got != held {
		t.Errorf("a paused timer moved: %v then %v", held, got)
	}
	if err := ts.Pause(id); err != nil {
		t.Errorf("pausing a paused timer: %v", err)
	}

	if err := ts.Resume(id); err != nil {
		t.Fatal(err)
	}
	if !c.active {
		t.Fatal("the timer is not running after a resume")
	}
	if got := c.remaining(time.Now()); got > held || got < held-time.Second {
		t.Errorf("resumed with %v left, paused with %v", got, held)
	}
}

func TestAdjustAddsAndRemovesTime(t *testing.T) {
	ts := controlled(t)
	id := ts.Start("Eggs", 7*time.Minute)

	if err := ts.Adjust(id, 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	c := ts.held[id]
	if left := c.remaining(time.Now()); left <= 9*time.Minute || left > 10*time.Minute {
		t.Errorf("after adding 3 minutes to 7: %v left", left)
	}
	if c.total != 10*time.Minute {
		t.Errorf("the total is %v, want 10m so the countdown is drawn against it", c.total)
	}

	if err := ts.Adjust(id, -5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if left := c.remaining(time.Now()); left <= 4*time.Minute || left > 5*time.Minute {
		t.Errorf("after taking 5 minutes off: %v left", left)
	}

	if err := ts.Adjust(id, -time.Hour); !errors.Is(err, errTooShort) {
		t.Errorf("taking off more than is left: %v, want refused", err)
	}
	if ts.held[id] == nil {
		t.Error("a refused change ended the timer")
	}
}

func TestATimerCannotGrowPastTheLongest(t *testing.T) {
	ts := controlled(t)
	id := ts.Start("Brisket", 23*time.Hour)
	if err := ts.Adjust(id, 2*time.Hour); !errors.Is(err, errTooLong) {
		t.Errorf("adding two hours to 23: %v, want refused", err)
	}
	if err := ts.Adjust(id, 30*time.Minute); err != nil {
		t.Errorf("adding half an hour to 23 hours: %v", err)
	}
}

func TestAdjustWorksOnAPausedTimer(t *testing.T) {
	ts := controlled(t)
	id := ts.Start("Bread", 20*time.Minute)
	ts.Pause(id)
	if err := ts.Adjust(id, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	c := ts.held[id]
	if c.active {
		t.Error("adding time started a paused timer")
	}
	if left := c.remaining(time.Now()); left <= 24*time.Minute || left > 25*time.Minute {
		t.Errorf("paused at about 20 minutes, added 5: %v left", left)
	}
}

// A timer at zero that the countdown has not ended yet is left to ring. Paused there, it would sit at
// 0:00 for ever and ring on a resume.
func TestATimerThatRanOutIsNotChanged(t *testing.T) {
	ts := controlled(t)
	id := ts.Start("Tea", time.Minute)
	ts.mu.Lock()
	ts.held[id].at = time.Now().Add(-2 * time.Minute)
	ts.mu.Unlock()

	for name, err := range map[string]error{
		"pause":  ts.Pause(id),
		"adjust": ts.Adjust(id, time.Minute),
	} {
		if !errors.Is(err, errRunOut) {
			t.Errorf("%s at zero: %v, want refused as run out", name, err)
		}
	}
	if !ts.held[id].active {
		t.Error("a timer at zero was paused, so it will never ring")
	}
	ts.ripe(time.Now())
	if !ts.Ringing() {
		t.Error("the timer did not ring after the refused changes")
	}
}

func TestHomeAssistantsOwnTimersAreRefused(t *testing.T) {
	ts := controlled(t)
	ts.Event(started("kettle", 180))
	for name, err := range map[string]error{
		"pause":  ts.Pause("kettle"),
		"resume": ts.Resume("kettle"),
		"adjust": ts.Adjust("kettle", time.Minute),
	} {
		if err == nil {
			t.Errorf("%s on Home Assistant's timer should be refused", name)
		}
	}
	if err := ts.Pause("local:nope"); !errors.Is(err, errNoTimer) {
		t.Errorf("pausing a timer that does not exist: %v, want no such timer", err)
	}
}

func TestPauseAllTakesOnlyTheDevicesOwn(t *testing.T) {
	ts := controlled(t)
	ts.Event(started("kettle", 180))
	a := ts.Start("A", time.Minute)
	b := ts.Start("B", 2*time.Minute)

	if err := ts.eachFromHA("all", ts.Pause); err != nil {
		t.Fatal(err)
	}
	if ts.held[a].active || ts.held[b].active {
		t.Error("pause all left one of the device's own timers running")
	}
	if !ts.held["kettle"].active {
		t.Error("pause all paused Home Assistant's timer")
	}
}

// One timer going away between the list and the pause does not stop "all" at it.
func TestAllCarriesOnPastATimerThatWentAway(t *testing.T) {
	ts := controlled(t)
	ids := []string{ts.Start("A", time.Minute), ts.Start("B", 2*time.Minute), ts.Start("C", 3*time.Minute)}

	first := true
	pause := func(id string) error {
		if first {
			first = false
			// The other two are listed already; one of them is canceled before it is reached.
			for _, other := range ids {
				if other != id {
					ts.Cancel(other)
					break
				}
			}
		}
		return ts.Pause(id)
	}
	if err := ts.eachFromHA("all", pause); err != nil {
		t.Fatalf("pause all with a timer canceled midway: %v", err)
	}
	for id, c := range ts.held {
		if c.active {
			t.Errorf("%s was left running", id)
		}
	}
	if len(ts.held) != 2 {
		t.Errorf("holding %d timers, want the two that were not canceled", len(ts.held))
	}
}

// A paused timer comes back paused after a restart, with what it had left.
func TestAPausedTimerComesBackPaused(t *testing.T) {
	ts := controlled(t)
	id := ts.Start("Bread", 20*time.Minute)
	if err := ts.Pause(id); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ts.mu.Lock()
	saved := localList(ts.held, now)
	left := ts.held[id].left
	ts.mu.Unlock()

	again := build()
	again.restore(saved, now.Add(3*time.Hour))
	c := again.held[id]
	if c == nil {
		t.Fatal("the paused timer did not come back")
	}
	if c.active {
		t.Error("the paused timer came back running")
	}
	if c.left != left {
		t.Errorf("came back with %v left, paused with %v", c.left, left)
	}
}

// Pausing and resuming while the countdown runs is safe, and never leaves a timer paused at zero.
func TestPauseAndResumeAgainstTheCountdown(t *testing.T) {
	ts := controlled(t)
	id := ts.Start("Quick", 20*time.Millisecond)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				ts.ripe(time.Now())
				time.Sleep(time.Millisecond)
			}
		}
	}()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		ts.Pause(id)
		ts.Resume(id)
		ts.mu.Lock()
		c := ts.held[id]
		gone := c == nil
		if c != nil && !c.active && c.left <= 0 {
			t.Error("a timer was paused at zero")
		}
		ts.mu.Unlock()
		if gone {
			break
		}
	}
	close(stop)
	wg.Wait()
	if _, held := ts.held[id]; held {
		t.Error("the timer never ran out with pauses and resumes around it")
	}
}

func TestTheActionsReadTheirArguments(t *testing.T) {
	ts := controlled(t)
	id := ts.Start("Pasta", 10*time.Minute)

	if err := ts.control("timer_pause", "  "+id+"  ", ""); err != nil {
		t.Errorf("an id with spaces around it: %v", err)
	}
	if ts.held[id].active {
		t.Error("timer_pause did not pause")
	}
	if err := ts.control("timer_resume", id, ""); err != nil || !ts.held[id].active {
		t.Errorf("timer_resume: %v, running %v", err, ts.held[id].active)
	}
	if err := ts.control("timer_add", id, "5 minutes"); err != nil {
		t.Errorf("timer_add 5 minutes: %v", err)
	}
	if left := ts.held[id].remaining(time.Now()); left <= 14*time.Minute {
		t.Errorf("after adding 5 minutes to 10: %v left", left)
	}
	if err := ts.control("timer_remove", id, "00:02:00"); err != nil {
		t.Errorf("timer_remove 00:02:00: %v", err)
	}
	if left := ts.held[id].remaining(time.Now()); left > 13*time.Minute {
		t.Errorf("after taking 2 minutes off 15: %v left", left)
	}

	for _, c := range []struct{ action, id, duration, says string }{
		{"timer_add", id, "soon", "soon"},
		{"timer_add", id, "", ""},
		{"timer_pause", "", "", "which timer"},
		{"timer_add", "all", "5 minutes", "one timer"},
		{"timer_remove", "all", "5 minutes", "one timer"},
	} {
		err := ts.control(c.action, c.id, c.duration)
		if err == nil {
			t.Errorf("%s id=%q duration=%q: no error", c.action, c.id, c.duration)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s id=%q duration=%q: %q, want it to say %q", c.action, c.id, c.duration, err, c.says)
		}
	}
}

// The actions run with nothing filled in say what is missing rather than doing anything.
func TestTheActionsWithNoArguments(t *testing.T) {
	ts := controlled(t)
	want := map[string]bool{"timer_pause": false, "timer_resume": false, "timer_add": false, "timer_remove": false}
	for _, a := range ts.Actions() {
		if _, ok := want[a.Name]; !ok {
			continue
		}
		want[a.Name] = true
		if _, err := a.Run(esphome.Call{}); err == nil {
			t.Errorf("%s with no id ran without complaint", a.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s is not offered", name)
		}
	}
}

func TestAPausedTimerIsDescribedWithWhatItHasLeft(t *testing.T) {
	ts := controlled(t)
	if err := config.Set().Screen().Clock24(true); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 18, 38, 0, 0, time.Local)
	ts.mu.Lock()
	ts.held["local:a"] = &timer{name: "Pasta", total: 10 * time.Minute, left: 10 * time.Minute, at: now, active: true, local: true}
	ts.held["local:b"] = &timer{name: "Bread", total: time.Hour, left: 4*time.Minute + 5*time.Second, at: now, local: true}
	ts.mu.Unlock()

	if got, want := ts.describe(now), "Pasta at 18:48, Bread paused (4:05 left)"; got != want {
		t.Errorf("describing %q, want %q", got, want)
	}
}

// Telling Home Assistant what is counting down reads the timers while a pause or a change from it is
// writing them.
func TestDescribingWhilePausingAndChanging(t *testing.T) {
	ts := controlled(t)
	id := ts.Start("Stew", time.Hour)
	ts.Event(started("kettle", 600))

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				ts.publish()
				ts.describe(time.Now())
			}
		}
	}()
	for range 50 {
		ts.Pause(id)
		ts.Adjust(id, time.Second)
		ts.Resume(id)
		ts.Adjust(id, -time.Second)
	}
	close(stop)
	wg.Wait()
}
