//go:build !dot && !spot

package display

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"sync"

	"github.com/HuskerMinion/techo5/echod/internal/lib/picscale"
)

// What the readable skins draw behind the home clock (skinBackdrops): each faint, at the edges or low on
// the screen, so the time and the words over it keep their contrast. None over a photo.

// The photo skins' pictures behind the home clock (skinphotos/, public domain): Prairie's, the Sandhills
// at Crescent Lake National Wildlife Refuge at sunrise (U.S. Fish and Wildlife Service, Mountain-Prairie
// region); Earthrise's, the Earth over the Moon's horizon from Apollo 8 (NASA, Bill Anders).
//
//go:embed skinphotos/prairie.jpg
var prairieJPEG []byte

//go:embed skinphotos/earthrise.jpg
var earthriseJPEG []byte

func (r *renderer) prairiePhoto(s scene) {
	r.skinPhoto("prairie", prairieJPEG, func(float64) float64 { return 0.42 }, s)
}

// Earthrise is black sky above, which keeps most of its light so the Earth stays blue, and the Moon's
// gray ground below, under the date and the timers, which is taken down hard.
// Behind the Dashboard style the Earth would sit under the calendar's Next column, so there it is drawn
// alone on black, smaller, in the gap under the date (earthriseAside).
func (r *renderer) earthrisePhoto(s scene) {
	if s.style.style() == styleDashboard {
		r.earthriseAside(s)
		return
	}
	r.skinPhoto("earthrise", earthriseJPEG, func(t float64) float64 {
		return 0.75 - 0.45*min(max((t-0.4)/0.3, 0), 1)
	}, s)
}

// earthriseEarth is where the Earth is in skinphotos/earthrise.jpg (1280 by 800), with black sky round it.
var earthriseEarth = image.Rect(610, 95, 840, 265)

// earthriseAside is the Earth alone, at three quarters of its light, left of the Dashboard's divider and
// between the date and the forecast: lower and smaller on the Show 5's wide panel, where that gap is
// narrow.
func (r *renderer) earthriseAside(s scene) {
	if s.slideshow != nil {
		return
	}
	tall, cy := 0.22, 0.52
	if r.w*10 >= r.h*19 {
		tall, cy = 0.13, 0.51
	}
	img := skinPicture(fmt.Sprintf("earthrise aside %dx%d", r.w, r.h), func() *image.RGBA {
		src, err := jpeg.Decode(bytes.NewReader(earthriseJPEG))
		if err != nil {
			return nil
		}
		h := int(tall * float64(r.h))
		w := h * earthriseEarth.Dx() / earthriseEarth.Dy()
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		picscale.CatmullRom.Scale(out, out.Rect, src, earthriseEarth, draw.Src)
		shadeRows(out, func(float64) float64 { return 0.75 })
		return out
	})
	draw.Draw(r.dst, image.Rect(0, 0, r.w, r.h), image.Black, image.Point{}, draw.Src)
	if img == nil {
		return
	}
	at := image.Pt(r.w*24/100-img.Rect.Dx()/2, int(cy*float64(r.h))-img.Rect.Dy()/2)
	draw.Draw(r.dst, img.Rect.Add(at), img, image.Point{}, draw.Src)
}

// skinPictures are the pictures the photo skins draw, scaled and shaded for the panel, by what and what
// size, so a frame only copies one.
var skinPictures struct {
	sync.Mutex
	made map[string]*image.RGBA
}

// skinPicture is the picture named key, made once by make; nil if it could not be made.
func skinPicture(key string, make func() *image.RGBA) *image.RGBA {
	skinPictures.Lock()
	defer skinPictures.Unlock()
	if img, ok := skinPictures.made[key]; ok {
		return img
	}
	if skinPictures.made == nil {
		skinPictures.made = map[string]*image.RGBA{}
	}
	img := make()
	skinPictures.made[key] = img
	return img
}

// shadeRows keeps shade(t) of each row's light, t from 0 at the top to 1 at the bottom.
func shadeRows(img *image.RGBA, shade func(t float64) float64) {
	h, w := img.Rect.Dy(), img.Rect.Dx()
	for y := 0; y < h; y++ {
		k := shade(float64(y) / float64(h))
		row := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for i := 0; i < len(row); i += 4 {
			for c := 0; c < 3; c++ {
				row[i+c] = uint8(float64(row[i+c]) * k)
			}
		}
	}
}

// skinPhoto fills the screen with a skin's picture, cropped to cover it and shaded (shade, as for
// shadeRows: dark enough that the words over it, even over the sun, keep their contrast); not over a
// photo of the slideshow's.
func (r *renderer) skinPhoto(name string, file []byte, shade func(t float64) float64, s scene) {
	if s.slideshow != nil {
		return
	}
	img := skinPicture(fmt.Sprintf("%s %dx%d", name, r.w, r.h), func() *image.RGBA {
		src, err := jpeg.Decode(bytes.NewReader(file))
		if err != nil {
			return nil
		}
		img := image.NewRGBA(image.Rect(0, 0, r.w, r.h))
		picscale.CatmullRom.Scale(img, img.Rect, src, cover(src.Bounds(), img.Rect), draw.Src)
		shadeRows(img, shade)
		return img
	})
	if img != nil {
		draw.Draw(r.dst, image.Rect(0, 0, r.w, r.h), img, image.Point{}, draw.Src)
	}
}

// controlFrame is a console's thin frame round the screen, with ticks at its corners and three status
// lights at the top left, green, amber and red, only the green lit.
func (r *renderer) controlFrame(s scene) {
	if s.slideshow != nil {
		return
	}
	line := rgb(0x3b434d)
	in := r.s(8)
	for x := in; x < r.w-in; x++ {
		r.blendAt(x, in, line, 1)
		r.blendAt(x, r.h-in, line, 1)
	}
	for y := in; y < r.h-in; y++ {
		r.blendAt(in, y, line, 1)
		r.blendAt(r.w-in, y, line, 1)
	}
	tick := rgb(0x8a95a3)
	n := r.s(18)
	for _, c := range [][2]int{{in, in}, {r.w - in, in}, {in, r.h - in}, {r.w - in, r.h - in}} {
		dx, dy := 1, 1
		if c[0] > r.w/2 {
			dx = -1
		}
		if c[1] > r.h/2 {
			dy = -1
		}
		for i := 0; i < n; i++ {
			for t := 0; t < r.s(2); t++ {
				r.blendAt(c[0]+dx*i, c[1]+dy*t, tick, 1)
				r.blendAt(c[0]+dx*t, c[1]+dy*i, tick, 1)
			}
		}
	}
	y := in + r.s(14)
	for i, col := range []uint32{0x52e07c, 0xffb020, 0xff4d4d} {
		c := rgb(col)
		x := in + r.s(16) + i*r.s(16)
		if i == 0 {
			r.disc(x, y, r.s(4), c)
		} else {
			r.disc(x, y, r.s(4), lerpRGB(rgb(0x111418), c, 0.3))
		}
	}
}
