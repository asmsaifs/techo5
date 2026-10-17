package speaker

import (
	"testing"
)

// fakeSink takes what is written and counts pauses and resumes.
type fakeSink struct{ pauses, resumes int }

func (f *fakeSink) Write(b []byte) (int, error) { return len(b), nil }
func (f *fakeSink) Pause() error                { f.pauses++; return nil }
func (f *fakeSink) Resume() error               { f.resumes++; return nil }
func (f *fakeSink) Close() error                { return nil }

// Whether sound is going to the sink: not while it is quiet or paused for silence, and from the first
// period with something in it, told once, through the short gaps in speech.
func TestSinkSoundingFollowsTheSound(t *testing.T) {
	p := New()
	told := 0
	p.OnSinkSound.Listen(func(struct{}) { told++ })
	sink := &fakeSink{}
	p.SetSink(sink, "test", Rate, Channels)
	s := p.sink.Load()

	silent := make([]byte, period*Channels*2)
	sound := make([]byte, period*Channels*2)
	for i := range sound {
		sound[i] = 0x10
	}
	if _, ok := p.SinkSounding(); ok {
		t.Fatal("a sink that was just attached is sounding")
	}
	s.push(silent)
	if _, ok := p.SinkSounding(); ok {
		t.Fatal("a sink given silence is sounding")
	}
	s.push(sound)
	if _, ok := p.SinkSounding(); !ok || told != 1 {
		t.Fatalf("sound started: sounding %t, told %d times", ok, told)
	}
	// A gap shorter than the pause keeps it sounding, and is not a new start.
	for range sinkQuietAfter - 1 {
		s.push(silent)
	}
	s.push(sound)
	if _, ok := p.SinkSounding(); !ok || told != 1 {
		t.Fatalf("after a short gap: sounding %t, told %d times", ok, told)
	}
	// Quiet long enough to pause: no longer sounding, and sound after it is a new start.
	for range sinkQuietAfter {
		s.push(silent)
	}
	if _, ok := p.SinkSounding(); ok || sink.pauses != 1 {
		t.Fatalf("paused for silence: sounding %t, pauses %d", ok, sink.pauses)
	}
	s.push(sound)
	if _, ok := p.SinkSounding(); !ok || told != 2 || sink.resumes != 1 {
		t.Fatalf("sound after the pause: sounding %t, told %d, resumes %d", ok, told, sink.resumes)
	}
	p.SetSink(nil, "", 0, 0)
	if _, ok := p.SinkSounding(); ok {
		t.Fatal("no sink, and sounding")
	}
}
