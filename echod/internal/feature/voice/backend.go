package voice

import (
	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// backend is what a turn runs against: Home Assistant's Assist pipeline over the ESPHome connection,
// or the device's own direct pipeline (direct.go). Either reports back through the conversation's
// events - heard, reply text, reply audio, run end, error - so everything else about a turn, the
// listening, the ducking, the ring and the screen, is the same whichever answers it.
//
// Its methods are called in order from the conversation's send queue, one at a time.
type backend interface {
	// Ready is whether a turn can start now.
	Ready() bool
	// Start opens a turn; phrase is the wake word that opened it.
	Start(phrase string) error
	// Audio is one frame of the microphone: 16 kHz, 16-bit little-endian, mono.
	Audio(frame []byte) error
	// End says the speaker has finished.
	End() error
	// Stop closes the turn, whatever it was doing.
	Stop() error
	// Name is for the log.
	Name() string
}

// ha is Home Assistant's pipeline. watch opens a run at the wake word stage instead (hawake.go), and
// ready stands in for the satellite's subscription where a test has none, as start does for the
// request that opens a turn.
type ha struct {
	vs    *esphome.VoiceSatellite
	watch func(phrase string) error
	ready func() bool
	start func(phrase string) error
}

func (h ha) Ready() bool {
	if h.ready != nil {
		return h.ready()
	}
	return h.vs.Subscribed()
}

func (h ha) Start(phrase string) error {
	if h.start != nil {
		return h.start(phrase)
	}
	return h.vs.StartTurn(phrase, audioSettings())
}

func (h ha) Audio(frame []byte) error { return h.vs.SendAudio(frame) }
func (h ha) End() error               { return h.vs.EndAudio() }
func (h ha) Stop() error              { return h.vs.StopTurn() }
func (h ha) Name() string             { return "home assistant" }

// backendFor is which one a new turn runs against: the direct pipeline when it is chosen and set up,
// Home Assistant otherwise.
func (c *conversation) backendFor(slot int) backend {
	// A slot chosen for realtime voice is answered by it, whatever the brain: not ready (not set up, or
	// the day's minutes used), the turn says so rather than going to another assistant unasked.
	if w := config.Get().Wake.Words; slot >= 0 && slot < len(w) && w[slot].Realtime {
		return c.realtime
	}
	if config.Get().Brain.Direct() {
		return c.direct
	}
	return c.ha
}

// onHA is whether the turn is Home Assistant's pipeline's.
func (c *conversation) onHA() bool {
	_, on := c.be.(ha)
	return on
}

// stopsAtOnce is whether the turn's backend stops the moment it is told, with nothing more to come from
// it: the device's own, not Home Assistant's run.
func (c *conversation) stopsAtOnce() bool {
	switch c.be.(type) {
	case *direct, *rtBackend:
		return true
	}
	return false
}
