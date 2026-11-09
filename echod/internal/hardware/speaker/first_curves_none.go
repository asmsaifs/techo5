//go:build !panel && !(dot && donut)

package speaker

// firstCurves is empty on this fork's Show, Dot and Spot: the volume is not put in front of the tuning
// (reverted), so it attenuates what the tuning produced. Louder top volume (louder_top.go) has no set to
// lift here and stays off. The panel and the Dot 2 say the same in their own files.
var firstCurves = map[string][VolumeSteps + 1]float64{}
