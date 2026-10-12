package speaker

import "math"

// Synth is a sound made in code, mono at Rate, as a note: for a sound the plain notes cannot make,
// such as a struck bar ringing on under the next (feature/doorbell). It plays like a recording
// (clips.go), scaled so its loudest sample is a tone's at the same level, so a level means the same
// for it as for a beep.
func Synth(samples []int16) Note {
	most := 0
	for _, s := range samples {
		most = max(most, abs(int(s)))
	}
	scaled := make([]int16, len(samples))
	if most > 0 {
		g := toneLevel * math.MaxInt16 / float64(most)
		for i, s := range samples {
			scaled[i] = int16(math.Round(float64(s) * g))
		}
	}
	c := &Clip{made: true, loaded: true}
	c.set(scaled)
	return Note{Clip: c, Ms: len(scaled) * 1000 / Rate}
}
