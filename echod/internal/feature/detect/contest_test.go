package detect

import (
	"testing"
	"time"
)

// contestRig is a contest with the clock and the timers in the test's hands.
type contestRig struct {
	c      *contest
	clock  time.Time
	timers []func()
	opened []bool
}

func newContestRig() *contestRig {
	r := &contestRig{clock: time.Unix(1_000_000, 0)}
	r.c = &contest{
		now:   func() time.Time { return r.clock },
		after: func(_ time.Duration, f func()) { r.timers = append(r.timers, f) },
	}
	return r
}

func (r *contestRig) wake(margin float64, hold bool) {
	if gate, ok := r.c.wakeHeard(margin, hold); ok {
		gate(func() { r.opened = append(r.opened, hold) })
	}
}

func (r *contestRig) fire() {
	due := r.timers
	r.timers = nil
	for _, f := range due {
		f()
	}
}

// With no command word listening, a wake word opens its turn at once, as it always did.
func TestContestWakeWordAloneOpensAtOnce(t *testing.T) {
	r := newContestRig()
	r.wake(0.01, false)
	if len(r.opened) != 1 || r.opened[0] {
		t.Fatalf("opened %v, want one turn at once", r.opened)
	}
}

// A wake word waits for a command word while one listens, and opens after the window if none comes.
func TestContestWakeWordWaitsAndOpens(t *testing.T) {
	r := newContestRig()
	r.wake(0.3, true)
	if len(r.opened) != 0 {
		t.Fatal("the turn opened before the window")
	}
	r.fire()
	if len(r.opened) != 1 || !r.opened[0] {
		t.Fatalf("opened %v, want one turn after the window", r.opened)
	}
}

// Margin over its own cutoff decides: the wake word at 0.78 over a 0.77 cutoff loses to a command word
// that crossed by more, and no turn opens.
func TestContestCommandWordWithTheBiggerMarginWins(t *testing.T) {
	r := newContestRig()
	r.wake(0.01, true)
	if !r.c.commandCrossed(0.18) {
		t.Fatal("the command word was refused")
	}
	r.fire()
	if len(r.opened) != 0 {
		t.Fatalf("a turn opened for %v", r.opened)
	}
}

// The wake word that crossed by more wins, and the command word does not act.
func TestContestWakeWordWithTheBiggerMarginWins(t *testing.T) {
	r := newContestRig()
	r.wake(0.2, true)
	if r.c.commandCrossed(0.05) {
		t.Fatal("the command word acted")
	}
	r.fire()
	if len(r.opened) != 1 {
		t.Fatalf("opened %v, want the turn", r.opened)
	}
}

// A command word that acted first still beats a wake word that crosses within the window by less, and
// loses to one that crosses by more. Outside the window it counts for nothing.
func TestContestCommandWordFirst(t *testing.T) {
	r := newContestRig()
	r.c.commandCrossed(0.2)
	r.clock = r.clock.Add(200 * time.Millisecond)
	r.wake(0.01, true)
	if len(r.opened) != 0 || len(r.timers) != 0 {
		t.Fatal("the weaker wake word was not ignored")
	}
	r.wake(0.3, false)
	if len(r.opened) != 1 {
		t.Fatal("the stronger wake word did not open")
	}
	r.clock = r.clock.Add(time.Second)
	r.wake(0.01, false)
	if len(r.opened) != 2 {
		t.Fatal("an old command word held back a wake word")
	}
}

// A command word with no wake word near acts, as it did.
func TestContestCommandWordAlone(t *testing.T) {
	r := newContestRig()
	if !r.c.commandCrossed(0.1) {
		t.Fatal("the command word was refused")
	}
}
