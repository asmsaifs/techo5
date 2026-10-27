//go:build !dot && !spot

package display

import (
	"log/slog"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/presence"
)

// idleDimTick dims the screen once it has been idle for Dim after idle, and brings it back up at the
// first touch, turn or person near, or when the screen is turned on or its brightness set (apply). busy is anything going on that keeps it lit (a turn, a call, a ring,
// the settings open); it counts as use, so the wait starts again when it ends.
func (d *Display) idleDimTick(now time.Time, on, busy, night bool) {
	after := idleDimAfter()
	d.mu.Lock()
	if busy || presence.Present() || d.usedAt.IsZero() {
		d.usedAt = now
	}
	last := d.usedAt
	if d.touchedAt.After(last) {
		last = d.touchedAt
	}
	was := d.idleDimmed
	// The night starts no dimming, and ends none either: a screen already down stays down into it
	// rather than jumping up to its brightness as the night begins.
	dim := on && after > 0 && now.Sub(last) >= after && (!night || was)
	d.idleDimmed = dim
	d.mu.Unlock()
	if dim == was {
		return
	}
	if dim {
		slog.Info("screen: idle, dimmed", "after", after)
	} else {
		slog.Info("screen: in use, undimmed")
	}
	// Down slowly, so it is seen to happen and not taken for a fault; back up at once.
	d.relight(!dim)
}

// idleDimLevel is the backlight a dimmed screen gets: its Dimmest share of the brightness, never more
// than it would have anyway.
func idleDimLevel(target float64, ceiling int) float64 {
	return min(target, dayBacklight(ceiling, true, 0, dimmest()))
}

// setIdleDim saves the choice at i and shows it in Home Assistant.
func (d *Display) setIdleDim(i int) {
	if i < 0 || i >= len(idleDims) {
		return
	}
	if err := config.Set().Screen().DimAfterMinutes(idleDims[i].minutes); err != nil {
		slog.Error("saving dim after idle failed", "err", err)
		return
	}
	if d.idleDimSel != nil {
		d.idleDimSel.Set(idleDims[i].label)
	}
	slog.Info("setting changed", "setting", "screen_dim_after_idle", "using", idleDims[i].label)
	d.wake()
}

// idleDimSelect is the Home Assistant setting.
func idleDimSelect(d *Display) *esphome.Select {
	s := &esphome.Select{
		Base: esphome.Base{
			ObjectID: "screen_dim_after_idle",
			Name:     "Dim after idle",
			Icon:     "mdi:brightness-auto",
			Category: esphome.CategoryConfig,
		},
		Options: idleDimOptions(),
	}
	s.OnCommand = func(v string) {
		for i, t := range idleDims {
			if t.label == v {
				d.setIdleDim(i)
				return
			}
		}
	}
	return s
}
