//go:build !dot

package dashboard

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/cast"
)

// The sound a deck server may send (the audio1 extension, see techo5-streamdeck/docs/protocol.md):
// stamped chunks of PCM and the server's clock, played on the speaker the way a cast is.
const (
	kindAudio = 4 // 8-byte stamp (µs), then S16LE 48 kHz stereo PCM
	kindClock = 5 // 8-byte stamp: the server's clock now
	kindSetup = 6 // JSON {"latency_ms": n}

	// capAudio is what the hello says to be sent kinds 4 to 6; a server never sends them without it.
	capAudio = "audio1"

	// audioLatency is how long after its stamp a chunk is heard until the server says otherwise, and
	// the bounds on what it may say: below the lower the network's jitter is heard, above the upper
	// the sound is long after the picture.
	audioLatency    = 300 * time.Millisecond
	audioLatencyMin = 100 * time.Millisecond
	audioLatencyMax = time.Second

	// audioIdle is how long the speaker is held with no audio arriving: the server stops sending
	// when the Show goes back to the deck, and nothing says so, so the device lets go by itself.
	// Clock messages do not count, or a page that is silent for a while would hold it.
	audioIdle = 3 * time.Second
)

// soundOut is what plays the stream's sound: cast.Playback on the device, a fake in tests.
type soundOut interface {
	Clock(us int64)
	Audio(us int64, pcm []byte)
	Close()
}

// newSound makes the output for a connection that has sound to play.
var newSound = func(latency time.Duration) soundOut { return cast.NewPlayback(latency) }

// sound is one connection's audio. The speaker is taken when audio arrives, not for a clock
// message: a page that is silent, or a deck, then never holds it. It is let go when no audio has
// come for audioIdle, and taken again by the next audio.
type sound struct {
	idle time.Duration

	mu      sync.Mutex
	latency time.Duration
	out     soundOut
	timer   *time.Timer

	// The last clock message, for an output made after it came: its stamp and when it arrived.
	clockUs int64
	clockAt time.Time
}

func newSoundState() *sound { return &sound{latency: audioLatency, idle: audioIdle} }

// message handles kinds 4 to 6; body is the message after its kind byte.
func (a *sound) message(kind byte, body []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch kind {
	case kindSetup:
		var m struct {
			LatencyMs int `json:"latency_ms"`
		}
		if json.Unmarshal(body, &m) != nil || m.LatencyMs <= 0 {
			return
		}
		a.latency = min(max(time.Duration(m.LatencyMs)*time.Millisecond, audioLatencyMin), audioLatencyMax)
	case kindClock:
		us, _, ok := cast.Split(body)
		if !ok {
			return
		}
		a.clockUs, a.clockAt = us, time.Now()
		if a.out != nil {
			a.out.Clock(us)
		}
	case kindAudio:
		us, pcm, ok := cast.Split(body)
		if !ok || a.clockAt.IsZero() {
			return // audio before a clock cannot be placed
		}
		if a.out == nil {
			a.out = newSound(a.latency)
			// The clock arrived before this output existed: tell it what it said, aged by the
			// time since, so the offset it works out is the one the arrival gave.
			a.out.Clock(a.clockUs + time.Since(a.clockAt).Microseconds())
		}
		a.out.Audio(us, pcm)
		a.arm()
	}
}

// arm starts the idle wait over. Wants mu.
func (a *sound) arm() {
	if a.timer == nil {
		a.timer = time.AfterFunc(a.idle, a.release)
		return
	}
	a.timer.Reset(a.idle)
}

// release lets the speaker go because no audio has come for a while.
func (a *sound) release() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drop()
}

// drop closes the output. Wants mu.
func (a *sound) drop() {
	if a.timer != nil {
		a.timer.Stop()
	}
	if a.out != nil {
		a.out.Close()
		a.out = nil
	}
}

// close lets the speaker go; the connection has ended.
func (a *sound) close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drop()
}
