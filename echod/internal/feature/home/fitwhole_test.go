package home

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Rect, image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

// A see-through PNG comes out opaque either way: crossfade blends bytes and counts on it.
func TestPhotosComeOutOpaque(t *testing.T) {
	src := solid(300, 600, color.RGBA{}) // all see-through
	for name, img := range map[string]*image.RGBA{"whole": fitWhole(src, 960, 480), "filled": cropToFill(src, 960, 480)} {
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] != 255 {
				t.Errorf("%s: pixel %d has alpha %d", name, i/4, img.Pix[i])
				break
			}
		}
	}
}

// A tall photo on a wide screen shows whole, in the middle, with its own colors darkened down the
// sides, not black bars and not cropped.
func TestWholePhotoKeepsATallPhotoWhole(t *testing.T) {
	if roundPanel {
		t.Skip("a round screen fits the circle instead: fitwhole_spot_test.go")
	}
	red := color.RGBA{R: 200, G: 40, B: 20, A: 255}
	src := solid(300, 600, red)
	draw.Draw(src, image.Rect(0, 0, 300, 1), image.NewUniform(color.RGBA{G: 255, A: 255}), image.Point{}, draw.Src)     // the top row, which a crop to fill would cut
	draw.Draw(src, image.Rect(0, 599, 300, 600), image.NewUniform(color.RGBA{B: 255, A: 255}), image.Point{}, draw.Src) // and the bottom

	got := fitWhole(src, 1000, 500)
	if got.Bounds() != image.Rect(0, 0, 1000, 500) {
		t.Fatalf("got %v, want the screen's size", got.Bounds())
	}
	if c := got.RGBAAt(500, 250); c != red {
		t.Errorf("the middle is %v, want the photo's %v", c, red)
	}
	if c := got.RGBAAt(500, 0); c.G < 100 {
		t.Errorf("the photo's top row is gone: %v", c)
	}
	if c := got.RGBAAt(500, 499); c.B < 100 {
		t.Errorf("the photo's bottom row is gone: %v", c)
	}
	for _, x := range []int{0, 50, 949, 999} {
		c := got.RGBAAt(x, 250)
		want := byte(int(red.R) * wholeShade / 256)
		if c.A != 255 || c.R < want-4 || c.R > want+4 || c.G >= c.R {
			t.Errorf("side at x=%d is %v, want the photo's red darkened to about %d", x, c, want)
		}
	}
}

// A photo already the screen's shape fills it, as it would cropped.
func TestWholePhotoOfTheScreensShapeFillsIt(t *testing.T) {
	if roundPanel {
		t.Skip("a round screen fits the circle instead: fitwhole_spot_test.go")
	}
	src := solid(2000, 1000, color.RGBA{R: 10, G: 120, B: 230, A: 255})
	got, crop := fitWhole(src, 1000, 500), cropToFill(src, 1000, 500)
	for i := range got.Pix {
		if got.Pix[i] != crop.Pix[i] {
			t.Fatalf("byte %d differs from the plain crop", i)
		}
	}
}

// A screen whose size is not a whole number of blur blocks, a wide photo on a square screen (the
// Spot's), and an empty picture all come through without a panic, at the screen's size.
func TestWholePhotoOddSizes(t *testing.T) {
	for _, tc := range []struct{ sw, sh, w, h int }{
		{1600, 900, 480, 480},
		{37, 1001, 1279, 799},
		{0, 0, 480, 480},
		{300, 600, 1, 1},
		{300, 600, 10, 5},
	} {
		got := fitWhole(solid(tc.sw, tc.sh, color.RGBA{R: 90, G: 90, B: 90, A: 255}), tc.w, tc.h)
		if got.Bounds() != image.Rect(0, 0, tc.w, tc.h) {
			t.Errorf("%dx%d on %dx%d: got %v", tc.sw, tc.sh, tc.w, tc.h, got.Bounds())
		}
	}
}
