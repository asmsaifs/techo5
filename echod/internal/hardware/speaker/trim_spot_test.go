//go:build spot

package speaker

// untunedTrim undoes the 6 dB the Spot's speaker curve is turned down by for playing untuned
// (paths_spot.go), so the curves in front of the tuning are checked against the vendor curve itself.
const untunedTrim = 1.9952623 // +6 dB
