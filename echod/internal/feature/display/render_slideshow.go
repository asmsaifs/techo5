//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// slideshowWash is the theme's ground color, translucent, over a photo — the same technique the
// now-playing screen uses for cover art (render_nowplaying.go's background): the photo stays
// recognizable. Light, because readable.go darkens a bright photo further just where the words go.
const slideshowWash = 90

// slideshowBackground draws a Background-mode photo full-bleed, then the wash over it.
func (r *renderer) slideshowBackground(img *image.RGBA) {
	r.washed.lay(r.dst, img, color.RGBA{walnut.R, walnut.G, walnut.B, slideshowWash})
}

// artWeather is the rain, snow, storm or fog moving over the weather art, when it is the picture.
func (r *renderer) artWeather(s scene) {
	if s.artFx != fxNone {
		r.sky(s.artFx, s.now, r.dst.Rect, image.Rect(r.w/5, 0, r.w*4/5, r.h/2))
		r.artDrawn = true
	}
}

// slideshowScreensaverPage is Screensaver mode: the photo full-bleed, and — unless the overlay is
// off — the wash plus a clock, small (cornerClock) or normal size (the same layout bigClock uses,
// without its weather and timers, which belong to the ordinary idle page).
func (r *renderer) slideshowScreensaverPage(s scene) {
	if s.slideshowOverlay == config.SlideshowOverlayOff {
		draw.Draw(r.dst, r.dst.Rect, s.slideshowScreensaver, s.slideshowScreensaver.Bounds().Min, draw.Src)
		r.artWeather(s)
		return
	}
	// Washed as the background is, and kept for the second when it is the weather art (washedArt).
	r.washed.lay(r.dst, s.slideshowScreensaver, color.RGBA{walnut.R, walnut.G, walnut.B, slideshowWash})
	r.artWeather(s)
	r.readableOver(s.slideshowScreensaver, walnut, slideshowWash, func() {
		if s.slideshowOverlay == config.SlideshowOverlaySmall {
			r.cornerClock(s)
			return
		}
		r.timeAndDate(s.now, r.h/2+60, "")
	})
}
