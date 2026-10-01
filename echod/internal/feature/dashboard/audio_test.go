//go:build !dot

package dashboard

import (
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

type fakeSound struct {
	mu     sync.Mutex
	clocks []int64
	audio  []int64
	closed bool
}

func (f *fakeSound) Clock(us int64)           { f.clocks = append(f.clocks, us) }
func (f *fakeSound) Audio(us int64, _ []byte) { f.audio = append(f.audio, us) }
func (f *fakeSound) Close()                   { f.mu.Lock(); f.closed = true; f.mu.Unlock() }
func (f *fakeSound) isClosed() bool           { f.mu.Lock(); defer f.mu.Unlock(); return f.closed }

func stamped(us int64, pcm ...byte) []byte {
	b := binary.BigEndian.AppendUint64(nil, uint64(us))
	return append(b, pcm...)
}

func TestSound(t *testing.T) {
	fake := &fakeSound{}
	var gotLatency time.Duration
	old := newSound
	newSound = func(l time.Duration) soundOut { gotLatency = l; return fake }
	defer func() { newSound = old }()

	a := newSoundState()
	a.message(kindAudio, stamped(1, 0, 0, 0, 0))
	if fake.audio != nil {
		t.Fatal("audio before any clock was played")
	}
	a.message(kindSetup, []byte(`{"latency_ms":400}`))
	a.message(kindClock, stamped(10))
	if gotLatency != 0 {
		t.Fatal("the speaker was taken for a clock message")
	}
	time.Sleep(20 * time.Millisecond)
	a.message(kindAudio, stamped(20, 0, 0, 0, 0))
	a.message(kindClock, []byte{1, 2}) // too short: ignored
	if gotLatency != 400*time.Millisecond {
		t.Errorf("latency = %v, want 400ms", gotLatency)
	}
	// The output hears the clock that came before it, aged by the 20 ms since.
	if len(fake.clocks) != 1 || fake.clocks[0] < 10+20_000 || fake.clocks[0] > 10+200_000 {
		t.Errorf("clocks %v", fake.clocks)
	}
	if len(fake.audio) != 1 || fake.audio[0] != 20 {
		t.Errorf("audio %v", fake.audio)
	}
	a.message(kindClock, stamped(30)) // with an output, clocks go straight to it
	if len(fake.clocks) != 2 || fake.clocks[1] != 30 {
		t.Errorf("clocks %v", fake.clocks)
	}
	a.close()
	if !fake.isClosed() || a.out != nil {
		t.Error("close did not release the output")
	}
}

func TestSoundLatencyIsClamped(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want time.Duration
	}{
		{`{"latency_ms":5}`, audioLatencyMin},
		{`{"latency_ms":60000}`, audioLatencyMax},
		{`{"latency_ms":0}`, audioLatency},
		{`nonsense`, audioLatency},
	} {
		a := newSoundState()
		a.message(kindSetup, []byte(tt.in))
		if a.latency != tt.want {
			t.Errorf("%s: latency = %v, want %v", tt.in, a.latency, tt.want)
		}
	}
}

func TestSilentDeckNeverTakesTheSpeaker(t *testing.T) {
	old := newSound
	newSound = func(time.Duration) soundOut { t.Fatal("speaker taken without audio"); return nil }
	defer func() { newSound = old }()
	a := newSoundState()
	a.message(kindSetup, []byte(`{"latency_ms":300}`))
	a.message(kindClock, stamped(5))
	a.message(kindClock, stamped(6))
	a.close()
}

func TestSpeakerIsLetGoWhenAudioStops(t *testing.T) {
	var made []*fakeSound
	old := newSound
	newSound = func(time.Duration) soundOut { f := &fakeSound{}; made = append(made, f); return f }
	defer func() { newSound = old }()

	a := newSoundState()
	a.idle = 50 * time.Millisecond
	a.message(kindClock, stamped(10))
	// Audio holds it for as long as it comes.
	for i := 0; i < 4; i++ {
		a.message(kindAudio, stamped(30, 0, 0, 0, 0))
		time.Sleep(30 * time.Millisecond)
	}
	if len(made) != 1 || made[0].isClosed() {
		t.Fatalf("the speaker was let go while audio was arriving: %d outputs", len(made))
	}
	time.Sleep(120 * time.Millisecond)
	if !made[0].isClosed() {
		t.Fatal("the speaker was kept after the audio stopped")
	}
	// Clock messages from a server still running with a silent page do not take it back.
	a.message(kindClock, stamped(40))
	time.Sleep(80 * time.Millisecond)
	if len(made) != 1 {
		t.Errorf("a clock message took the speaker: %d outputs", len(made))
	}
	// Audio does, with the clock it already had.
	a.message(kindAudio, stamped(50, 0, 0, 0, 0))
	if len(made) != 2 || len(made[1].clocks) != 1 {
		t.Errorf("audio did not take the speaker again: %d outputs", len(made))
	}
	a.close()
}
