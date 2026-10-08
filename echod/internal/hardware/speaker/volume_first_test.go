//go:build !dot && !spot

package speaker

import (
	"math"
	"math/rand"
	"os"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/asp"
)

// The curves in front of the tuning rise with every step, start at mute, and the ratio that turns the
// usual gain into one of them lands exactly on it.
func TestFirstCurves(t *testing.T) {
	for name, curve := range firstCurves {
		if curve[0] > mute {
			t.Errorf("%s: step 0 is %v dB, not muted", name, curve[0])
		}
		for s := 1; s <= VolumeSteps; s++ {
			if curve[s] <= curve[s-1] {
				t.Errorf("%s: step %d (%v dB) is not louder than step %d (%v dB)", name, s, curve[s], s-1, curve[s-1])
			}
			got := float64(gainForStep(OutputSpeaker, s) * firstRatio(&curve, OutputSpeaker, s))
			if want := math.Pow(10, curve[s]/20); math.Abs(got-want) > want*1e-4 {
				t.Errorf("%s step %d: %v, want %v", name, s, got, want)
			}
		}
		if r := firstRatio(&curve, OutputSpeaker, 0); r != 0 {
			t.Errorf("%s: step 0 lets %v through", name, r)
		}
	}
}

// musicLike is a few seconds of something like mastered pop: a kick on every beat peaking near full
// scale, a bass line, and pink noise for the rest.
func musicLike(seconds int) []float32 {
	r := rand.New(rand.NewSource(1))
	x := make([]float32, asp.Rate*seconds)
	var b0, b1, b2 float64
	for i := range x {
		tm := float64(i) / asp.Rate
		beat := math.Mod(tm, 0.5)
		kick := 0.0
		if beat < 0.2 {
			kick = 0.6 * math.Exp(-beat*18) * math.Sin(2*math.Pi*(50+100*math.Exp(-beat*35))*beat)
		}
		bass := 0.18 * math.Sin(2*math.Pi*[]float64{55, 65.4, 49, 73.4}[int(tm/2)%4]*tm)
		w := r.NormFloat64()
		b0 = 0.99765*b0 + w*0.0990460
		b1 = 0.96300*b1 + w*0.2965164
		b2 = 0.57000*b2 + w*1.0526913
		x[i] = float32(kick + bass + (b0+b1+b2+w*0.1848)*0.045)
	}
	return x
}

// levels plays src through a fresh chain at step with pre in front and post behind, and gives the
// second half's RMS and peak, in dB.
func levels(t *testing.T, tun *asp.Tuning, src []float32, step int, pre, post float32) (rms, peak float64) {
	c, err := tun.Chain(period)
	if err != nil {
		t.Fatal(err)
	}
	c.Volume(float64(step) / VolumeSteps)
	x := make([]float32, len(src)/period*period)
	for i := range x {
		x[i] = src[i] * pre
	}
	for i := 0; i < len(x); i += period {
		c.Process(x[i : i+period])
	}
	var sum, most float64
	tail := x[len(x)/2:]
	for _, v := range tail {
		v := float64(v * post)
		sum += v * v
		most = math.Max(most, math.Abs(v))
	}
	return 10 * math.Log10(sum/float64(len(tail))), 20 * math.Log10(most)
}

// With the real tuning, every step in front of it is as loud as the same step was behind it, and the
// music keeps its punch: the peaks stand further above the average than they did when the compressor
// was flattening every kick. Needs a copy of the unit's tuning:
//
//	ECHOLOCAL_VENDOR_DIR=/tmp/coefs go test ./internal/hardware/speaker/ -run VolumeInFront -v
func TestVolumeInFrontKeepsLoudnessAndPunch(t *testing.T) {
	dir := os.Getenv("ECHOLOCAL_VENDOR_DIR")
	if dir == "" {
		t.Skip("set ECHOLOCAL_VENDOR_DIR to a copy of /vendor/etc/audio-algorithms")
	}
	tun, err := asp.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	curve, ok := firstCurves[tun.Name()]
	if !ok {
		t.Skipf("no curve in front for the %s tuning", tun.Name())
	}
	src := musicLike(6)
	for _, step := range []int{1, 5, 10, 13, 15, 20, 25, 28, 30} {
		g := gainForStep(OutputSpeaker, step)
		oldRMS, oldPeak := levels(t, tun, src, step, 1, g)
		newRMS, newPeak := levels(t, tun, src, step, g*firstRatio(&curve, OutputSpeaker, step), 1)
		if math.Abs(newRMS-oldRMS) > 0.5 {
			t.Errorf("step %d: %.1f dB, was %.1f dB", step, newRMS, oldRMS)
		}
		if oldCrest, newCrest := oldPeak-oldRMS, newPeak-newRMS; newCrest < oldCrest+1 {
			t.Errorf("step %d: peaks %.1f dB over the average, %.1f dB before", step, newCrest, oldCrest)
		}
		t.Logf("step %2d: %.1f dB (was %.1f), peaks %.1f dB over it (was %.1f)", step, newRMS, oldRMS, newPeak-newRMS, oldPeak-oldRMS)
	}
}
