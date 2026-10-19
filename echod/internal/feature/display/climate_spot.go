//go:build spot

package display

import (
	"log/slog"

	"github.com/HuskerMinion/techo5/echod/internal/feature/dashboard"
)

// The Spot has no thermostat sheet: its round screen has no room for one. A tap on the left third of
// a thermostat's tile, whose card sets no tap of its own, sets it a step cooler, and on the right third
// a step warmer; the middle third does nothing, so a tap meant for neither changes nothing.

// climateSet and climateNow are how a step reaches Home Assistant and what it steps from; variables
// so that a test can see what a tap asks for.
var (
	climateSet = func(entity string, low, high float64) { dashboard.Get().SetClimate(entity, low, high) }
	climateNow = func(entity string) (dashboard.ClimateNow, bool) { return dashboard.Get().ClimateNow(entity) }
)

// climatePress is a tap at x on a thermostat's tile whose card sets no tap of its own.
func (d *Display) climatePress(t *dashTile, x int) {
	third := t.r.Dx() / 3
	n := 0
	switch {
	case x < t.r.Min.X+third:
		n = -1
	case x >= t.r.Max.X-third:
		n = 1
	default:
		return
	}
	c, ok := climateNow(t.entity)
	if !ok {
		return
	}
	if !c.HasTarget {
		// Off, or keeping a range: there is no one temperature to step.
		slog.Info("dashboard thermostat: nothing to step", "entity", t.entity, "state", c.State, "range", c.HasRange)
		return
	}
	v := c.Stepped(c.Target, n)
	slog.Info("dashboard thermostat", "entity", t.entity, "to", v)
	climateSet(t.entity, v, 0)
}
