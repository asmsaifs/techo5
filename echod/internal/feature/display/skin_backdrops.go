//go:build !dot && !spot

package display

import (
	"image"
	"math"
)

// What a skin draws behind the home clock (skinLooks): Synthwave's sunset over a
// grid, HAL 9000's eye. Each is drawn on the ground before the clock, never over a photo, and changes
// at most once a minute, so it costs one drawing a frame and no frames of its own.

// synthwaveSun is the neon sunset: a sun from yellow at its top to pink at the horizon, cut by bands
// in its lower half, sunk behind a grid of cyan lines running to a point on the horizon.
func (r *renderer) synthwaveSun(s scene) {
	if s.slideshow != nil {
		return
	}
	horizon := r.h * 64 / 100
	// The sun only behind a clock in the middle: beside a dial or a list it would sit under the words.
	switch clockStyle() {
	case styleClassic, styleBig, styleFlip, styleLED, styleGlow, styleNixie, styleStack, styleOverlap:
	default:
		r.synthwaveGrid(horizon)
		return
	}
	rad := float64(r.h) * 0.30
	cx, cy := float64(r.w)/2, float64(horizon)-rad*0.25
	top, bottom := rgb(0xffd319), rgb(0xff2975)
	for y := int(cy - rad); y < horizon; y++ {
		dy := float64(y) + 0.5 - cy
		if math.Abs(dy) > rad {
			continue
		}
		t := (dy + rad) / (2 * rad)
		// The bands: thicker toward the horizon.
		if t > 0.5 {
			band := int((t - 0.5) * 22)
			if math.Mod((t-0.5)*22, 1) < 0.18+0.06*float64(band) {
				continue
			}
		}
		half := math.Sqrt(rad*rad - dy*dy)
		c := lerpRGB(top, bottom, t)
		for x := int(cx - half); x <= int(cx+half); x++ {
			edge := math.Min(float64(x)-(cx-half), cx+half-float64(x))
			r.blendAt(x, y, c, 0.78*math.Min(edge, 1))
		}
	}
	r.synthwaveGrid(horizon)
}

// synthwaveGrid is the horizon, and the grid below it: rows closer together as they go away, lines
// running in to the middle of the horizon.
func (r *renderer) synthwaveGrid(horizon int) {
	cyan, pink := rgb(0x05d9e8), rgb(0xff2a6d)
	for x := 0; x < r.w; x++ {
		r.blendAt(x, horizon, pink, 0.8)
		r.blendAt(x, horizon+1, pink, 0.4)
	}
	depth := float64(r.h - horizon)
	for i := 1; i < 12; i++ {
		y := horizon + int(depth*math.Pow(float64(i)/11, 2))
		for x := 0; x < r.w; x++ {
			r.blendAt(x, y, cyan, 0.35)
		}
	}
	vx := float64(r.w) / 2
	for i := -12; i <= 12; i++ {
		foot := vx + float64(i)*float64(r.w)/9
		for y := horizon + 1; y < r.h; y++ {
			k := float64(y-horizon) / depth
			x := vx + (foot-vx)*k
			r.blendAt(int(x), y, cyan, 0.35)
		}
	}
}

// halBackdrop is the computer's eye at the top right (halEye, turn_visual.go, the HAL 9000 turn
// screen's), its glow as it is between words.
func (r *renderer) halBackdrop(s scene) {
	b := r.halEyeAt(s)
	if b.Empty() {
		return
	}
	rad := float64(b.Dx()) / 2
	halEye(r.dst, float64(b.Min.X)+rad, float64(b.Min.Y)+rad, rad, 0.55)
}

// halEyeAt is where the HAL 9000 skin's eye goes on the home clock, or empty where there is none: not
// over a photo, not while an alert's badge is there, and not behind the Overlap style, whose figures
// reach up into its corner. The Corner timer's badge goes under it (timerCorner).
func (r *renderer) halEyeAt(s scene) image.Rectangle {
	if skin() != skinHAL || s.slideshow != nil || len(s.alerts.Here) > 0 || s.style.style() == styleOverlap {
		return image.Rectangle{}
	}
	d, right := 2*r.s(44), r.w-r.margin
	return image.Rect(right-d, r.s(20), right, r.s(20)+d)
}
