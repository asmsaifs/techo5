//go:build !dot

package display

import "math"

// eqVoice is a made-up voice's spectrum: most of it low, a little higher up.
func eqVoice(amp float64, seed int) (level, peak []float64) {
	level, peak = make([]float64, eqBands), make([]float64, eqBands)
	for i := range level {
		f := float64(i) / (eqBands - 1)
		env := 0.85*math.Exp(-(f-0.28)*(f-0.28)/0.05) + 0.25*math.Exp(-(f-0.62)*(f-0.62)/0.02)
		wobble := 0.75 + 0.5*math.Abs(math.Sin(float64(i*7+seed)))
		level[i] = min(1, max(0.04, amp*env*wobble))
		peak[i] = min(1, level[i]+0.06+0.1*math.Abs(math.Cos(float64(i*3+seed))))
	}
	return level, peak
}
