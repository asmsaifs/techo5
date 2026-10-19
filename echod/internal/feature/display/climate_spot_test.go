//go:build spot

package display

import (
	"image"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/feature/dashboard"
)

// On the Spot a tap on a thermostat's left third sets it a step cooler, on its right third a step
// warmer, and in the middle nothing; a thermostat keeping a range, or off, is not stepped.
func TestATapStepsAThermostatOnTheSpot(t *testing.T) {
	var set [][3]any
	prevNow, prevSet := climateNow, climateSet
	climateNow = func(entity string) (dashboard.ClimateNow, bool) {
		switch entity {
		case "climate.hall":
			return dashboard.ClimateNow{Entity: entity, State: "heat", Target: 70, HasTarget: true, Min: 45, Max: 95, Step: 1}, true
		case "climate.den":
			return dashboard.ClimateNow{Entity: entity, State: "heat_cool", Low: 20, High: 24, HasRange: true, Min: 7, Max: 35, Step: 0.5}, true
		}
		return dashboard.ClimateNow{}, false
	}
	climateSet = func(entity string, low, high float64) { set = append(set, [3]any{entity, low, high}) }
	t.Cleanup(func() { climateNow, climateSet = prevNow, prevSet })

	d := &Display{poke: make(chan struct{}, 1)}
	tile := &dashTile{r: image.Rect(100, 100, 400, 200), entity: "climate.hall"}
	d.climatePress(tile, 120) // left
	d.climatePress(tile, 250) // middle
	d.climatePress(tile, 390) // right
	d.climatePress(&dashTile{r: tile.r, entity: "climate.den"}, 390)
	want := [][3]any{{"climate.hall", 69.0, 0.0}, {"climate.hall", 71.0, 0.0}}
	if len(set) != len(want) || set[0] != want[0] || set[1] != want[1] {
		t.Errorf("set %v, want %v", set, want)
	}
}
