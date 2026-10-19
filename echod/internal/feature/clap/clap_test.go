package clap

import (
	"context"
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"
)

// frames is a script as the microphones hand it over: 320 samples a frame, two blocks each.
func frames(script string) <-chan []int16 {
	r := rand.New(rand.NewPCG(3, 4))
	ch := make(chan []int16, len(script)+200)
	var pending []int16
	push := func(b []int16) {
		pending = append(pending, b...)
		if len(pending) >= 2*block {
			ch <- pending[:2*block]
			pending = append([]int16(nil), pending[2*block:]...)
		}
	}
	for range 100 {
		push(room(r))
	}
	for _, c := range script {
		if c == 'x' {
			push(loud(r, 6000))
		} else {
			push(room(r))
		}
	}
	close(ch)
	return ch
}

func listenTo(t *testing.T, script string, busy bool) int32 {
	t.Helper()
	c := build()
	c.enabled = true
	var heard atomic.Int32
	c.heard = func() { heard.Add(1) }
	c.busy = func() bool { return busy }
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.listen(ctx, frames(script))
	return heard.Load()
}

func TestTheMicrophonesHearADoubleClap(t *testing.T) {
	if n := listenTo(t, "xx"+repeat(".", 30)+"xx"+repeat(".", 60), false); n != 1 {
		t.Errorf("heard %d double claps", n)
	}
}

func TestNothingIsHeardWhileTheDeviceIsBusy(t *testing.T) {
	if n := listenTo(t, "xx"+repeat(".", 30)+"xx"+repeat(".", 60), true); n != 0 {
		t.Errorf("heard %d double claps over the device's own sound", n)
	}
}

// A sound the speaker plays without a claim (a volume beep, the doorbell) is in the loopback, and holds the
// listening off as busy does.
func TestNothingIsHeardWhileTheLoopbackSounds(t *testing.T) {
	c := build()
	c.enabled = true
	var heard atomic.Int32
	c.heard = func() { heard.Add(1) }
	c.busy = func() bool { return false }
	c.played.Store(time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.listen(ctx, frames("xx"+repeat(".", 30)+"xx"+repeat(".", 60)))
	if n := heard.Load(); n != 0 {
		t.Errorf("heard %d double claps over a beep", n)
	}
}

func TestSounding(t *testing.T) {
	quiet := [][]int16{{1, -2, 3, 0}, {}}
	beep := [][]int16{{0, 0, 0, 0}, {2000, -2000, 2000, -2000}}
	if sounding(quiet) {
		t.Error("a quiet loopback taken for a sound")
	}
	if !sounding(beep) {
		t.Error("a beep in the loopback not noticed")
	}
}
