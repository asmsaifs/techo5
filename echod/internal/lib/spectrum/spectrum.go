// Package spectrum turns a stream of audio into bars to look at: a handful of bands from the low end
// of a voice to its top, each 0 to 1, rising fast and falling slowly like the meters on a stereo.
//
// It is built to cost nothing while nobody is looking. The audio side only copies samples into a
// ring, and only while a reader has asked for bars in the last second; the transform runs when bars
// are read, at the screen's pace, not at the audio's.
package spectrum

import (
	"encoding/binary"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/fft"
)

// Mic is the room as the microphone hears it, and Speaker what the device is playing.
var (
	Mic     = New(16000, 512)
	Speaker = New(48000, 2048)
)

// watched is how long a read keeps the audio side copying samples.
const watched = time.Second

// The bands span 90 Hz to 7 kHz, where a voice lives, spaced evenly on a log scale.
const (
	lowHz  = 90
	highHz = 7000
)

// A band reads 0 at floorDB and 1 at topDB, relative to a full-scale sine.
const (
	floorDB = -72.0
	topDB   = -18.0
)

// Analyzer is one stream.
type Analyzer struct {
	rate int
	n    int

	until atomic.Int64 // unix nanoseconds until which Push keeps samples

	mu     sync.Mutex
	ring   []float32
	pos    int
	f      *fft.FFT
	buf    []complex64
	window []float32
	level  []float64
	peak   []float64
	held   []time.Time
	last   time.Time
}

// New makes an analyzer for audio at rate, reading windows of n samples (a power of two).
func New(rate, n int) *Analyzer {
	a := &Analyzer{rate: rate, n: n, ring: make([]float32, n), f: fft.New(n), buf: make([]complex64, n), window: make([]float32, n)}
	for i := range a.window {
		a.window[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	return a
}

// watching is whether a reader wants samples now.
func (a *Analyzer) watching() bool { return time.Now().UnixNano() < a.until.Load() }

// Push takes mono samples. While nobody is reading it returns at once.
func (a *Analyzer) Push(samples []int16) {
	if !a.watching() {
		return
	}
	a.mu.Lock()
	for _, v := range samples {
		a.ring[a.pos] = float32(v) / 32768
		a.pos = (a.pos + 1) % a.n
	}
	a.mu.Unlock()
}

// PushStereo takes interleaved 16-bit little-endian stereo, as a playback period is, and keeps the
// average of the two channels. While nobody is reading it returns at once.
func (a *Analyzer) PushStereo(b []byte) {
	if !a.watching() {
		return
	}
	a.mu.Lock()
	for i := 0; i+3 < len(b); i += 4 {
		l := int16(binary.LittleEndian.Uint16(b[i:]))
		r := int16(binary.LittleEndian.Uint16(b[i+2:]))
		a.ring[a.pos] = (float32(l) + float32(r)) / 65536
		a.pos = (a.pos + 1) % a.n
	}
	a.mu.Unlock()
}

// Bands returns bands bars and the peak held over each, both 0 to 1, as of now. The slices are the
// analyzer's own and are only good until the next call.
func (a *Analyzer) Bands(bands int, now time.Time) (level, peak []float64) {
	a.until.Store(now.Add(watched).UnixNano())

	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.level) != bands {
		a.level, a.peak, a.held = make([]float64, bands), make([]float64, bands), make([]time.Time, bands)
	}
	dt := 0.1
	if !a.last.IsZero() {
		dt = min(max(now.Sub(a.last).Seconds(), 0), 0.5)
	}
	a.last = now

	for i := range a.buf {
		v := a.ring[(a.pos+i)%a.n] * a.window[i]
		a.buf[i] = complex(v, 0)
	}
	a.f.Forward(a.buf)

	hzPerBin := float64(a.rate) / float64(a.n)
	top := min(highHz, float64(a.rate)/2)
	ratio := math.Pow(top/lowHz, 1/float64(bands))
	norm := float64(a.n) / 4 // a full-scale sine's peak bin under a Hann window
	for b := range bands {
		lo := int(lowHz * math.Pow(ratio, float64(b)) / hzPerBin)
		hi := int(lowHz * math.Pow(ratio, float64(b+1)) / hzPerBin)
		hi = max(hi, lo+1)
		var m float64
		for k := lo; k < hi && k < a.n/2; k++ {
			re, im := float64(real(a.buf[k])), float64(imag(a.buf[k]))
			m = max(m, math.Sqrt(re*re+im*im))
		}
		v := 0.0
		if m > 0 {
			v = (20*math.Log10(m/norm) - floorDB) / (topDB - floorDB)
		}
		v = min(max(v, 0), 1)

		// Quick up, slow down: a meter a voice can move but that does not flicker.
		if v > a.level[b] {
			a.level[b] += (v - a.level[b]) * 0.7
		} else {
			a.level[b] = max(v, a.level[b]-1.6*dt)
		}
		// The peak rides up with the bar, holds a moment, then falls.
		if a.level[b] >= a.peak[b] {
			a.peak[b], a.held[b] = a.level[b], now
		} else if now.Sub(a.held[b]) > 400*time.Millisecond {
			a.peak[b] = max(a.level[b], a.peak[b]-0.9*dt)
		}
	}
	return a.level, a.peak
}

// Quiet is whether every bar and peak has fallen to nothing.
func (a *Analyzer) Quiet() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.level {
		if a.level[i] > 0.01 || a.peak[i] > 0.01 {
			return false
		}
	}
	return true
}
