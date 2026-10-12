package media

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// stopOnRead is a sound whose read is silenced while it waits: the read then brings a full chunk.
type stopOnRead struct{ stop func() }

func (r stopOnRead) Read(p []byte) (int, error) {
	r.stop()
	return len(p), nil
}

// A sound silenced while a read waited does not play what that read brought: a camera's view that closed
// is not heard for one more chunk after it.
func TestASilencedSoundPlaysNothingMoreAfterItsRead(t *testing.T) {
	stop, cancel := context.WithCancel(context.Background())
	defer cancel()
	fetch, giveUp := context.WithCancel(stop)
	defer giveUp()
	var asked atomic.Int32 // whether to play it is asked only of samples about to go to the speaker
	err := pumpOver(stop, fetch, giveUp, stopOnRead{stop: cancel}, speaker.New(), func() bool {
		asked.Add(1)
		return false
	})
	if err != nil {
		t.Fatalf("a silenced sound ended with %v", err)
	}
	if n := asked.Load(); n != 0 {
		t.Errorf("what a read brought after the sound was silenced went on to the speaker (%d chunks)", n)
	}
}
