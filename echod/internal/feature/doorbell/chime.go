package doorbell

import (
	"math"
	"sync"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// The chime is a mechanical door chime's: three tone bars struck one after another, each ringing on
// under the next, high to low (G, E, C), played twice. What makes it sound struck rather than beeped
// is in strike: a bright click as the plunger hits, a fundamental that dies away over a couple of
// seconds, overtones that die sooner, and a slow beat from a bar never quite in tune with itself.
//
// The bars are G5, E5 and C5, a real chime's own register. Tried an octave up first, where the small
// speaker is louder, it sounded too high; the overtones carry these down here (a plain E5 and C5 beep
// was too quiet to fetch anybody).

// bars are the three notes, highest first, in hertz.
var bars = []float64{784.0, 659.25, 523.25}

const (
	strikeEvery = 0.42 // seconds between one bar being struck and the next
	roundGap    = 1.1  // seconds from the last bar of a round to the first of the next
	rounds      = 2
	ringOut     = 2.2 // seconds a bar is let ring after it is struck
)

// partial is one overtone of a struck bar: its frequency as a multiple of the fundamental, how loud it
// starts, and how long it takes to fall to a third.
type partial struct {
	ratio, amp, decay float64
}

// barPartials are a struck bar's: the fundamental and a twin a hair off it (the beat), the octave, and
// the inharmonic partials of a free bar (2.76, 5.40), which are the metal in the sound and go first.
var barPartials = []partial{
	{1.000, 1.00, 1.30},
	{1.003, 0.35, 1.10},
	{2.000, 0.22, 0.55},
	{2.756, 0.18, 0.22},
	{5.404, 0.10, 0.05},
}

// dingDong is the whole chime, made once.
var dingDong = sync.OnceValue(func() []speaker.Note {
	return []speaker.Note{speaker.Synth(render(speaker.Rate))}
})

// render is the chime as mono samples at rate.
func render(rate int) []int16 {
	roundLen := float64(len(bars)-1)*strikeEvery + roundGap
	total := float64(rounds-1)*roundLen + float64(len(bars)-1)*strikeEvery + ringOut
	out := make([]float64, int(total*float64(rate)))
	for r := range rounds {
		for i, f := range bars {
			at := float64(r)*roundLen + float64(i)*strikeEvery
			strike(out, int(at*float64(rate)), f, rate)
		}
	}
	peak := 0.0
	for _, v := range out {
		peak = max(peak, math.Abs(v))
	}
	pcm := make([]int16, len(out))
	if peak == 0 {
		return pcm
	}
	for i, v := range out {
		pcm[i] = int16(math.Round(v / peak * 0.98 * math.MaxInt16))
	}
	return pcm
}

// strike adds one bar, struck at sample from, ringing for ringOut.
func strike(out []float64, from int, freq float64, rate int) {
	n := min(int(ringOut*float64(rate)), len(out)-from)
	attack := float64(rate) * 0.002 // the plunger's hit: 2 ms up, not a click of the speaker's own
	fade := float64(rate) * 0.15    // and the last 150 ms let down to nothing
	for i := range n {
		t := float64(i) / float64(rate)
		env := math.Min(1, float64(i)/attack) * math.Min(1, float64(n-i)/fade)
		var v float64
		for _, p := range barPartials {
			v += p.amp * math.Exp(-t/p.decay) * math.Sin(2*math.Pi*freq*p.ratio*t)
		}
		out[from+i] += env * v
	}
}
