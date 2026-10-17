package echoref

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// TestFillLinesUp plays a click through Push, stamped as a card would stamp it, and checks Fill puts
// it where the microphones would hear it, Lead early.
func TestFillLinesUp(t *testing.T) {
	const (
		rate   = 48000
		period = 768
		click  = 20000 // the input frame the click is on
	)
	played = &ring{clock: NewClock(Rate)}
	t0 := time.Now()
	pcm := make([]byte, period*4)
	for n := 0; n < 40; n++ {
		clear(pcm)
		if at := click - n*period; at >= 0 && at < period {
			binary.LittleEndian.PutUint16(pcm[4*at:], uint16(int16(16000)))
			binary.LittleEndian.PutUint16(pcm[4*at+2:], uint16(int16(16000)))
		}
		// A few hundred microseconds of jitter in each reading, as a delay read beside a clock has.
		jitter := time.Duration((n%5)-2) * 150 * time.Microsecond
		end := t0.Add(time.Duration((n+1)*period-1)*time.Second/rate + jitter)
		Push(pcm, rate, end)
	}

	// The click is heard at t0 + click/rate. Read a stretch around it.
	heard := Seconds(t0) + float64(click)/rate
	start := heard - 0.01
	l := make([]float32, Rate/50)
	r := make([]float32, len(l))
	if !Fill(l, r, start) {
		t.Fatal("Fill found nothing")
	}
	peak, at := float32(0), -1
	for i, v := range l {
		if a := float32(math.Abs(float64(v))); a > peak {
			peak, at = a, i
		}
	}
	want := int(math.Round((0.01 - Lead.Seconds()) * Rate))
	if at < want-2 || at > want+2 {
		t.Errorf("click at sample %d of the read, want %d (±2): Lead %v early", at, want, Lead)
	}
	if l[at] != r[at] {
		t.Errorf("left %v and right %v differ", l[at], r[at])
	}
}

// TestFillSilentWithoutSpeaker: nothing pushed is silence, and says so.
func TestFillSilentWithoutSpeaker(t *testing.T) {
	played = &ring{clock: NewClock(Rate)}
	l := []float32{1, 1}
	r := []float32{1, 1}
	if Fill(l, r, Seconds(time.Now())) {
		t.Error("Fill reported audio with no speaker")
	}
	if l[0] != 0 || r[1] != 0 {
		t.Error("Fill left stale samples")
	}
}

// TestClockSnapsOnRestart: a reading far off the line is a stream starting over, not jitter.
func TestClockSnapsOnRestart(t *testing.T) {
	c := NewClock(Rate)
	t0 := time.Now()
	c.Mark(0, t0)
	c.Mark(Rate, t0.Add(time.Second+200*time.Microsecond))
	if d := c.At(0) - Seconds(t0); math.Abs(d) > 50e-6 {
		t.Errorf("jitter moved the line %v s", d)
	}
	c.Mark(Rate, t0.Add(2*time.Second))
	if d := c.At(Rate) - Seconds(t0.Add(2*time.Second)); math.Abs(d) > 1e-6 {
		t.Errorf("a restart was smoothed rather than snapped to: %v s off", d)
	}
}

// TestRestartAfterGap: after the speaker stops and starts again, the reference holds only the new
// audio. Before it, the stretch is empty (the old audio is not placed just ahead of the new one), and
// the new audio's first samples carry nothing of the old through the decimators.
func TestRestartAfterGap(t *testing.T) {
	const (
		rate   = 48000
		period = 768
	)
	played = &ring{clock: NewClock(Rate)}
	stamp := func(from time.Time, n int) time.Time {
		return from.Add(time.Duration((n+1)*period-1) * time.Second / rate)
	}
	loud := make([]byte, period*4)
	for i := 0; i < len(loud); i += 2 {
		binary.LittleEndian.PutUint16(loud[i:], uint16(int16(16000)))
	}
	t0 := time.Now()
	for n := 0; n < 10; n++ {
		Push(loud, rate, stamp(t0, n))
	}
	// Silence, after a gap well past stale.
	t1 := t0.Add(500 * time.Millisecond)
	quiet := make([]byte, period*4)
	for n := 0; n < 10; n++ {
		Push(quiet, rate, stamp(t1, n))
	}

	l := make([]float32, Rate*30/1000)
	r := make([]float32, len(l))
	if Fill(l, r, Seconds(t1)-0.06) {
		t.Error("the stretch before the restart reads as the speaker's: the old audio was kept")
	}
	if !Fill(l, r, played.clock.At(0)-Lead.Seconds()) {
		t.Fatal("Fill found nothing at the start of the new audio")
	}
	for i := range l {
		if math.Abs(float64(l[i])) > 1e-4 || math.Abs(float64(r[i])) > 1e-4 {
			t.Fatalf("sample %d of the new audio is %v/%v, want silence: the old audio carried over", i, l[i], r[i])
		}
	}
}
