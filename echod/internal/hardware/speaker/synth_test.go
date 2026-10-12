package speaker

import (
	"math"
	"testing"
)

// A sound made in code plays at a tone's loudness, and is as long as its samples, never looking for a
// file of its own or of the owner's.
func TestSynth(t *testing.T) {
	samples := make([]int16, Rate/2)
	for i := range samples {
		samples[i] = int16(20000 * math.Sin(2*math.Pi*440*float64(i)/Rate))
	}
	n := Synth(samples)
	if n.Clip == nil || n.Ms != 500 || n.Clip.Ms() != 500 {
		t.Fatalf("note %+v, clip ms %d", n, n.Clip.Ms())
	}
	if got, want := n.Clip.peak, toneLevel; math.Abs(got-want) > 0.001 {
		t.Errorf("peak %.3f, want a tone's %.3f", got, want)
	}
	level := 0.6
	out := tone(n, level)
	most := 0
	for _, s := range out {
		most = max(most, abs(int(s)))
	}
	if want := int(level * math.MaxInt16); most < want-50 || most > want+50 {
		t.Errorf("rendered at level 0.6 peaks at %d, want about %d", most, want)
	}
	if len(out) != len(samples)*Channels {
		t.Errorf("%d samples out, want %d", len(out), len(samples)*Channels)
	}
	if s := Synth(nil); s.Ms != 0 || s.Clip.Ms() != 0 {
		t.Errorf("an empty sound is %+v", s)
	}
}
