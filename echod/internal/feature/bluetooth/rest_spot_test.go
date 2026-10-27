//go:build spot

package bluetooth

import (
	"sync/atomic"
	"testing"
	"time"
)

// While it is wanted (the proxy on, the speaker playing) the scanner rests, and is always given back;
// while it is not, the scanner is left alone.
func TestTheScannerRestsWhileTheSpeakerPlays(t *testing.T) {
	var pauses, resumes atomic.Int32
	var want atomic.Bool
	pause := func() func() {
		pauses.Add(1)
		return func() { resumes.Add(1) }
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() { rest(stop, pause, 5*time.Millisecond, 15*time.Millisecond, want.Load); close(done) }()
	defer func() { close(stop); <-done }()

	time.Sleep(60 * time.Millisecond)
	if pauses.Load() != 0 {
		t.Fatalf("rested %d times when not wanted", pauses.Load())
	}
	want.Store(true)
	time.Sleep(100 * time.Millisecond)
	if pauses.Load() == 0 {
		t.Fatal("never rested while wanted")
	}
	want.Store(false)
	time.Sleep(60 * time.Millisecond)
	settled := pauses.Load()
	if resumes.Load() != settled {
		t.Errorf("%d rests, %d given back", settled, resumes.Load())
	}
	time.Sleep(60 * time.Millisecond)
	if pauses.Load() != settled {
		t.Error("rested when not wanted")
	}
}
