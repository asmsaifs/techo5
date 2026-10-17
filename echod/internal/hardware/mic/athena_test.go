//go:build !dot && !spot

package mic

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/audio"
)

// asShow8 sets the frame to the Show 8's for one test: a workstation reads as a Show 5.
func asShow8(t *testing.T) {
	c, m, r := Channels, Mics, RefFirst
	Channels, Mics, RefFirst = show8Channels, show8Mics, show8RefFirst
	t.Cleanup(func() { Channels, Mics, RefFirst = c, m, r })
}

// athenaSignal is the same few tones at any rate, so the two devices can be given the same sound.
func athenaSignal(i, rate int) float64 {
	t := float64(i) / float64(rate)
	return 0.3*math.Sin(2*math.Pi*310*t) + 0.2*math.Sin(2*math.Pi*1730*t+1) + 0.1*math.Sin(2*math.Pi*3190*t+2)
}

// athenaBlock is one Read's worth of both devices: athenaSignal at level on microphone mic, and on the
// reference's channel ref.
func athenaBlock(frames, block, mic int, micLevel float64, ref int, refLevel float64) (mics, refs []byte) {
	micN := frames * athenaMicRate / Rate
	for i := range micN {
		at := block*micN + i
		for ch := range athenaMicChannels {
			v := int32(0)
			if ch == mic {
				// 24 bits, sign-extended in the low three bytes, as the AFE hands them over.
				v = int32(micLevel * athenaSignal(at, athenaMicRate) * (athenaMicFull - 1))
			}
			mics = binary.LittleEndian.AppendUint32(mics, uint32(v))
		}
	}
	refN := frames * athenaRefRate / Rate
	for i := range refN {
		at := block*refN + i
		for ch := range athenaRefChannels {
			v := int16(0)
			if ch == ref {
				v = int16(refLevel * athenaSignal(at, athenaRefRate) * math.MaxInt16)
			}
			refs = binary.LittleEndian.AppendUint16(refs, uint16(v))
		}
	}
	return mics, refs
}

func rmsInt16(xs []int16, skip int) float64 {
	var sum float64
	for _, x := range xs[skip:] {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum / float64(len(xs)-skip))
}

func asFloats(xs []int16) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = float64(x)
	}
	return out
}

// The two devices come out as the 1st gen Show 8's frame: each microphone in its own channel at
// Fire OS's level after its boost, the reference's left and right after them, and the two lined up.
func TestAthenaMakesTheShow8Frame(t *testing.T) {
	asShow8(t)
	c := newPairedCapture(nil, nil)

	const frames, blocks = FrameSamples, 50
	var all []byte
	for b := range blocks {
		mics, refs := athenaBlock(frames, b, 2, 0.5, 1, 0.5)
		out := make([]byte, frames*Channels*Bits/8)
		c.convert(out, mics, refs)
		all = append(all, out...)
	}

	got, ref := Decode(all), Reference(all)
	// Well past the filters filling.
	const skip = 2 * FrameSamples

	boost := math.Pow(10, athenaMicBoostDB/20)
	want := 0.5 * rmsOfSignal() * boost * math.MaxInt16
	for ch := range Mics {
		l := rmsInt16(got[ch], skip)
		switch {
		case ch == 2 && math.Abs(l-want) > 0.02*want:
			t.Errorf("microphone 2: level %.0f, want %.0f", l, want)
		case ch != 2 && l > 1:
			t.Errorf("microphone %d is silent in, but came out at %.0f", ch, l)
		}
	}
	if l := rmsInt16(ref[0], skip); l > 1 {
		t.Errorf("reference left is silent in, but came out at %.0f", l)
	}
	wantRef := 0.5 * rmsOfSignal() * math.MaxInt16
	if l := rmsInt16(ref[1], skip); math.Abs(l-wantRef) > 0.02*wantRef {
		t.Errorf("reference right: level %.0f, want %.0f", l, wantRef)
	}

	// The same sound on both devices comes out at the same moment.
	lag, _ := audio.BestLag(asFloats(got[2][skip:]), asFloats(ref[1][skip:]), 40)
	if lag != 0 {
		t.Errorf("the reference is %d samples off the microphones", lag)
	}
}

// Without a reference the frame still comes out whole, with silence where it would be.
func TestAthenaWithoutAReference(t *testing.T) {
	asShow8(t)
	c := newPairedCapture(nil, nil)

	mics, _ := athenaBlock(FrameSamples, 0, 0, 0.5, 0, 0)
	out := make([]byte, FrameSamples*Channels*Bits/8)
	for i := range out {
		out[i] = 0xff // whatever the buffer held before
	}
	c.convert(out, mics, nil)
	for _, ch := range Reference(out) {
		if l := rmsInt16(ch, 0); l != 0 {
			t.Errorf("reference without a device came out at %.0f", l)
		}
	}
	if l := rmsInt16(Decode(out)[0], FrameSamples/2); l == 0 {
		t.Error("microphone 0 came out silent")
	}
}

// Lookup takes a control's name whole, so every name written has to fit the kernel's 44 bytes.
func TestAthenaControlNamesFit(t *testing.T) {
	names := []string{athenaGainControl}
	for _, c := range athenaControls {
		names = append(names, c.name)
	}
	for _, n := range names {
		if len(n) >= 44 {
			t.Errorf("%q is too long for a control id", n)
		}
	}
	if athenaControls[0].name != "ADC Reset A" {
		t.Errorf("Fire OS resets the converter first; the list starts with %q", athenaControls[0].name)
	}
}

// rmsOfSignal is athenaSignal's RMS: three tones, each amplitude over root two.
func rmsOfSignal() float64 {
	return math.Sqrt((0.3*0.3 + 0.2*0.2 + 0.1*0.1) / 2)
}

// Amazon's driver does not hold a ring to the DMA block it set aside, and one that does not fit is a
// kernel panic (athena.go). Both rings have to fit, with the period count the reader relies on.
func TestAthenaRingsFitTheDriver(t *testing.T) {
	mic := athenaMicPeriod * athenaMicPeriods * athenaMicChannels * athenaMicBits / 8
	if mic > athenaMicRingMax {
		t.Errorf("microphone ring %d bytes, the driver has %d", mic, athenaMicRingMax)
	}
	ref := athenaRefPeriod * athenaRefPeriods * athenaRefChannels * athenaRefBits / 8
	if ref > athenaRefRingMax {
		t.Errorf("reference ring %d bytes, the driver has %d", ref, athenaRefRingMax)
	}
	// The reader takes 20 ms at a time, so a ring has to hold more than that.
	if athenaMicPeriods*athenaMicPeriod*Rate/athenaMicRate <= FrameSamples {
		t.Errorf("a %d-period microphone ring is no deeper than one read", athenaMicPeriods)
	}
}

// The PMIC path is Fire OS's builtin_Mic_SingleMic (audio_device_tas5805m.xml) after its mic type,
// and every name has to fit a control id.
func TestAthenaPmicPath(t *testing.T) {
	if athenaPmicMicOn[0].name != "Audio_MIC1_Mode_Select" || athenaPmicMicOn[0].item != "ACCMODE" {
		t.Errorf("the mic type goes first: %+v", athenaPmicMicOn[0])
	}
	for _, c := range append(append([]struct{ name, item string }{}, athenaPmicMicOn...), athenaPmicMicOff...) {
		if len(c.name) >= 44 || c.item == "" {
			t.Errorf("%+v", c)
		}
	}
	if athenaADCMicBias {
		t.Error("the converter's MICBIAS is on by default; Fire OS never turns it on")
	}
}

// hw:0,17's samples are 24 bits in the low bytes of each word: a sample at 2^23 - 1 is full scale, a
// steady one comes out at its level after the boost, and one past 24 bits saturates rather than wraps.
func TestAthenaMicSamplesAre24BitLowAligned(t *testing.T) {
	asShow8(t)
	run := func(v int32) int16 {
		c := newPairedCapture(nil, nil)
		var mics []byte
		for range 20 * FrameSamples * athenaMicRate / Rate {
			for range athenaMicChannels {
				mics = binary.LittleEndian.AppendUint32(mics, uint32(v))
			}
		}
		out := make([]byte, 20*FrameSamples*Channels*Bits/8)
		c.convert(out, mics, nil)
		d := Decode(out)[0]
		return d[len(d)-1]
	}
	// A quarter of 24-bit full scale, boosted by 5.89 dB, in the 16-bit terms Decode returns.
	want := 0.25 * math.Pow(10, athenaMicBoostDB/20) * math.MaxInt16
	if got := float64(run(1 << 21)); math.Abs(got-want) > 0.01*want {
		t.Errorf("a quarter of 24-bit full scale came out at %.0f, want %.0f", got, want)
	}
	if got := run(1 << 30); got != math.MaxInt16 {
		t.Errorf("a sample past 24 bits came out at %d, want saturation at %d", got, math.MaxInt16)
	}
}
