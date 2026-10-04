//go:build spot

package home

import (
	"image/color"
	"math"
	"testing"
)

// On the Spot's round face a whole photo fits inside the circle, corners and all.
func TestWholePhotoFitsTheCircle(t *testing.T) {
	red := color.RGBA{R: 200, G: 40, B: 20, A: 255}
	got := fitWhole(solid(400, 300, red), slideshowW, slideshowH)
	r := float64(slideshowW) / 2
	lit := 0
	for y := 0; y < slideshowH; y++ {
		for x := 0; x < slideshowW; x++ {
			if got.RGBAAt(x, y) != red {
				continue
			}
			lit++
			if d := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r); d > r+1 {
				t.Fatalf("the photo reaches (%d,%d), %.0f px from the middle, outside the circle", x, y, d)
			}
		}
	}
	if lit < slideshowW*slideshowH/4 {
		t.Errorf("only %d pixels of the photo show", lit)
	}
}
