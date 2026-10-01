//go:build !dot

package dashboard

import (
	"encoding/binary"
	"testing"
	"time"
)

type fakeSound struct {
	clocks []int64
	audio  []int64
	closed bool
}

func (f *fakeSound) Clock(us int64)           { f.clocks = append(f.clocks, us) }
func (f *fakeSound) Audio(us int64, _ []byte) { f.audio = append(f.audio, us) }
func (f *fakeSound) Close()                   { f.closed = true }

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
	a.message(kindAudio, stamped(20, 0, 0, 0, 0))
	a.message(kindClock, []byte{1, 2}) // too short: ignored
	if gotLatency != 400*time.Millisecond {
		t.Errorf("latency = %v, want 400ms", gotLatency)
	}
	if len(fake.clocks) != 1 || fake.clocks[0] != 10 || len(fake.audio) != 1 || fake.audio[0] != 20 {
		t.Errorf("clocks %v audio %v", fake.clocks, fake.audio)
	}
	a.close()
	if !fake.closed || a.out != nil {
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
	newSound = func(time.Duration) soundOut { t.Fatal("speaker taken without a clock"); return nil }
	defer func() { newSound = old }()
	a := newSoundState()
	a.message(kindSetup, []byte(`{"latency_ms":300}`))
	a.message(kindAudio, stamped(1, 0, 0, 0, 0))
	a.close()
}
