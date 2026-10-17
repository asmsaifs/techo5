//go:build !dot && !spot

package touch

import "github.com/HuskerMinion/techo5/echod/internal/layout"

// The Echo Show 5: a Goodix GT9xx in the panel's portrait frame, 480 wide and 960 tall, while the
// device sits landscape. The Echo Show 8 is the same arrangement one size up: a FocalTech behind an
// 800 by 1280 portrait panel in a 1280 by 800 landscape device, five slots rather than ten, and the
// same protocol B with tracking ids.
//
// Landscape x runs along the panel's y, landscape y runs back along its x, on both. Checked on a
// Show 8 on 2026-09-22 by tapping its four corners: the top left of the screen reads (800, 0) in
// panel coordinates, the top right (800, 1280), the bottom right (0, 1280), the bottom left (0, 0).
// Those are what toFrame below already turns the right way round, so the two generations share it
// and only the sizes differ.
//
// The 2nd gen Show 8 (athena) has a FocalTech too (fts_ts, chip 0x5422, five slots, protocol B), but
// under Amazon's own kernel its driver is told the display is 1280 by 800 (focaltech,display-coords
// in the device tree) and says its axes run 0 to 1280 across and 0 to 800 down: already the landscape
// frame. toFrame tells the two apart by which axis is the longer, so a controller that reports
// landscape is scaled and not turned. Not yet checked with a finger on that unit: if its corners come
// out mirrored, toFrame is the place to say so.
//
// These are variables rather than constants because one build serves all four screens and the
// board is only known at run time; nothing outside this package reads them.
var (
	deviceName = onShow8("goodix-ts", "fts_ts")

	// Width and Height are the frame coordinates are reported in.
	Width  = onShow8(960, 1280)
	Height = onShow8(480, 800)

	// rawFallbackW and rawFallbackH stand in when the driver will not say what its axes run to.
	rawFallbackW = onShow8(480, 800)
	rawFallbackH = onShow8(960, 1280)

	// tapMove is how far a finger may wander and still be a tap, in frame pixels, so the Show 8's
	// larger screen gets the same allowance in proportion. Not yet re-measured on a Show 8: the
	// Spot's comment records that a still finger on that controller drifts, and a FocalTech may
	// differ from a Goodix.
	tapMove = onShow8(24, 32)

	// notch is the vertical travel per volume step, scaled the same way.
	notch = onShow8(40, 56)
)

const (
	// holdGestures: the Show's screen has no use for holds; its taps and swipes stay as they were.
	holdGestures = false
)

// onShow8 picks the Show 8's value over the Show 5's: both Show 8 generations against both Show 5
// generations.
func onShow8[T any](show5, show8 T) T {
	if layout.Show8() {
		return show8
	}
	return show5
}

// toFrame turns a reading into frame coordinates. A controller whose axes run portrait, the panel's
// own way, is turned a quarter; one that already reports landscape (athena's) is only scaled.
func toFrame(rawW, rawH, rx, ry int) (x, y int) {
	if rawW > rawH {
		x = rx * Width / max(rawW, 1)
		y = ry * Height / max(rawH, 1)
		return x, y
	}
	x = ry * Width / max(rawH, 1)
	y = (rawW - 1 - rx) * Height / max(rawW, 1)
	return x, y
}
