//go:build !dot

package dashboard

import (
	"encoding/json"
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
)

// soundOut is what plays the stream's sound: cast.Playback on the device, a fake in tests.
type soundOut interface {
	Clock(us int64)
	Audio(us int64, pcm []byte)
	Close()
}

// newSound makes the output for a connection that has sound to play.
var newSound = func(latency time.Duration) soundOut { return cast.NewPlayback(latency) }

// sound is one connection's audio. The speaker is taken at the first clock message, which the
// server sends only when a source with sound is shown, so a silent deck never holds it.
type sound struct {
	latency time.Duration
	out     soundOut
}

func newSoundState() *sound { return &sound{latency: audioLatency} }

// message handles kinds 4 to 6; body is the message after its kind byte.
func (a *sound) message(kind byte, body []byte) {
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
		if a.out == nil {
			a.out = newSound(a.latency)
		}
		a.out.Clock(us)
	case kindAudio:
		us, pcm, ok := cast.Split(body)
		if !ok || a.out == nil {
			return // audio before a clock cannot be placed
		}
		a.out.Audio(us, pcm)
	}
}

// close lets the speaker go; the connection has ended or the server has gone quiet.
func (a *sound) close() {
	if a.out != nil {
		a.out.Close()
		a.out = nil
	}
}
