package spectrum

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func sine(hz, rate float64, n int, amp float64) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(amp * 32767 * math.Sin(2*math.Pi*hz*float64(i)/rate))
	}
	return out
}

// A loud tone lights the band it is in and leaves the far bands dark; silence brings every bar down.
func TestToneLightsItsBand(t *testing.T) {
	a := New(16000, 512)
	now := time.Now()
	a.Bands(24, now) // start watching
	var level []float64
	for i := range 10 {
		a.Push(sine(1000, 16000, 512, 0.5))
		level, _ = a.Bands(24, now.Add(time.Duration(i+1)*50*time.Millisecond))
	}
	loudest := 0
	for b := range level {
		if level[b] > level[loudest] {
			loudest = b
		}
	}
	if level[loudest] < 0.8 {
		t.Fatalf("a half-scale tone reads %.2f at its loudest", level[loudest])
	}
	// 1 kHz sits a little over halfway along a log scale from 90 Hz to 7 kHz.
	if loudest < 12 || loudest > 16 {
		t.Errorf("1 kHz lit band %d of 24", loudest)
	}
	if level[0] > 0.2 || level[23] > 0.2 {
		t.Errorf("far bands lit: %.2f and %.2f", level[0], level[23])
	}

	later := now.Add(time.Second)
	for i := range 20 {
		a.Push(make([]int16, 512))
		a.Bands(24, later.Add(time.Duration(i)*100*time.Millisecond))
	}
	if !a.Quiet() {
		t.Error("the bars did not fall after two seconds of silence")
	}
}

// Nobody reading means nothing is kept: the audio side must cost nothing while the screen is elsewhere.
func TestPushIgnoredUnwatched(t *testing.T) {
	a := New(48000, 2048)
	b := make([]byte, 4*2048)
	for i, v := range sine(1000, 48000, 2048, 0.5) {
		binary.LittleEndian.PutUint16(b[4*i:], uint16(v))
		binary.LittleEndian.PutUint16(b[4*i+2:], uint16(v))
	}
	a.PushStereo(b)
	for _, v := range a.ring {
		if v != 0 {
			t.Fatal("samples kept with nobody watching")
		}
	}
	a.Bands(24, time.Now())
	a.PushStereo(b)
	kept := false
	for _, v := range a.ring {
		kept = kept || v != 0
	}
	if !kept {
		t.Error("samples not kept while watched")
	}
}
