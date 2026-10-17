//go:build spot

package display

// hasNightTap is false on the Spot: a touch on its night clock always lifts it (night_spot.go), so
// there is nothing to set.
const hasNightTap = false

// setNightTapLifts is the setting the Spot does not have.
func (d *Display) setNightTapLifts(bool) {}
