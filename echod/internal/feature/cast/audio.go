package cast

import (
	"encoding/binary"
	"math"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// holdMax bounds what is held ahead: the phone may send only the latency ahead, so more than this is a
// timestamp that cannot be believed.
const holdMax = 5 * speaker.Rate

// audioOut places a cast's sound by output frame index, as the speaker's Source asks. The first chunk
// fixes where the phone's timeline lands on the card, and each chunk after it goes where its time says,
// so a burst of network jitter moves nothing: the chunk is either in time or dropped.
//
// It does not yet correct for the card's clock running a little fast or slow against the phone's
// (sendspin's does, in output.go): about 200 ppm measured on this hardware, 12 ms a minute, which is
// no trouble for a film clip and will want doing for a long one.
type audioOut struct {
	p *speaker.Player

	mu    sync.Mutex
	ready bool
	held  bool
	gain  float32

	// pcm holds frames from base onward; played is where the card has got to.
	base   uint64
	played uint64
	pcm    []int16

	// The frame that carries the time at, fixed by the first chunk.
	anchored bool
	frame    uint64
	at       time.Time

	late, dropped int
}

var (
	_ speaker.Producer = (*audioOut)(nil)
	_ speaker.Source   = (*audioOut)(nil)
)

func newAudioOut(p *speaker.Player) *audioOut { return &audioOut{p: p, gain: 1} }

// open readies the output for a new cast and takes the speaker's source.
func (o *audioOut) open() {
	o.mu.Lock()
	o.ready, o.held, o.anchored = true, false, false
	o.pcm, o.base, o.played, o.late, o.dropped = nil, 0, 0, 0, 0
	o.mu.Unlock()
	o.p.Attach(o)
}

func (o *audioOut) close() {
	o.p.Attach(nil)
	o.mu.Lock()
	o.ready, o.pcm = false, nil
	o.mu.Unlock()
}

// write places pcm, interleaved S16LE stereo, to be heard at at.
func (o *audioOut) write(pcm []byte, at time.Time) {
	samples := make([]int16, len(pcm)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[2*i:]))
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.ready {
		return
	}
	frame := o.frameFor(at)
	if frame > o.played && frame-o.played > holdMax {
		o.dropped++
		return
	}
	span := uint64(len(samples) / speaker.Channels)
	if frame+span <= o.played {
		o.late++ // its frames have played: playing it now only leaves this behind for good
		return
	}
	if frame < o.played {
		samples = samples[(o.played-frame)*speaker.Channels:]
		frame = o.played
	}
	if len(o.pcm) == 0 {
		o.base = frame
	}
	if frame < o.base {
		o.pcm = append(make([]int16, (o.base-frame)*speaker.Channels), o.pcm...)
		o.base = frame
	}
	off := int(frame-o.base) * speaker.Channels
	if need := off + len(samples); need > len(o.pcm) {
		o.pcm = append(o.pcm, make([]int16, need-len(o.pcm))...)
	}
	copy(o.pcm[off:], samples)
}

// frameFor is the output frame a time falls on. Wants mu.
func (o *audioOut) frameFor(at time.Time) uint64 {
	if !o.anchored {
		// Frame Written() is going to the card now and is heard a hardware tail later.
		ahead := time.Until(at) - speaker.HardwareTail
		o.frame = o.p.Written() + uint64(max(0, ahead.Seconds()*speaker.Rate))
		o.at, o.anchored = at, true
		o.played = o.frame
		return o.frame
	}
	return uint64(int64(o.frame) + int64(at.Sub(o.at).Seconds()*speaker.Rate))
}

// Render is the speaker asking what to play next.
func (o *audioOut) Render(from uint64, buf []int16) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if from > o.played {
		o.played = from
	}
	if from > o.base && len(o.pcm) > 0 {
		if gone := int((from - o.base) * speaker.Channels); gone >= len(o.pcm) {
			o.pcm = o.pcm[:0]
		} else {
			o.pcm = append(o.pcm[:0], o.pcm[gone:]...)
		}
	}
	if from > o.base {
		o.base = from
	}
	if o.held || !o.ready || from < o.base {
		return
	}
	for i := range min(len(buf), len(o.pcm)) {
		buf[i] = scale(o.pcm[i], o.gain)
	}
}

// Suspend and Resume: whatever else wants the speaker (an answer, an alarm) takes it, and the cast
// rejoins where its clock says when it is let back.
func (o *audioOut) Suspend() { o.mu.Lock(); o.held = true; o.mu.Unlock() }
func (o *audioOut) Resume()  { o.mu.Lock(); o.held = false; o.mu.Unlock() }

// Duck quietens rather than pauses: a hole in the sound of a film is worse than a quiet stretch.
func (o *audioOut) Duck(on bool) {
	gain := float32(1)
	if on {
		gain = float32(math.Pow(10, float64(config.Get().Media.DuckDB)/20))
	}
	o.mu.Lock()
	o.gain = gain
	o.mu.Unlock()
}

// Requeue has nothing to do: the level is applied as frames are rendered.
func (o *audioOut) Requeue() {}

func (o *audioOut) misses() (late, dropped int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.late, o.dropped
}

func scale(s int16, gain float32) int16 {
	if gain == 1 {
		return s
	}
	return int16(max(math.MinInt16, min(float64(s)*float64(gain), math.MaxInt16)))
}
