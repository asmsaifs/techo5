//go:build !dot && !spot

package display

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

// A wash is a valid premultiplied color whatever the ground, light themes' included, and lays down
// what the ground at that opacity would, to within a step of rounding: no channel above its alpha, so
// nothing overflows.
func TestAWashIsTheGroundAtItsOpacity(t *testing.T) {
	grounds := []color.RGBA{{0xff, 0xff, 0xff, 0xff}, {0, 0, 0, 0xff}, {0xff, 0, 0x80, 0xff}}
	for _, th := range themes {
		grounds = append(grounds, th.colors[roleGround])
	}
	unders := []color.RGBA{{0, 0, 0, 0xff}, {0xff, 0xff, 0xff, 0xff}, {0x30, 0x90, 0xe0, 0xff}}
	near := func(a, b color.RGBA) bool {
		step := func(x, y uint8) bool { return x-y <= 1 || y-x <= 1 }
		return step(a.R, b.R) && step(a.G, b.G) && step(a.B, b.B) && a.A == b.A
	}
	for _, g := range grounds {
		for _, alpha := range []uint8{0, 1, slideshowWash, 120, 150, 254, 255} {
			w := washOf(g, alpha)
			if w.A != alpha || w.R > alpha || w.G > alpha || w.B > alpha {
				t.Errorf("washOf(%v, %d) = %v: a channel above its alpha", g, alpha, w)
			}
			for _, u := range unders {
				got, want := image.NewRGBA(image.Rect(0, 0, 1, 1)), image.NewRGBA(image.Rect(0, 0, 1, 1))
				got.SetRGBA(0, 0, u)
				want.SetRGBA(0, 0, u)
				draw.Draw(got, got.Rect, image.NewUniform(w), image.Point{}, draw.Over)
				draw.Draw(want, want.Rect, image.NewUniform(color.NRGBA{g.R, g.G, g.B, alpha}), image.Point{}, draw.Over)
				if !near(got.RGBAAt(0, 0), want.RGBAAt(0, 0)) {
					t.Errorf("washOf(%v, %d) over %v = %v, the ground at that opacity is %v", g, alpha, u, got.RGBAAt(0, 0), want.RGBAAt(0, 0))
				}
			}
		}
	}
}

// A light theme is turned over only while a photo is drawn: the palette is its own again once the
// frame is done, and once a page drawn over a photo panics, too.
func TestALightThemeOverAPhotoLeavesThePaletteAsItWas(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	keepPalette(t)
	at := time.Date(2026, 9, 16, 14, 7, 0, 0, time.Local)
	caps := []home.PhotoCaption{{When: at.AddDate(-3, 0, 0), Place: "Somewhere"}}
	for _, name := range []string{"Paper", "Linen"} {
		th := themes[themeIndex(name)]
		applyTheme(th)
		if dark() {
			t.Fatalf("%s is not a light theme", name)
		}
		photo := gradientPair(showWide, showHigh)
		for _, s := range []scene{
			{now: at, phase: "idle", weather: home.Weather{Condition: "sunny", Temp: "72°"}, slideshow: photo},
			{now: at, phase: "idle", slideshowScreensaver: photo, slideshowCaptions: caps},
			{now: at, phase: "idle", slideshowScreensaver: photo, slideshowOverlay: config.SlideshowOverlayOff, slideshowCaptions: caps},
		} {
			newRenderer(image.NewRGBA(image.Rect(0, 0, showWide, showHigh))).draw(s)
			if got := [roles]color.RGBA{walnut, amber, cream, dim, ember}; got != th.colors {
				t.Errorf("%s: the palette after a frame over a photo is %v, not the theme's %v", name, got, th.colors)
			}
		}

		turned := false
		func() {
			defer func() { _ = recover() }()
			onPhoto(func() {
				turned = dark()
				panic("a page that fails")
			})
		}()
		if !turned {
			t.Errorf("%s: over a photo the ground is still light", name)
		}
		if got := [roles]color.RGBA{walnut, amber, cream, dim, ember}; got != th.colors {
			t.Errorf("%s: a panic over a photo left the palette %v, not the theme's %v", name, got, th.colors)
		}
	}
}

// The settings screen's kept ground, rail and heading are drawn again in a font chosen while it is
// open, not kept in the old one until the category changes.
func TestTheSettingsScreenTakesANewFontWhileOpen(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	sheet := scene{now: time.Date(2026, 9, 16, 14, 7, 0, 0, time.Local), phase: "idle", showSheet: true,
		sheet: settings{cat: catDisplay, brightness: 70}}
	for _, size := range []image.Point{{showWide, showHigh}, {show8Wide, show8High}} {
		_ = config.Set().Screen().Font("")
		img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
		r := newRenderer(img)
		r.draw(sheet)
		if err := config.Set().Screen().Font("saira"); err != nil {
			t.Fatal(err)
		}
		r.draw(sheet)
		fresh := image.NewRGBA(img.Rect)
		newRenderer(fresh).draw(sheet)
		if !bytes.Equal(img.Pix, fresh.Pix) {
			t.Errorf("%v: the open settings screen differs from one opened in the new font", size)
		}
	}
	_ = config.Set().Screen().Font("")
}
