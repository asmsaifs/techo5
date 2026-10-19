//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"image/draw"
)

// colStreaming is the dot that says the camera is being streamed: the Spot's camera-on green.
var colStreaming = color.RGBA{60, 203, 127, 255}

// streamingDot is drawn in the top right corner, over everything, while camera streaming (feature/camstream)
// is sending the camera to somebody: a recorder can watch all day, and the room should be able to tell.
func (r *renderer) streamingDot(s scene) {
	if !s.streaming {
		return
	}
	x, y := r.w-r.s(20), r.s(20)
	r.disc(x, y, r.s(10), color.RGBA{0, 0, 0, 160})
	r.disc(x, y, r.s(7), colStreaming)
}

// watchedDark is the frame of a dark panel while the camera is streamed: black, and the dot.
func (r *renderer) watchedDark() {
	draw.Draw(r.dst, r.dst.Rect, image.NewUniform(color.Black), image.Point{}, draw.Src)
	r.streamingDot(scene{streaming: true})
}
