//go:build !dot && !spot

package display

import (
	"log/slog"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// A tap lifts the night light: off by default, since a hand in the dark is more likely a knock than a
// request. On, a tap on the night light brings the screen back as by day, as a long press does,
// without reaching what is under the finger; the night light comes back once the room has been quiet
// for nightIdle (night). The Spot has no setting: a touch there always lifts its night clock.

// hasNightTap is whether the setting is offered.
const hasNightTap = true

// nightTapLifts is whether a tap on the night light lifts it.
func nightTapLifts() bool { return config.Get().Screen.NightTapLifts }

// nightTapSwitch is the Home Assistant switch for the setting.
func nightTapSwitch(d *Display) *esphome.Switch {
	s := &esphome.Switch{
		Base: esphome.Base{
			ObjectID: "screen_night_tap_lifts",
			Name:     "Tap lifts the night light",
			Icon:     "mdi:gesture-tap",
			Category: esphome.CategoryConfig,
		},
	}
	s.OnCommand = d.setNightTapLifts
	return s
}

// setNightTapLifts saves the setting and tells Home Assistant.
func (d *Display) setNightTapLifts(on bool) {
	if err := config.Set().Screen().NightTapLifts(on); err != nil {
		slog.Error("saving a setting failed", "setting", "screen_night_tap_lifts", "err", err)
		on = nightTapLifts()
	} else {
		slog.Info("setting changed", "setting", "screen_night_tap_lifts", "using", on)
	}
	if d.nightTapSw != nil {
		d.nightTapSw.Set(on)
	}
	d.wake()
}
