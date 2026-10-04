//go:build spot

package display

import (
	"log/slog"
	"sync/atomic"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The Spot's muted ring, full or subtle. The full ring is the rim's whole width in a bright red, which
// in a dark room lights the walls; subtle keeps the ring, so the state can still be seen at a glance,
// but thin and a dimmer red. Full until somebody chooses.

// mutedSoftWidth is the subtle ring's width in pixels, against the full ring's rimOut-rimIn.
const mutedSoftWidth = 5

// muteRingSubtle is the setting, read by each frame without the config's lock (see clock24).
var muteRingSubtle atomic.Bool

// muteRingSwitch is the Home Assistant switch for it; wake redraws the screen once it changes.
func muteRingSwitch(wake func()) *esphome.Switch {
	s := &esphome.Switch{
		Base: esphome.Base{
			ObjectID: "screen_mute_ring_subtle",
			Name:     "Subtle mute ring",
			Icon:     "mdi:microphone-off",
			Category: esphome.CategoryConfig,
		},
	}
	s.OnCommand = func(on bool) {
		if err := config.Set().Screen().MuteRingSubtle(on); err != nil {
			slog.Error("saving the mute ring setting failed", "err", err)
			return
		}
		setMuteRingSubtle(s, on)
		slog.Info("screen: subtle mute ring", "on", on)
		wake()
	}
	return s
}

func setMuteRingSubtle(s *esphome.Switch, on bool) {
	muteRingSubtle.Store(on)
	s.Set(on)
}
