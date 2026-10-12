package speaker

import (
	"math"
	"testing"
)

// Tuned, a ring and the music are weighed at the level they come out at, the curve's in front of the
// tuning, not the usual curve's, which on the Spot and the Dot stands still across some steps where the
// curve in front rises: a ring set above the music goes out at its own step, and the music under it
// is turned down by what the curve in front puts between them.
func TestARingIsWeighedOnTheCurveItPlaysAt(t *testing.T) {
	var first [VolumeSteps + 1]float64
	first[0] = mute
	for s := 1; s <= VolumeSteps; s++ {
		first[s] = -40 + float64(s) // a dB a step
	}
	for _, c := range []struct{ music, ring int }{{20, 25}, {23, 27}, {5, 6}} {
		mg, rg := gainForStep(OutputSpeaker, c.music), gainForStep(OutputSpeaker, c.ring)
		mediaK, bellK, g, s := ringBalance(&first, OutputSpeaker, mg, c.music, rg, c.ring)
		if s != c.ring || g != rg || bellK != 1 {
			t.Errorf("ring %d over music %d: out at step %d gain %v, ring scaled %v", c.ring, c.music, s, g, bellK)
		}
		if want := float32(math.Pow(10, float64(c.music-c.ring)/20)); math.Abs(float64(mediaK-want)) > 1e-4 {
			t.Errorf("ring %d over music %d: the music scaled %v, want %v", c.ring, c.music, mediaK, want)
		}
	}

	// A ring set below the music goes in under it at the curve's difference.
	mg, rg := gainForStep(OutputSpeaker, 25), gainForStep(OutputSpeaker, 20)
	mediaK, bellK, _, s := ringBalance(&first, OutputSpeaker, mg, 25, rg, 20)
	if want := float32(math.Pow(10, -5.0/20)); s != 25 || mediaK != 1 || math.Abs(float64(bellK-want)) > 1e-4 {
		t.Errorf("ring 20 under music 25: step %d, music %v, ring %v; want 25, 1, %v", s, mediaK, bellK, want)
	}
}
