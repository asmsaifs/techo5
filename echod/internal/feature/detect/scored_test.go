package detect

import (
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/wake"
)

// A wake reports the highest score it reached once the hold is over, not the score that first crossed
// the cutoff: the peak is what says how far over the threshold it was.
func TestAWakeReportsItsPeak(t *testing.T) {
	fakes(t)
	e := New(2, nil)
	e.Threshold = func(int) float64 { return 0.9 }
	if err := e.Use(0, model("hey_jarvis", wake.KindMicroWakeWord)); err != nil {
		t.Fatalf("Use(0): %v", err)
	}

	type report struct {
		slot         int
		peak, cutoff float64
	}
	got := make(chan report, 2)
	e.OnScored = func(slot int, peak, cutoff float64) { got <- report{slot, peak, cutoff} }

	at := time.Now()
	s := &e.slots[0]
	e.judge(0, s, 0.92, at, nil)
	e.judge(0, s, 0.97, at.Add(100*time.Millisecond), nil)
	e.judge(0, s, 0.95, at.Add(200*time.Millisecond), nil)
	select {
	case r := <-got:
		t.Fatalf("reported %+v before the hold was over", r)
	case <-time.After(50 * time.Millisecond):
	}

	e.judge(0, s, 0.4, at.Add(Hold+time.Millisecond), nil)
	select {
	case r := <-got:
		if r != (report{0, 0.97, 0.9}) {
			t.Errorf("reported %+v, want slot 0 at its peak 0.97 against 0.9", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no score was reported after the hold")
	}
}
