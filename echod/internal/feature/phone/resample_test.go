package phone

import (
	"math"
	"testing"
)

func tone(freq, rate float64, n, from int) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(10000 * math.Sin(2*math.Pi*freq*float64(from+i)/rate))
	}
	return out
}

func rms(s []int16) float64 {
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(s)))
}

// Speech passes at its level, whether it arrives in one piece or in 20 ms frames.
func TestDownKeepsSpeechAcrossFrames(t *testing.T) {
	d := newDown()
	var out []int16
	for f := 0; f < 50; f++ {
		out = append(out, d.Run(tone(1000, 16000, 320, f*320))...)
	}
	if len(out) != 8000 {
		t.Fatalf("got %d samples, want 8000", len(out))
	}
	got := rms(out[100:])
	if want := 10000 / math.Sqrt2; math.Abs(got-want)/want > 0.05 {
		t.Fatalf("1 kHz level %.0f, want about %.0f", got, want)
	}
}

// What a call cannot carry is removed rather than folded back down into it.
func TestDownRemovesWhatWouldAlias(t *testing.T) {
	d := newDown()
	out := d.Run(tone(6000, 16000, 16000, 0))
	if got := rms(out[100:]); got > 200 {
		t.Fatalf("6 kHz came through at %.0f", got)
	}
}

func TestUpKeepsSpeechAcrossFrames(t *testing.T) {
	u := newUp()
	var out []int16
	for f := 0; f < 50; f++ {
		out = append(out, u.Run(tone(1000, 8000, 160, f*160))...)
	}
	if len(out) != 16000 {
		t.Fatalf("got %d samples, want 16000", len(out))
	}
	got := rms(out[100:])
	if want := 10000 / math.Sqrt2; math.Abs(got-want)/want > 0.05 {
		t.Fatalf("1 kHz level %.0f, want about %.0f", got, want)
	}
	// The image at 7 kHz that doubling the rate makes is filtered out: no sample-to-sample jumps
	// bigger than a 1 kHz sine at 16 kHz can make.
	limit := 10000 * 2 * math.Sin(math.Pi*1000/16000) * 1.1
	for i := 101; i < len(out); i++ {
		if math.Abs(float64(out[i])-float64(out[i-1])) > limit {
			t.Fatalf("jump at %d: %d to %d", i, out[i-1], out[i])
		}
	}
}
