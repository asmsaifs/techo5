package media

import (
	"math"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// speechLike is a second of a 200 Hz tone whose level moves like words: loud syllables, quiet ones
// and short gaps, normalized as replies are.
func speechLike() []int16 {
	out := make([]int16, speaker.VoiceRate)
	for i := range out {
		t := float64(i) / speaker.VoiceRate
		level := []float64{0.5, 0.08, 0.3, 0, 0.05, 0.4, 0.1, 0}[int(t*8)%8]
		out[i] = int16(level * 32767 * math.Sin(2*math.Pi*200*t))
	}
	return Normalize(out)
}

func rms(in []int16) float64 { return math.Sqrt(SumSquares(in) / float64(len(in))) }

func peak(in []int16) int {
	p := 0
	for _, s := range in {
		p = max(p, int(math.Abs(float64(s))))
	}
	return p
}

// Louder speech lifts the average by several dB, lifts the quiet parts more than the loud ones, and
// keeps the peaks within full scale.
func TestLouderLiftsSpeechAndKeepsPeaks(t *testing.T) {
	in := speechLike()
	var l Louder
	out := l.Process(in)
	gain := 20 * math.Log10(rms(out)/rms(in))
	t.Logf("average gain %.1f dB, peak %d to %d", gain, peak(in), peak(out))
	if gain < 3 || gain > 9 {
		t.Errorf("average gain %.1f dB, want 3 to 9", gain)
	}
	quiet := func(s []int16) float64 { n := speaker.VoiceRate / 8; return rms(s[n : 2*n]) }
	loud := func(s []int16) float64 { return rms(s[:speaker.VoiceRate/8]) }
	if quiet(out)/quiet(in) <= loud(out)/loud(in) {
		t.Errorf("the quiet syllable was not lifted more than the loud one")
	}
	if peak(out) > 32767 {
		t.Errorf("peak %d over full scale", peak(out))
	}
	silent := l.Process(make([]int16, 100))
	if peak(silent) != 0 {
		t.Errorf("silence came out %d", peak(silent))
	}
}

// A reply compressed in chunks comes out as it does in one piece: the level carries across.
func TestLouderCarriesAcrossChunks(t *testing.T) {
	in := speechLike()
	var whole, parts Louder
	want := whole.Process(in)
	var got []int16
	for i := 0; i < len(in); i += 320 {
		got = append(got, parts.Process(in[i:min(i+320, len(in))])...)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("sample %d: %d in one piece, %d in chunks", i, want[i], got[i])
		}
	}
}
