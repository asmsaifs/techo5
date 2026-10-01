// Package halfrate converts between 16 kHz, the microphones' and the voice path's rate, and 8 kHz, a
// phone call's and a camera's talk-back channel's. Both directions keep their state between frames, so
// a stream is one continuous signal rather than 20 ms pieces with a click at every join.
package halfrate

import "math"

// halfTaps is a windowed-sinc low-pass at 3.5 kHz for a 16 kHz signal, just under the 4 kHz a call can
// carry, which is what has to go before every other sample is dropped: anything above it would fold
// back down into the call as noise. Odd length and symmetric, so no phase distortion; 1 ms of delay.
var halfTaps = lowpass(31, 3500.0/16000)

// Down halves the sample rate.
type Down struct {
	hist []float64 // the last len(halfTaps)-1 input samples
	odd  bool      // whether the next input sample is one that is dropped
}

// NewDown starts a conversion from 16 kHz to 8 kHz.
func NewDown() *Down { return &Down{hist: make([]float64, len(halfTaps)-1)} }

// Run takes 16 kHz and returns 8 kHz.
func (d *Down) Run(in []int16) []int16 {
	out := make([]int16, 0, len(in)/2+1)
	n := len(halfTaps)
	for _, s := range in {
		d.hist = append(d.hist, float64(s))
		if !d.odd {
			var acc float64
			w := d.hist[len(d.hist)-n:]
			for i, t := range halfTaps {
				acc += t * w[i]
			}
			out = append(out, Clamp(acc))
		}
		d.odd = !d.odd
		d.hist = d.hist[len(d.hist)-(n-1):]
	}
	return out
}

// Up doubles the sample rate: a zero between every sample, then the same low-pass with twice the gain
// to fill them in.
type Up struct {
	hist []float64
}

// NewUp starts a conversion from 8 kHz to 16 kHz.
func NewUp() *Up { return &Up{hist: make([]float64, len(halfTaps)-1)} }

// Run takes 8 kHz and returns 16 kHz.
func (u *Up) Run(in []int16) []int16 {
	out := make([]int16, 0, 2*len(in))
	n := len(halfTaps)
	push := func(v float64) {
		u.hist = append(u.hist, v)
		var acc float64
		w := u.hist[len(u.hist)-n:]
		for i, t := range halfTaps {
			acc += t * w[i]
		}
		out = append(out, Clamp(2*acc))
		u.hist = u.hist[len(u.hist)-(n-1):]
	}
	for _, s := range in {
		push(float64(s))
		push(0)
	}
	return out
}

// lowpass is n taps of a Hann-windowed sinc with its cutoff at fc of the sample rate, scaled to unity
// gain at DC.
func lowpass(n int, fc float64) []float64 {
	taps := make([]float64, n)
	mid := float64(n-1) / 2
	var sum float64
	for i := range taps {
		x := float64(i) - mid
		s := 2 * fc
		if x != 0 {
			s = math.Sin(2*math.Pi*fc*x) / (math.Pi * x)
		}
		w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		taps[i] = s * w
		sum += taps[i]
	}
	for i := range taps {
		taps[i] /= sum
	}
	return taps
}

// Clamp rounds v to the nearest sample, held to 16 bits.
func Clamp(v float64) int16 {
	switch {
	case v > 32767:
		return 32767
	case v < -32768:
		return -32768
	case v < 0:
		return int16(v - 0.5)
	}
	return int16(v + 0.5)
}
