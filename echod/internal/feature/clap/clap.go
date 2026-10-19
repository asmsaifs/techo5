// Package clap listens for a double clap and tells Home Assistant: an automation turns a light on, or stops
// something, on two claps in the room, the way the palm over the camera already does on a Show
// (feature/presence). It works on every device, the Dot included, since it is the microphones that hear it.
//
// Off unless somebody turns it on. Nothing is kept and nothing is sent but the event: the microphones are
// read on the device, as the wake word engine already reads them. It does not listen while the device makes
// a sound of its own, is in a request or a call, or plays music, since then what it hears is mostly that.
//
// It hears one microphone as captured, before the echo canceler, the noise reduction and the leveler: the
// leveler turns a sudden loud sound down in the frame it arrives in, which is exactly the jump a clap is.
package clap

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/ring"
	"github.com/HuskerMinion/techo5/echod/internal/feature/talkback"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

func init() {
	component.Register(component.Device, Get(), component.Order(7))
}

// GestureEvent is the event Home Assistant gets, the same as the palm over the camera's (presence), so one
// automation can take either: gesture "double_clap", and the device's name.
const GestureEvent = "esphome.techo5_gesture"

// playingLevel is the speaker loopback's RMS above which the device is taken to be making a sound: the
// chimes and beeps the speaker plays without a claim (volume, mute, doorbell, feedback) are not seen by
// busy, but they are in the loopback captured with the microphones. Silence there is a few counts.
const playingLevel = 64

// busyEvery is how often, in blocks, the device is asked whether it is busy with a sound of its own, and
// busyAfter how long it is still taken for busy once it says it is not: the speaker goes on sounding for a
// moment after its queue runs out, and the room rings after that.
const (
	busyEvery = 10
	busyAfter = 250 * time.Millisecond
)

// Clap is the switch and the listening behind it.
type Clap struct {
	on *esphome.Switch

	// busy and heard are the device; replaced in tests.
	busy  func() bool
	heard func()

	mu      sync.Mutex
	enabled bool
	wake    chan struct{}

	// played is when the loopback last carried a sound (monoOf), in UnixNano.
	played atomic.Int64
}

var (
	once   sync.Once
	shared *Clap
)

// Get is the one.
func Get() *Clap {
	once.Do(func() { shared = build() })
	return shared
}

func build() *Clap {
	c := &Clap{
		on: &esphome.Switch{Base: esphome.Base{
			ObjectID: "clap_gesture", Name: "Clap gesture", Icon: "mdi:hand-clap",
			Category: esphome.CategoryConfig, DeviceID: component.DeviceMicrophone,
		}},
		busy:  busy,
		heard: fire,
		wake:  make(chan struct{}, 1),
	}
	c.on.OnCommand = func(on bool) {
		c.set(on)
		if err := config.Set().Microphone().Claps(on); err != nil {
			slog.Error("saving the clap gesture failed", "err", err)
		}
	}
	return c
}

func (c *Clap) Name() string { return "clap gesture" }

func (c *Clap) Entities() []esphome.Entity { return []esphome.Entity{c.on} }

func (c *Clap) Restore(cfg config.Config) {
	c.set(cfg.Microphone.Claps)
	slog.Info("restored", "what", c.on.ObjectID, "using", cfg.Microphone.Claps)
}

func (c *Clap) set(on bool) {
	c.on.Set(on)
	c.mu.Lock()
	c.enabled = on
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Clap) isOn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// busy is whether the device is making or taking a sound that is not the room.
func busy() bool {
	playing, _ := media.Get().Playing()
	return voice.Get().Busy() || phone.Get().Busy() || talkback.Get().Busy() || ring.IsSounding() ||
		speaker.Sound().Busy() || playing || media.Get().ExternalPlaying()
}

// fire tells Home Assistant.
func fire() {
	slog.Info("clap gesture: a double clap")
	component.Fire.Emit(component.Event{Name: GestureEvent, Data: map[string]string{
		"gesture": "double_clap",
		"device":  config.Get().Device.Name,
	}})
}

// Run listens while the switch is on.
func (c *Clap) Run(ctx context.Context) error {
	for {
		if !c.isOn() {
			select {
			case <-ctx.Done():
				return nil
			case <-c.wake:
			}
			continue
		}
		raw, unlisten := mic.Get().ListenRaw()
		c.listen(ctx, c.monoOf(raw))
		unlisten()
		if ctx.Err() != nil {
			return nil
		}
	}
}

// monoOf is one microphone's samples, as captured, from the raw capture, noting when the speaker's
// loopback in the same capture carries a sound.
func (c *Clap) monoOf(raw <-chan []byte) <-chan []int16 {
	out := make(chan []int16, 8)
	go func() {
		defer close(out)
		for b := range raw {
			if sounding(mic.Reference(b)) {
				c.played.Store(time.Now().UnixNano())
			}
			select {
			case out <- mic.Mono(b):
			default: // behind: a frame lost is a clap perhaps missed, not the capture held up
			}
		}
	}()
	return out
}

// sounding is whether any loopback channel is above playingLevel.
func sounding(refs [][]int16) bool {
	for _, ch := range refs {
		if len(ch) == 0 {
			continue
		}
		var sum float64
		for _, v := range ch {
			sum += float64(v) * float64(v)
		}
		if math.Sqrt(sum/float64(len(ch))) > playingLevel {
			return true
		}
	}
	return false
}

// listen takes the microphones' frames a block at a time until the switch goes off or ctx ends.
func (c *Clap) listen(ctx context.Context, frames <-chan []int16) {
	d := newDetector()
	pending := make([]int16, 0, block*4)
	n := 0
	var quietFrom time.Time // when the device last said it was busy, plus busyAfter
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
			if !c.isOn() {
				return
			}
		case f, ok := <-frames:
			if !ok {
				return
			}
			pending = append(pending, f...)
			used := 0
			for len(pending)-used >= block {
				b := pending[used : used+block]
				used += block
				if n%busyEvery == 0 && c.busy() {
					quietFrom = time.Now().Add(busyAfter)
				}
				if p := c.played.Load(); p != 0 {
					if after := time.Unix(0, p).Add(busyAfter); after.After(quietFrom) {
						quietFrom = after
					}
				}
				n++
				if time.Now().Before(quietFrom) {
					d.reset()
					continue
				}
				if d.add(b) && c.isOn() {
					c.heard()
				}
			}
			pending = pending[:copy(pending, pending[used:])]
		}
	}
}
